// Package server (this file): route registration for POST /webhooks/github
// (task B8). All of the actual behavior — per-mapping HMAC verification,
// event filtering, idempotent redelivery handling, and event -> content
// application — lives in api/internal/github; see that package's doc
// comment for the full contract. This file is deliberately just wiring, kept
// in its own function (called with one additive line from server.go's
// Handler()) rather than inlined there, to minimize collision with sibling
// Wave B tasks that also register HTTP-native routes on the shared mux
// (e.g. B2's GET /auth/callback).
package server

import (
	"net/http"

	"github.com/catalystcommunity/firepit/api/internal/github"
)

// registerGithubWebhookRoutes wires POST /webhooks/github into mux, backed
// by a github.Handler constructed over s.store.
func (s *Server) registerGithubWebhookRoutes(mux *http.ServeMux) {
	h := github.NewWebhookHandler(s.store)
	mux.HandleFunc("POST /webhooks/github", h.ServeHTTP)
}
