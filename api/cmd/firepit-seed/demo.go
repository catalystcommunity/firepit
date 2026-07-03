// This file (demo.go): `--demo` seeds a small, deliberately hand-curated
// dataset so a fresh dev environment is immediately browsable end to end —
// PLANDOC.md §7 D3's accept criterion ("fresh ./tools.sh dev gives a
// browsable seeded forum") plus this task's own scope note ("a few threads
// with nested comments, endorsements, and subscriptions"). Never run this
// against a production database: demo users are fake linkkeys identities
// nobody can ever actually log in as.
//
// Idempotency: demoIsAlreadySeeded checks for one canary post (the
// "Welcome to Firepit" post below) before creating anything. If it's
// there, the whole block is skipped — this dataset is meant to exist
// exactly once, not to accumulate a fresh copy of itself on every
// `./tools.sh seed --demo` re-run. Demo USERS are still upserted
// unconditionally first (store.UpsertUser is itself idempotent, and doing
// so means `--demo` always leaves the demo identities present even if
// someone deleted the content but not the users).
package main

import (
	"context"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/content"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// demoLinkkeysDomain is the fake identity domain every demo user lives
// under — chosen to be obviously non-routable/non-real, mirroring
// api/internal/github's "github.firepit.system" convention for its own
// synthetic identities.
const demoLinkkeysDomain = "demo.firepit.local"

// demoUserHandles is every demo identity --demo creates, in creation order.
var demoUserHandles = []string{"alice", "bob", "carol", "dave"}

// demoWelcomePostTitle is the canary demoIsAlreadySeeded checks for.
const demoWelcomePostTitle = "Welcome to Firepit \U0001F44B" // "👋"

// seedDemo seeds demo users, then (unless the canary post already exists)
// a few threads with nested comments, endorsements, and subscriptions
// across the general/firepit/announcements boards.
func seedDemo(ctx context.Context, st *store.Store, boardIDs map[string]string) error {
	users, err := seedDemoUsers(ctx, st)
	if err != nil {
		return err
	}

	generalID := boardIDs["general"]
	firepitID := boardIDs["firepit"]
	announceID := boardIDs["announcements"]

	seeded, err := demoIsAlreadySeeded(ctx, st, generalID)
	if err != nil {
		return err
	}
	if seeded {
		log.Info("firepit-seed: demo content already present, skipping")
		return nil
	}

	pub := notify.NewDBPublisher()
	alice, bob, carol, dave := users["alice"], users["bob"], users["carol"], users["dave"]

	// --- Thread 1: general board, a welcome post with a nested reply ---
	welcome, err := createDemoPost(ctx, st, pub, generalID, alice.ID, demoWelcomePostTitle,
		"Hi all! This is firepit's dev instance, seeded so there's something to look at.\n\n"+
			"Boards on the left, threads inside them, replies nest as deep as you like. "+
			"No upvotes here — instead you can **endorse** a post or comment to publicly stand "+
			"behind it. Say hello below!")
	if err != nil {
		return err
	}
	if _, err := st.Subscribe(ctx, alice.ID, "board", generalID); err != nil {
		return err
	}
	bobHello, err := createDemoComment(ctx, st, pub, generalID, welcome, bob.ID,
		"Hello! Glad this is finally running somewhere I can poke at it.", nil)
	if err != nil {
		return err
	}
	if _, err := createDemoComment(ctx, st, pub, generalID, welcome, carol.ID,
		"+1, the nested-reply rendering looks right so far.", bobHello); err != nil {
		return err
	}
	if _, _, err := st.InsertEndorsement(ctx, st.DB, carol.ID, "post", welcome.ID); err != nil {
		return err
	}

	// --- Thread 2: firepit board, a roadmap question with a deeper reply ---
	roadmap, err := createDemoPost(ctx, st, pub, firepitID, bob.ID,
		"What should v1.1 focus on?",
		"v1 shipped boards/threads/endorsements/subscriptions/notifications/GitHub ingestion. "+
			"Once we're dogfooding on this instance for a bit, what's the first thing that's "+
			"going to feel missing?")
	if err != nil {
		return err
	}
	if _, err := st.Subscribe(ctx, carol.ID, "post", roadmap.ID); err != nil {
		return err
	}
	daveSuggestion, err := createDemoComment(ctx, st, pub, firepitID, roadmap, dave.ID,
		"Full-text search, probably — PLANDOC already leaves room for it in the schema.", nil)
	if err != nil {
		return err
	}
	if _, err := createDemoComment(ctx, st, pub, firepitID, roadmap, alice.ID,
		"Agreed, though I'd want unread counts to feel instant first.", daveSuggestion); err != nil {
		return err
	}
	if _, _, err := st.InsertEndorsement(ctx, st.DB, bob.ID, "comment", daveSuggestion.ID); err != nil {
		return err
	}

	// --- Thread 3: announcements board, a single top-level post ---
	if _, err := createDemoPost(ctx, st, pub, announceID, dave.ID,
		"Firepit dev instance is live",
		"This instance is seeded dev data — see docs/OPERATING.md if you're standing up your own."); err != nil {
		return err
	}

	log.Info("firepit-seed: demo content seeded (3 threads, nested replies, endorsements, subscriptions)")
	return nil
}

// seedDemoUsers upserts every handle in demoUserHandles under
// demoLinkkeysDomain, returning a handle -> *store.User map. Idempotent via
// store.UpsertUser.
func seedDemoUsers(ctx context.Context, st *store.Store) (map[string]*store.User, error) {
	out := make(map[string]*store.User, len(demoUserHandles))
	for _, handle := range demoUserHandles {
		user, err := st.UpsertUser(ctx, demoLinkkeysDomain, handle, handle, titleCaseWords(handle))
		if err != nil {
			return nil, err
		}
		out[handle] = user
	}
	return out, nil
}

// demoIsAlreadySeeded reports whether the welcome canary post already
// exists on the general board.
func demoIsAlreadySeeded(ctx context.Context, st *store.Store, generalBoardID string) (bool, error) {
	var count int64
	err := st.DB.WithContext(ctx).Table("posts").
		Where("board_id = ? AND title = ? AND deleted_at IS NULL", generalBoardID, demoWelcomePostTitle).
		Count(&count).Error
	return count > 0, err
}

// createDemoPost wraps content.CreatePost in its own transaction, matching
// the pattern csilservices.threadService.CreatePost uses (see thread.go) so
// seeded posts fan out notifications identically to a human-authored one.
func createDemoPost(ctx context.Context, st *store.Store, pub notify.Publisher, boardID, authorID, title, body string) (*store.Post, error) {
	var post *store.Post
	err := st.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		post, err = content.CreatePost(ctx, tx, st, pub, content.CreatePostParams{
			BoardID:  boardID,
			AuthorID: authorID,
			Title:    title,
			BodyMD:   body,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return post, nil
}

// createDemoComment wraps content.CreateComment in its own transaction. If
// parent is non-nil, the new comment nests under it (ParentCommentID +
// ParentPath); otherwise it's a top-level reply directly on the post.
func createDemoComment(ctx context.Context, st *store.Store, pub notify.Publisher, boardID string, post *store.Post, authorID, body string, parent *store.Comment) (*store.Comment, error) {
	var parentID *string
	var parentPath store.Ltree
	if parent != nil {
		id := parent.ID
		parentID = &id
		parentPath = parent.Path
	}

	var comment *store.Comment
	err := st.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		comment, err = content.CreateComment(ctx, tx, st, pub, content.CreateCommentParams{
			BoardID:         boardID,
			PostID:          post.ID,
			ParentCommentID: parentID,
			ParentPath:      parentPath,
			AuthorID:        authorID,
			BodyMD:          body,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return comment, nil
}
