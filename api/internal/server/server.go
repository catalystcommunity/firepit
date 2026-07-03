package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/cors"
	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/config"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/api/internal/transport"
)

// rpcMountPath is the canonical CSIL-RPC HTTP mount (csil-rpc-transport.md
// §3, csilgen/docs/serving-csil-rpc-http-and-tcp.md §3): the whole POST body
// is one request envelope, the whole response body is one response
// envelope, and the path itself carries no routing meaning (service/op live
// inside the envelope).
const rpcMountPath = "/csil/v1/rpc"

// maxRPCBodyBytes caps one envelope's HTTP body, matching the transport
// package's MaxFrameDefault so an HTTP caller can't allocate past the same
// guard a stream carrier already enforces.
const maxRPCBodyBytes = transport.MaxFrameDefault

// Server is firepit-api's HTTP surface: CSIL-RPC dispatch, session
// middleware, /healthz, CORS, and request logging.
type Server struct {
	cfg    config.Config
	store  *store.Store
	routes map[string]map[string]typedHandler

	httpServer *http.Server
}

// New constructs a Server. svcs supplies one implementation per generated
// csil service (see Services); main.go wires csilservices.NewXService stubs
// in today, real implementations as B2-B9 land.
func New(cfg config.Config, st *store.Store, svcs Services) *Server {
	return &Server{
		cfg:    cfg,
		store:  st,
		routes: buildRoutes(svcs),
	}
}

// Handler builds the full middleware chain around the mux: CORS (outermost,
// so even a rejected/OPTIONS request gets CORS headers) -> request logging
// -> session -> mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+rpcMountPath, s.handleRPC)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	// GET /auth/callback (task B2, PLANDOC.md §5): the one HTTP-native route
	// the linkkeys login flow needs — see authcallback.go.
	mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)

	var handler http.Handler = mux
	handler = sessionMiddleware(s.store)(handler)
	handler = requestLoggingMiddleware(handler)
	handler = corsMiddleware(s.cfg.CORSOrigins)(handler)
	return handler
}

// handleRPC is the HTTP carrier for CSIL-RPC: decode the POST body as one
// request envelope, run the shared (service, op) dispatch, encode the
// response, and always answer HTTP 200 — a non-zero transport status still
// rides inside a successfully-encoded envelope (serving-csil-rpc-http-and-tcp.md
// §3); HTTP-level status codes are reserved for carrier failures the
// envelope itself can't express (wrong mount -> 404 via the mux, over-size
// body -> 413 here).
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRPCBodyBytes+1))
	if err != nil {
		http.Error(w, "reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > maxRPCBodyBytes {
		http.Error(w, "request body exceeds max envelope size", http.StatusRequestEntityTooLarge)
		return
	}

	var resp transport.RpcResponse
	req, decodeErr := transport.DecodeRpcRequest(body)
	if decodeErr != nil {
		resp = transport.NewRpcResponseTransportError(transport.StatusMalformedEnvelope, decodeErr.Error())
	} else {
		outcome := dispatch(r.Context(), s.routes, &req)
		if outcome.IsReply {
			resp = transport.NewRpcResponseOk(outcome.Variant, outcome.Payload).WithID(req.ID)
		} else {
			resp = transport.NewRpcResponseTransportError(outcome.Status, outcome.Message).WithID(req.ID)
		}
	}

	out, err := resp.Encode()
	if err != nil {
		// Encoding a well-formed RpcResponse should never fail; if it does,
		// there's nothing typed left to hand back.
		log.WithError(err).Error("encoding CSIL-RPC response envelope")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/cbor")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// handleHealthz is the k8s-probe endpoint: 200 only if the database is
// reachable, so a pod that can't talk to Postgres reports unhealthy instead
// of accepting traffic it can't serve.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]string{"status": "ok"}

	sqlDB, err := s.store.DB.DB()
	if err == nil {
		err = sqlDB.PingContext(r.Context())
	}
	if err != nil {
		status = http.StatusServiceUnavailable
		body = map[string]string{"status": "error", "error": err.Error()}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// corsMiddleware wraps handler with rs/cors configured from origins. A
// single "*" entry (the config default) allows any origin.
func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})
	return c.Handler
}

// statusRecorder captures the status code a handler writes so the logging
// middleware can report it after the fact (http.ResponseWriter has no
// getter of its own).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// requestLoggingMiddleware logs one logrus line per request: method, path,
// status, and duration.
func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.WithFields(log.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"status":   rec.status,
			"duration": time.Since(start).String(),
		}).Info("request")
	})
}

// ListenAndServe starts the HTTP server and blocks until ctx is canceled,
// at which point it drains in-flight requests (graceful shutdown) before
// returning. A listener error returns immediately without waiting for ctx.
func (s *Server) ListenAndServe(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.Port),
		Handler: s.Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
