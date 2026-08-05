// This file registers the GitHub webhook route. The github package verifies
// and processes each event.
package server

import (
	"net/http"

	"github.com/catalystcommunity/firepit/api/internal/github"
)

// registerGithubWebhookRoutes wires POST /webhooks/github into mux, backed
// by a github.Handler constructed over s.store and s.notify — the same
// notify.Publisher every other write path uses, so GitHub-originated
// content fans out notifications identically to human-authored content
// (api/internal/content).
func (s *Server) registerGithubWebhookRoutes(mux *http.ServeMux) {
	h := github.NewWebhookHandler(s.store, s.notify)
	mux.HandleFunc("POST /webhooks/github", h.ServeHTTP)
}
