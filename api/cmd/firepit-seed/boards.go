package main

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// boardSeed is one row of the fixed board catalog every firepit-seed run
// upserts. Titles/descriptions are condensed from each project's own
// README (see this task's notes) rather than invented — this is meant to
// read as "the actual project," not filler copy.
type boardSeed struct {
	Slug        string
	Title       string
	Description string
	Kind        string // store.Board.Kind: "discussion" or "announce"
}

// projectBoards is every board firepit-seed creates unconditionally (no
// flag needed): one per real catalystcommunity project this org runs
// (PLANDOC.md §2's "prior art" table is the same five repos), plus a
// general-purpose board and an announce-kind board for instance-wide
// notices.
var projectBoards = []boardSeed{
	{
		Slug:  "firepit",
		Title: "Firepit",
		Description: "Firepit talking about itself: a dev coordination forum for open " +
			"source projects — threaded discussion, endorsements instead of upvotes, " +
			"subscriptions/notifications, and GitHub event ingestion so project activity " +
			"flows into the same threads humans use. This board is meta discussion, " +
			"roadmap, and dogfooding notes for firepit's own development.",
		Kind: "discussion",
	},
	{
		Slug:  "csilgen",
		Title: "csilgen",
		Description: "CSIL (CBOR Service Interface Language) compiler and code generator — " +
			"the schema/codegen tool that defines firepit's own API contract " +
			"(csil/firepit.csil) and generates its Go server surface and TypeScript client.",
		Kind: "discussion",
	},
	{
		Slug:  "linkkeys",
		Title: "LinkKeys",
		Description: "Domain-anchored identity protocol and server (\"authentication for " +
			"everywhere\") — the login backbone firepit runs on via a linkkeys sidecar in " +
			"relying-party mode.",
		Kind: "discussion",
	},
	{
		Slug:  "reactorcide",
		Title: "Reactorcide",
		Description: "A minimalist CI/CD system this org builds and deploys on, including " +
			"firepit's own test/release/deploy jobs (.reactorcide/jobs/*.yaml).",
		Kind: "discussion",
	},
	{
		Slug:  "longhouse",
		Title: "Longhouse",
		Description: "Coordination system for small-to-medium organizations and " +
			"neighborhoods (tasks, calendar, projects, members, groups, skills) — the " +
			"sibling project firepit's repo layout, linkkeys client, and auth flow were " +
			"patterned on (PLANDOC.md §2).",
		Kind: "discussion",
	},
	{
		Slug:        "general",
		Title:       "General",
		Description: "Anything that doesn't fit a specific project board — cross-project chatter, questions, and introductions.",
		Kind:        "discussion",
	},
	{
		Slug:  "announcements",
		Title: "Announcements",
		Description: "Instance-wide announcements: releases, downtime, policy changes. " +
			"Read like any other board — replies and endorsements still work — but new " +
			"threads here are expected to come from maintainers/admins, not general chatter.",
		Kind: "announce",
	},
}

// seedBoards upserts every board in projectBoards, keyed by slug (the
// schema's own UNIQUE constraint — PLANDOC.md §4), and returns a
// slug -> board id map for callers that need to attach content
// (demo.go) or GitHub mappings (github.go) to a specific board. createdBy
// is used only when a board doesn't exist yet; an existing board's
// created_by is never touched.
func seedBoards(ctx context.Context, st *store.Store, createdBy string) (map[string]string, error) {
	ids := make(map[string]string, len(projectBoards))
	for _, b := range projectBoards {
		board, created, err := upsertBoard(ctx, st, createdBy, b)
		if err != nil {
			return nil, err
		}
		ids[b.Slug] = board.ID
		log.WithFields(log.Fields{"slug": b.Slug, "created": created}).Info("firepit-seed: board")
	}
	return ids, nil
}

// upsertBoard creates the board described by seed if its slug doesn't
// exist yet, or brings an existing board's title/description/kind in line
// with seed if they've drifted (e.g. this catalog's copy improved since the
// board was first seeded) — never touches archived_at, board_members, or
// anything else an admin may have since configured by hand.
func upsertBoard(ctx context.Context, st *store.Store, createdBy string, seed boardSeed) (*store.Board, bool, error) {
	existing, err := st.GetBoardBySlug(ctx, seed.Slug)
	if err == nil {
		updates := map[string]any{}
		if existing.Title != seed.Title {
			updates["title"] = seed.Title
		}
		if existing.Description != seed.Description {
			updates["description"] = seed.Description
		}
		if existing.Kind != seed.Kind {
			updates["kind"] = seed.Kind
		}
		if len(updates) == 0 {
			return existing, false, nil
		}
		updated, err := st.UpdateBoardFields(ctx, existing.ID, updates)
		if err != nil {
			return nil, false, err
		}
		return updated, false, nil
	}
	if !store.IsNotFound(err) {
		return nil, false, err
	}

	board := &store.Board{
		Slug:        seed.Slug,
		Title:       seed.Title,
		Description: seed.Description,
		Kind:        seed.Kind,
		CreatedBy:   createdBy,
	}
	if err := st.CreateBoard(ctx, board); err != nil {
		// A racing concurrent seed run (or a board created by hand between
		// the lookup above and this insert) loses the create but not the
		// data: re-read and treat it the same as "already existed."
		if store.IsUniqueViolation(err) {
			existing, ferr := st.GetBoardBySlug(ctx, seed.Slug)
			if ferr == nil {
				return existing, false, nil
			}
		}
		return nil, false, err
	}
	return board, true, nil
}
