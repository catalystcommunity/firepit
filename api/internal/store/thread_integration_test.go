//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// threadTestEnv boots one testcontainers Postgres (baseline migrated) for
// every test in this file to share, since each spins up its own is slow and
// these tests don't mutate shared state in conflicting ways (each creates
// its own board/post/user rows).
func newThreadTestEnv(t *testing.T) (*gorm.DB, *store.Store) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:17",
		tcpostgres.WithDatabase("firepit_test"),
		tcpostgres.WithUsername("firepit"),
		tcpostgres.WithPassword("devpass123"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pgContainer.Terminate(ctx))
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, coredb.Up(dsn))

	gdb, err := store.Open(dsn)
	require.NoError(t, err)
	return gdb, store.New(gdb)
}

// seedUserAndBoard creates the minimal users/boards rows every test needs.
func seedUserAndBoard(t *testing.T, db *gorm.DB) (author *store.User, board *store.Board) {
	t.Helper()
	author = &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "u1", Handle: "author", Kind: "human"}
	require.NoError(t, db.Create(author).Error)

	board = &store.Board{Slug: "general", Title: "General", Kind: "discussion", CreatedBy: author.ID}
	require.NoError(t, db.Create(board).Error)
	return author, board
}

func seedPost(t *testing.T, db *gorm.DB, st *store.Store, board *store.Board, author *store.User) *store.Post {
	t.Helper()
	ctx := context.Background()
	post := &store.Post{
		BoardID:        board.ID,
		AuthorID:       author.ID,
		Title:          "Hello",
		BodyMD:         "hello world",
		LastActivityAt: time.Now().UTC(),
	}
	require.NoError(t, st.CreatePost(ctx, db, post))
	return post
}

// TestDeepCommentTreeOrdering builds a >=6-level-deep comment tree plus a
// sibling branch and confirms ListCommentsByPost returns them in
// depth-first pre-order (each node immediately followed by its whole
// subtree, before any sibling's subtree) — the ordering get-thread relies
// on to hand back one flat, tree-renderable list from one query.
func TestDeepCommentTreeOrdering(t *testing.T) {
	gdb, st := newThreadTestEnv(t)
	ctx := context.Background()
	author, board := seedUserAndBoard(t, gdb)
	post := seedPost(t, gdb, st, board, author)

	mkComment := func(parentPath store.Ltree, parentID *string, label string) *store.Comment {
		id, err := store.NewCommentID()
		require.NoError(t, err)
		c := &store.Comment{
			ID:              id,
			PostID:          post.ID,
			ParentCommentID: parentID,
			AuthorID:        author.ID,
			Path:            store.ChildPath(parentPath, id),
			BodyMD:          "reply " + label,
		}
		require.NoError(t, st.CreateComment(ctx, gdb, c))
		time.Sleep(2 * time.Millisecond) // keep NewCommentID's timestamp prefix strictly increasing
		return c
	}

	// Six-level-deep chain: L1 -> L2 -> L3 -> L4 -> L5 -> L6.
	l1 := mkComment("", nil, "L1")
	l2 := mkComment(l1.Path, &l1.ID, "L2")
	l3 := mkComment(l2.Path, &l2.ID, "L3")
	l4 := mkComment(l3.Path, &l3.ID, "L4")
	l5 := mkComment(l4.Path, &l4.ID, "L5")
	_ = mkComment(l5.Path, &l5.ID, "L6")

	// A second top-level branch, created after the deep chain, to prove
	// ordering is by tree structure (path), not creation time overall.
	_ = mkComment("", nil, "M1")

	// A reply hung directly off L3 (a second child, after L4 already
	// exists) — must sort after L3's entire L4->L5->L6 subtree, since it's
	// a *sibling* of L4, not an ancestor.
	_ = mkComment(l3.Path, &l3.ID, "L3b")

	comments, err := st.ListCommentsByPost(ctx, post.ID)
	require.NoError(t, err)
	require.Len(t, comments, 8)

	var order []string
	for _, c := range comments {
		order = append(order, c.BodyMD)
	}
	require.Equal(t,
		[]string{"reply L1", "reply L2", "reply L3", "reply L4", "reply L5", "reply L6", "reply L3b", "reply M1"},
		order,
		"expected depth-first pre-order: each node's whole subtree before any later sibling's",
	)
}

// TestTombstonedMidTreeCommentKeepsDescendants confirms that soft-deleting
// a comment in the middle of a tree leaves its descendants (and its own
// row/structure) fully queryable — SoftDeleteComment only sets deleted_at,
// it never touches parent_comment_id/path or cascades to children.
func TestTombstonedMidTreeCommentKeepsDescendants(t *testing.T) {
	gdb, st := newThreadTestEnv(t)
	ctx := context.Background()
	author, board := seedUserAndBoard(t, gdb)
	post := seedPost(t, gdb, st, board, author)

	mk := func(parentPath store.Ltree, parentID *string) *store.Comment {
		id, err := store.NewCommentID()
		require.NoError(t, err)
		c := &store.Comment{
			ID: id, PostID: post.ID, ParentCommentID: parentID, AuthorID: author.ID,
			Path: store.ChildPath(parentPath, id), BodyMD: "body",
		}
		require.NoError(t, st.CreateComment(ctx, gdb, c))
		time.Sleep(2 * time.Millisecond)
		return c
	}

	root := mk("", nil)
	mid := mk(root.Path, &root.ID)
	leaf := mk(mid.Path, &mid.ID)

	require.NoError(t, st.SoftDeleteComment(ctx, gdb, mid.ID, time.Now().UTC()))

	comments, err := st.ListCommentsByPost(ctx, post.ID)
	require.NoError(t, err)
	require.Len(t, comments, 3, "tombstoning must not remove the row or its descendants")

	byID := map[string]store.Comment{}
	for _, c := range comments {
		byID[c.ID] = c
	}

	gotMid, ok := byID[mid.ID]
	require.True(t, ok, "tombstoned comment must still be present")
	require.NotNil(t, gotMid.DeletedAt, "tombstoned comment must have deleted_at set")
	require.Equal(t, "body", gotMid.BodyMD, "store layer keeps the real body; redaction happens in csilservices, not here")

	gotLeaf, ok := byID[leaf.ID]
	require.True(t, ok, "descendant of a tombstoned comment must survive")
	require.Nil(t, gotLeaf.DeletedAt)
	require.NotNil(t, gotLeaf.ParentCommentID)
	require.Equal(t, mid.ID, *gotLeaf.ParentCommentID, "descendant's structure must be untouched by the ancestor's tombstone")
}

// TestRevisionSnapshotOnEveryEdit exercises the create-revision-then-update
// pattern ThreadService.EditPost/EditComment use, confirming multiple edits
// accumulate multiple revisions in reverse-chronological order and each one
// holds the content as it was immediately BEFORE that edit.
func TestRevisionSnapshotOnEveryEdit(t *testing.T) {
	gdb, st := newThreadTestEnv(t)
	ctx := context.Background()
	author, board := seedUserAndBoard(t, gdb)
	post := seedPost(t, gdb, st, board, author)

	applyEdit := func(newTitle, newBody string) {
		err := gdb.Transaction(func(tx *gorm.DB) error {
			var p store.Post
			if err := tx.First(&p, "id = ?", post.ID).Error; err != nil {
				return err
			}
			if err := st.CreateRevision(ctx, tx, &store.Revision{
				TargetType: "post", TargetID: p.ID, EditorID: author.ID,
				PrevTitle: &p.Title, PrevBodyMD: p.BodyMD,
			}); err != nil {
				return err
			}
			p.Title = newTitle
			p.BodyMD = newBody
			return st.UpdatePost(ctx, tx, &p)
		})
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}

	applyEdit("Hello v2", "hello world v2")
	applyEdit("Hello v3", "hello world v3")

	revisions, err := st.ListRevisions(ctx, gdb, "post", post.ID)
	require.NoError(t, err)
	require.Len(t, revisions, 2)

	// Most-recent-edit-first.
	require.Equal(t, "hello world v2", revisions[0].PrevBodyMD, "most recent revision snapshots the state right before the LAST edit")
	require.Equal(t, "hello world", revisions[1].PrevBodyMD, "oldest revision snapshots the original content")

	var current store.Post
	require.NoError(t, gdb.First(&current, "id = ?", post.ID).Error)
	require.Equal(t, "hello world v3", current.BodyMD, "the live row always holds the latest state, never a snapshot")
}

// TestCursorPaginationStabilityUnderConcurrentInserts drives
// ListPostsByBoard's keyset predicate through two pages and confirms a
// post inserted with newer activity than the cursor (simulating a write
// racing a paginating reader) never appears retroactively on the second
// page and never duplicates/reorders what the first page already returned
// — the property OFFSET-based pagination doesn't have.
func TestCursorPaginationStabilityUnderConcurrentInserts(t *testing.T) {
	gdb, st := newThreadTestEnv(t)
	ctx := context.Background()
	author, board := seedUserAndBoard(t, gdb)

	mkPost := func(title string, activityAt time.Time) *store.Post {
		p := &store.Post{
			BoardID: board.ID, AuthorID: author.ID, Title: title, BodyMD: "body",
			LastActivityAt: activityAt,
		}
		require.NoError(t, st.CreatePost(ctx, gdb, p))
		return p
	}

	base := time.Now().UTC().Truncate(time.Second)
	p1 := mkPost("post 1", base.Add(-3*time.Minute))
	p2 := mkPost("post 2", base.Add(-2*time.Minute))
	p3 := mkPost("post 3", base.Add(-1*time.Minute))

	// Page 1: newest first, page size 2 -> [p3, p2].
	page1, err := st.ListPostsByBoard(ctx, gdb, board.ID, nil, 2)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, p3.ID, page1[0].ID)
	require.Equal(t, p2.ID, page1[1].ID)

	cursor := &store.PostListCursor{LastActivityAt: page1[1].LastActivityAt, ID: page1[1].ID}

	// Simulate a concurrent writer: a brand new post with activity newer
	// than anything seen so far, inserted *after* page 1 was fetched.
	pNew := mkPost("post new (raced in)", base.Add(5*time.Minute))
	_ = pNew

	// Page 2, using the cursor taken before the race: must be exactly
	// [p1] — the raced-in post must not appear (it's newer than the
	// cursor, i.e. "before" it in DESC order, so the "< cursor" predicate
	// correctly excludes it), and p2/p3 must not repeat.
	page2, err := st.ListPostsByBoard(ctx, gdb, board.ID, cursor, 2)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Equal(t, p1.ID, page2[0].ID)
}

// TestCountsAndActivityMaintenance confirms BumpPostActivity keeps
// comment_count and last_activity_at consistent across several comments,
// matching what ThreadService.CreateComment calls inside its transaction.
func TestCountsAndActivityMaintenance(t *testing.T) {
	gdb, st := newThreadTestEnv(t)
	ctx := context.Background()
	author, board := seedUserAndBoard(t, gdb)
	post := seedPost(t, gdb, st, board, author)

	for i := 0; i < 3; i++ {
		id, err := store.NewCommentID()
		require.NoError(t, err)
		c := &store.Comment{
			ID: id, PostID: post.ID, AuthorID: author.ID,
			Path: store.ChildPath("", id), BodyMD: "reply",
		}
		require.NoError(t, st.CreateComment(ctx, gdb, c))
		require.NoError(t, st.BumpPostActivity(ctx, gdb, post.ID, time.Now().UTC(), 1))
		time.Sleep(2 * time.Millisecond)
	}

	var got store.Post
	require.NoError(t, gdb.First(&got, "id = ?", post.ID).Error)
	require.Equal(t, 3, got.CommentCount)
	require.True(t, got.LastActivityAt.After(post.LastActivityAt),
		"last_activity_at must have advanced past the post's own creation time")
}
