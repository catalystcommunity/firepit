package main

import (
	"context"
	"regexp"
	"strings"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// slugSanitizePattern collapses every run of non-[a-z0-9] characters to a
// single hyphen. Mirrors csilservices.slugify (api/internal/csilservices/
// auth.go) — duplicated here deliberately rather than imported: importing
// csilservices would pull in its linkkeys/config/reqctx dependency graph
// into a script that has no session, no RP client, and no HTTP server, for
// the sake of one ten-line pure function.
var slugSanitizePattern = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases s and replaces every run of non-[a-z0-9] characters
// with a single hyphen, trimming leading/trailing hyphens. Used to derive a
// `users.handle` candidate from a linkkeys user_id or a board title.
func slugify(s string) string {
	lowered := strings.ToLower(s)
	slug := slugSanitizePattern.ReplaceAllString(lowered, "-")
	return strings.Trim(slug, "-")
}

// titleCaseWords turns a hyphen/underscore/space-separated slug back into a
// readable display-name-shaped string ("firepit-seed-bot" ->
// "Firepit Seed Bot"). Only used for placeholder display names this script
// invents (seed-bot, demo users, CLI-bootstrapped admins with no real
// linkkeys claims to draw from) — see docs/OPERATING.md's admin bootstrap
// section for why these placeholders are sticky until the real person logs
// in and store.UpsertUser's doc comment for why a first CLI-created row's
// handle/display_name are never silently overwritten by a later login.
func titleCaseWords(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	if len(parts) == 0 {
		return s
	}
	return strings.Join(parts, " ")
}

// seedBotLinkkeysDomain/seedBotLinkkeysUserID identify the synthetic system
// user firepit-seed uses as `boards.created_by`/`github_mappings.created_by`
// when no `--admin` was given to attribute them to instead. Modeled on
// api/internal/github's per-repo system user scheme (getOrCreateSystemUser,
// doc.go's "System user per mapping" section): a fixed, constant identity
// pair so re-running the seeder always finds the same row rather than
// minting a new one every time.
const (
	seedBotLinkkeysDomain = "seed.firepit.system"
	seedBotLinkkeysUserID = "firepit-seed-bot"
	seedBotHandle         = "firepit-seed-bot"
)

// ensureSeedBotUser returns firepit-seed's own system user, creating it on
// first run. Deliberately does NOT go through store.UpsertUser: that
// helper hardcodes Kind: "human" (it exists to upsert real linkkeys logins,
// which are always human), and this row needs Kind: "system" so it reads
// correctly everywhere `users.kind` is surfaced (e.g. UserProfile.kind on
// the wire) — it's attribution plumbing for seeded rows, not a person.
func ensureSeedBotUser(ctx context.Context, st *store.Store) (*store.User, error) {
	var existing store.User
	err := st.DB.WithContext(ctx).
		Where("linkkeys_domain = ? AND linkkeys_user_id = ?", seedBotLinkkeysDomain, seedBotLinkkeysUserID).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !store.IsNotFound(err) {
		return nil, err
	}

	user := &store.User{
		LinkkeysDomain: seedBotLinkkeysDomain,
		LinkkeysUserID: seedBotLinkkeysUserID,
		Handle:         seedBotHandle,
		DisplayName:    "Firepit Seed Bot",
		Kind:           "system",
		Roles:          store.StringArray{},
	}
	if err := st.DB.WithContext(ctx).Create(user).Error; err != nil {
		if store.IsUniqueViolation(err) {
			// Lost a race with a concurrent seed run (or, vanishingly
			// unlikely, a handle collision with some other row): re-fetch
			// by identity rather than treating this as fatal.
			var winner store.User
			if ferr := st.DB.WithContext(ctx).
				Where("linkkeys_domain = ? AND linkkeys_user_id = ?", seedBotLinkkeysDomain, seedBotLinkkeysUserID).
				First(&winner).Error; ferr == nil {
				return &winner, nil
			}
		}
		return nil, err
	}
	return user, nil
}
