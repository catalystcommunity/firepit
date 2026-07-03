package server

import (
	"errors"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/csilservices"
)

// handleAuthCallback implements GET /auth/callback (PLANDOC.md §3, §5) — the
// one step of the linkkeys login flow that can't be a CSIL-RPC op: the IDP
// can only redirect a browser to a plain GET URL, never POST a CSIL-RPC
// envelope. Everything else (decrypt-token -> verify-assertion -> best-effort
// userinfo-fetch -> upsert users row -> mint a session) is
// csilservices.CompleteLogin — this handler is deliberately thin: extract the
// query param, call it, translate the result into a Set-Cookie + redirect or
// an HTTP error.
//
// This is registered directly on Server.Handler()'s mux (server.go) as the
// one addition to existing server wiring B2 makes; everything else about
// the login flow lives in the two files this package/task owns
// (csilservices/auth.go, this file).
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	encryptedToken := r.URL.Query().Get("encrypted_token")
	if encryptedToken == "" {
		http.Error(w, "missing encrypted_token", http.StatusBadRequest)
		return
	}

	pki := csilservices.BuildPKIClient(s.cfg)
	if pki == nil {
		log.Error("auth callback: linkkeys RP is not configured")
		http.Error(w, "linkkeys RP is not configured", http.StatusInternalServerError)
		return
	}

	result, err := csilservices.CompleteLogin(r.Context(), s.store, pki, csilservices.NewLoginOptions(s.cfg), encryptedToken)
	if err != nil {
		status, msg := authCallbackErrorResponse(err)
		log.WithError(err).Info("auth callback failed")
		http.Error(w, msg, status)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    result.RawToken,
		Path:     "/",
		Expires:  result.Session.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	redirectTo := s.cfg.PostLoginRedirectURL
	if redirectTo == "" {
		redirectTo = "/"
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// authCallbackErrorResponse maps a CompleteLogin failure to an HTTP status +
// safe-to-show message: an *AppError's Code names an EXPECTED failure mode
// (bad token, bad nonce, expired assertion, ...) that's fine to surface
// verbatim; any other error is an unexpected internal failure whose detail
// stays server-side-only (already logged by the caller), matching the same
// "typed failures are opt-in, everything else is opaque" contract
// api/internal/server/dispatch.go's routeFallible enforces for CSIL ops.
func authCallbackErrorResponse(err error) (int, string) {
	var appErr *csilservices.AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case csilservices.CodeUnauthenticated:
			return http.StatusUnauthorized, appErr.Message
		case csilservices.CodeValidation:
			return http.StatusBadRequest, appErr.Message
		default:
			return http.StatusInternalServerError, "internal error"
		}
	}
	return http.StatusInternalServerError, "internal error"
}
