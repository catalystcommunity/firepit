// Package content holds the single, shared implementation of "insert a
// post/comment row, maintain its derived state (ltree path, comment_count,
// last_activity_at), and publish the resulting notify.Publisher event" —
// the one thing every piece of firepit content creation must do correctly
// regardless of who or what is authoring it.
//
// Two callers use this package today:
//
//   - api/internal/csilservices/thread.go (ThreadService.CreatePost /
//     CreateComment): human-authored content, arriving over an
//     authenticated CSIL-RPC session.
//   - api/internal/github (contentWriter): system-authored content
//     ingested from GitHub webhook deliveries, attributed to a per-repo
//     system user, with no session at all (the caller isn't a user in any
//     sense a session could represent).
//
// Before this package existed, the github package had its own duplicate of
// the post-insert / comment-insert-plus-ltree-plus-bump logic (see git
// history: contentwriter.go's former createPost/createTopLevelComment) —
// written independently while ThreadService was still being built in a
// sibling worktree, and never emitting notify events, so board subscribers
// were never told about GitHub-originated activity. That was a product bug
// (PLANDOC.md §4: "GitHub content is first-class"), not just duplication.
// Both callers now go through CreatePost/CreateComment below, so there is
// exactly one place that knows how to create a post or comment, and every
// creation — human or system — fans out notifications identically.
//
// # What this package validates, and what it deliberately does NOT
//
// CreatePost/CreateComment perform no product-level input validation and no
// authorization check of their own. Both are the responsibility of the
// caller, and the two callers deliberately differ:
//
//   - ThreadService requires reqctx.User(ctx) (a real, authenticated
//     session) before calling in at all — see thread.go's Unauthenticated
//     checks. The github package has no session to check: a webhook
//     delivery is authenticated by its own HMAC signature (verify.go), and
//     the content it produces is attributed to a synthetic per-repo system
//     user (contentwriter.go's getOrCreateSystemUser) that was never meant
//     to log in. Routing system content through a fake session (rather than
//     skipping the session check entirely) would misrepresent an
//     unauthenticated delivery as an authenticated user action, so the
//     github package calls this package directly instead of going through
//     ThreadService/reqctx at all.
//   - ThreadService enforces product-facing bounds on human input
//     (validateTitleAndBody / validateBody in thread.go: non-blank title
//     capped at 200 chars, non-blank body capped at 100000 chars) before
//     calling CreatePost/CreateComment. The github package does NOT run
//     these checks on GitHub-sourced text, for two concrete reasons a human
//     compose box doesn't have to deal with:
//     1. GitHub issue/PR titles routinely exceed firepit's 200-char
//     product cap once contentWriter prefixes/suffixes them ("#123 ",
//     " (closed)", "PR #7 ", " (merged)") — rejecting those would silently
//     drop real GitHub activity for a cosmetic length rule invented for
//     human-typed titles.
//     2. A GitHub issue, PR, or release legitimately has an EMPTY body
//     (no description written) — validateBody's "must not be blank" rule
//     exists to stop a human from submitting a vacuous post, which doesn't
//     apply to "this is what GitHub sent."
//     Skipping these checks is safe here specifically because origin_ref
//     always identifies the exact GitHub object a post/comment came from —
//     unlike a human compose box, there's no risk of empty/garbage content
//     with no provenance.
//   - ThreadService's CreatePost/CreateComment also check the target
//     board/post isn't archived/deleted before calling in (thread.go). The
//     github package does not: an archived board rejecting new HUMAN posts
//     is a moderation control aimed at a UI a person interacts with and can
//     be told "no" — a GitHub webhook has no such UI, GitHub will retry a
//     rejected delivery believing it failed, and there's no product reason
//     project automation should silently stop working the moment a board
//     is archived. This matches contentWriter's pre-existing behavior
//     (it never checked board state either); unifying the write path
//     doesn't change that judgment call.
//
// Both callers remain responsible for resolving/validating everything
// above BEFORE calling into this package; CreatePost/CreateComment trust
// their params completely.
package content

import (
	"context"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// CreatePostParams is the input to CreatePost. Origin/OriginRef are passed
// through verbatim to the store.Post row: ThreadService leaves both zero
// (Origin's SQL default is 'user', per store.Post's gorm tag; OriginRef
// stays nil), the github package sets Origin="github" and a populated
// OriginRef (see github.OriginRef).
type CreatePostParams struct {
	BoardID   string
	AuthorID  string
	Title     string
	BodyMD    string
	Origin    string
	OriginRef datatypes.JSON
}

// CreatePost inserts a new post and publishes the notify.KindContentCreated
// event that follows from it, both via tx — the caller owns the
// transaction boundary (ThreadService.CreatePost / the github package's
// per-event handlers each wrap their own). See the package doc comment for
// what's validated before this is ever called.
func CreatePost(ctx context.Context, tx *gorm.DB, st *store.Store, pub notify.Publisher, p CreatePostParams) (*store.Post, error) {
	post := &store.Post{
		BoardID:        p.BoardID,
		AuthorID:       p.AuthorID,
		Title:          p.Title,
		BodyMD:         p.BodyMD,
		Origin:         p.Origin,
		OriginRef:      p.OriginRef,
		LastActivityAt: time.Now().UTC(),
	}
	if err := st.CreatePost(ctx, tx, post); err != nil {
		return nil, err
	}

	if err := pub.Publish(ctx, tx, notify.Event{
		Kind:    notify.KindContentCreated,
		ActorID: p.AuthorID,
		BoardID: p.BoardID,
		PostID:  post.ID,
		Body:    post.BodyMD,
	}); err != nil {
		return nil, err
	}
	return post, nil
}

// CreateCommentParams is the input to CreateComment. ParentCommentID/
// ParentPath are both left zero for a top-level reply directly on the post
// (the only kind the github package ever creates — see contentwriter.go);
// ThreadService supplies both when replying to an existing comment.
type CreateCommentParams struct {
	BoardID         string
	PostID          string
	ParentCommentID *string
	ParentPath      store.Ltree
	AuthorID        string
	BodyMD          string
	Origin          string
	OriginRef       datatypes.JSON
}

// CreateComment inserts a new comment, bumps its post's
// comment_count/last_activity_at, and publishes the resulting
// notify.KindContentCreated event, all via tx.
//
// The comment's id is minted client-side (store.NewCommentID) before the
// insert, exactly as ThreadService always has: the row's own `path` column
// is "parent's path + this comment's own label" (store.ChildPath), which
// isn't computable from a post-insert RETURNING id the way every other
// table's primary key is. This sidesteps what the pre-unification
// contentWriter had to do instead (insert with a placeholder path, then a
// second UPDATE once the DB-assigned id was known) — see the package doc
// comment.
func CreateComment(ctx context.Context, tx *gorm.DB, st *store.Store, pub notify.Publisher, p CreateCommentParams) (*store.Comment, error) {
	newID, err := store.NewCommentID()
	if err != nil {
		return nil, err
	}

	comment := &store.Comment{
		ID:              newID,
		PostID:          p.PostID,
		ParentCommentID: p.ParentCommentID,
		AuthorID:        p.AuthorID,
		Path:            store.ChildPath(p.ParentPath, newID),
		BodyMD:          p.BodyMD,
		Origin:          p.Origin,
		OriginRef:       p.OriginRef,
	}
	if err := st.CreateComment(ctx, tx, comment); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := st.BumpPostActivity(ctx, tx, p.PostID, now, 1); err != nil {
		return nil, err
	}

	if err := pub.Publish(ctx, tx, notify.Event{
		Kind:      notify.KindContentCreated,
		ActorID:   p.AuthorID,
		BoardID:   p.BoardID,
		PostID:    p.PostID,
		CommentID: comment.ID,
		Body:      comment.BodyMD,
	}); err != nil {
		return nil, err
	}
	return comment, nil
}
