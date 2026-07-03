package github

import (
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// maxBodyBytes caps a webhook request body. GitHub's own limit is 25MB;
// firepit's payloads (issue/PR/release bodies) are markdown text, nowhere
// near that in practice, so this is a generous ceiling against abuse, not a
// tight fit to expected payload size.
const maxBodyBytes = 10 << 20 // 10 MiB

// Handler is the POST /webhooks/github HTTP handler: per-mapping HMAC-SHA256
// verification, event filtering, idempotent redelivery handling, and
// event -> content application (see the package doc comment for the full
// request flow).
type Handler struct {
	store  *store.Store
	writer *contentWriter
}

// NewWebhookHandler constructs the GitHub webhook Handler over st, wiring
// pub (notify.NewDBPublisher in production; see cmd/firepit-api/main.go)
// through to contentWriter so a GitHub-originated post/comment fans out
// notifications exactly like a human-authored one — see
// api/internal/content's package doc comment for the shared creation path
// this exercises.
func NewWebhookHandler(st *store.Store, pub notify.Publisher) *Handler {
	return &Handler{store: st, writer: &contentWriter{store: st, notify: pub}}
}

// ServeHTTP implements http.Handler. See the package doc comment for the
// full step-by-step request flow this follows.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		http.Error(w, "reading request body", http.StatusBadRequest)
		return
	}
	if len(body) > maxBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	signature := r.Header.Get("X-Hub-Signature-256")

	if eventType == "" {
		http.Error(w, "missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}

	repo, err := peekRepoFullName(body)
	if err != nil {
		http.Error(w, "malformed JSON body", http.StatusBadRequest)
		return
	}
	if repo == "" {
		// No repository in the payload at all — nothing to look up a
		// mapping (or a secret) by, and nothing this package understands
		// is repo-less. Drop, not an error.
		writeOK(w, "no repository in payload")
		return
	}

	mapping, err := h.store.GetGithubMappingByRepo(ctx, repo)
	if err != nil {
		if store.IsNotFound(err) {
			log.WithField("repo", repo).Debug("github webhook: unmapped repo, dropping")
			writeOK(w, "unmapped repo")
			return
		}
		log.WithError(err).WithField("repo", repo).Error("github webhook: mapping lookup failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	secret, ok := ResolveSecret(mapping.SecretRef)
	if !ok {
		// Fail closed: a mapping whose secret_ref doesn't resolve to a
		// configured value is a deployment misconfiguration, never treated
		// as "skip verification."
		log.WithFields(log.Fields{"repo": repo, "secret_ref": mapping.SecretRef}).
			Error("github webhook: secret_ref did not resolve to a configured secret")
		http.Error(w, "signature verification unavailable", http.StatusUnauthorized)
		return
	}
	if !VerifySignature(secret, body, signature) {
		log.WithField("repo", repo).Warn("github webhook: signature verification failed")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	if eventType == "ping" {
		writeOK(w, "pong")
		return
	}

	if deliveryID == "" {
		log.WithFields(log.Fields{"repo": repo, "event": eventType}).
			Warn("github webhook: missing X-GitHub-Delivery header, cannot dedupe redeliveries for this request")
	}

	handled, err := h.writer.HandleEvent(ctx, mapping, eventType, deliveryID, body)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"repo": repo, "event": eventType, "delivery_id": deliveryID}).
			Error("github webhook: applying event failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	log.WithFields(log.Fields{"repo": repo, "event": eventType, "delivery_id": deliveryID, "handled": handled}).
		Debug("github webhook: processed")
	writeOK(w, "ok")
}

func writeOK(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"` + msg + `"}`))
}
