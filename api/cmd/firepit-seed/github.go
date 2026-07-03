// This file (github.go): `--github-mappings` seeds github_mappings rows
// for a few real catalystcommunity repos, per PLANDOC.md §7 D3 ("GitHub
// mappings for 2-3 real repos"). This ONLY creates the routing rows
// (repo -> board, which events, what thread_mode) — it does NOT set the
// FIREPIT_GH_SECRET_* environment variables the webhook receiver resolves
// secret_ref against (api/internal/github/doc.go's "Secret resolution"
// section), and it does NOT configure anything on GitHub's side. Both of
// those are deployment-specific and covered in docs/OPERATING.md's "GitHub
// webhook setup" section — this seeder only ever writes rows that make
// firepit's OWN side of the mapping ready to receive a correctly-configured
// webhook.
//
// Gated behind its own flag (never seeded unconditionally, unlike boards)
// so a prod run of `firepit-seed` doesn't silently start accepting webhook
// deliveries for repos an operator hasn't deliberately wired secrets for —
// this task's own scope note calls this out explicitly ("prod opt-in is
// explicit").
package main

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// githubMappingSeed is one row of the fixed mapping catalog --github-mappings
// upserts.
type githubMappingSeed struct {
	Repo       string   // "owner/name"
	BoardSlug  string   // must be a slug seedBoards already created
	Events     []string // github_mappings.events: raw X-GitHub-Event header values this package understands (api/internal/github/doc.go's "Event rules v1")
	ThreadMode string   // github_thread_mode enum value
	SecretRef  string   // env var name the webhook receiver reads at verify time (NOT the secret itself)
}

// githubMappings is every mapping --github-mappings seeds: the same three
// repos this task's scope calls out (firepit, csilgen, reactorcide),
// routed to the project board seedBoards already created for each. Every
// SecretRef follows api/internal/github/doc.go's documented
// FIREPIT_GH_SECRET_<REPO> convention exactly — an operator wiring up the
// real webhook needs only to set that env var and configure the same
// value as the GitHub repo's webhook secret (docs/OPERATING.md walks
// through this).
var githubMappings = []githubMappingSeed{
	{
		Repo:       "catalystcommunity/firepit",
		BoardSlug:  "firepit",
		Events:     []string{"release", "issues", "pull_request"},
		ThreadMode: "post_per_issue",
		SecretRef:  "FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_FIREPIT",
	},
	{
		Repo:       "catalystcommunity/csilgen",
		BoardSlug:  "csilgen",
		Events:     []string{"release", "issues", "pull_request"},
		ThreadMode: "post_per_release",
		SecretRef:  "FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_CSILGEN",
	},
	{
		Repo:       "catalystcommunity/reactorcide",
		BoardSlug:  "reactorcide",
		Events:     []string{"release", "issues", "pull_request"},
		ThreadMode: "post_per_pull_request",
		SecretRef:  "FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_REACTORCIDE",
	},
}

// seedGithubMappings upserts every entry in githubMappings, keyed by repo
// (the schema's own UNIQUE constraint). createdBy attributes newly-created
// rows only — an existing mapping's created_by is never touched.
func seedGithubMappings(ctx context.Context, st *store.Store, createdBy string, boardIDs map[string]string) error {
	for _, m := range githubMappings {
		boardID, ok := boardIDs[m.BoardSlug]
		if !ok {
			log.WithField("repo", m.Repo).Warn("firepit-seed: skipping github mapping, board not seeded")
			continue
		}
		created, err := upsertGithubMapping(ctx, st, createdBy, boardID, m)
		if err != nil {
			return err
		}
		log.WithFields(log.Fields{
			"repo":       m.Repo,
			"secret_ref": m.SecretRef,
			"created":    created,
		}).Info("firepit-seed: github mapping")
	}
	return nil
}

func upsertGithubMapping(ctx context.Context, st *store.Store, createdBy, boardID string, seed githubMappingSeed) (bool, error) {
	existing, err := st.GetGithubMappingByRepo(ctx, seed.Repo)
	if err == nil {
		updates := map[string]any{}
		if existing.BoardID != boardID {
			updates["board_id"] = boardID
		}
		if !stringSlicesEqual([]string(existing.Events), seed.Events) {
			updates["events"] = store.StringArray(seed.Events)
		}
		if existing.ThreadMode != seed.ThreadMode {
			updates["thread_mode"] = seed.ThreadMode
		}
		if existing.SecretRef != seed.SecretRef {
			updates["secret_ref"] = seed.SecretRef
		}
		if len(updates) == 0 {
			return false, nil
		}
		return false, st.DB.WithContext(ctx).Model(&store.GithubMapping{}).
			Where("id = ?", existing.ID).Updates(updates).Error
	}
	if !store.IsNotFound(err) {
		return false, err
	}

	mapping := &store.GithubMapping{
		BoardID:    boardID,
		Repo:       seed.Repo,
		Events:     store.StringArray(seed.Events),
		SecretRef:  seed.SecretRef,
		ThreadMode: seed.ThreadMode,
		CreatedBy:  createdBy,
	}
	if err := st.CreateGithubMapping(ctx, mapping); err != nil {
		if store.IsUniqueViolation(err) {
			return false, nil // lost a race with a concurrent seed run; already there
		}
		return false, err
	}
	return true, nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
