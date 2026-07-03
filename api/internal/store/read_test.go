//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// seedReadFixture builds one board with one post authored by someone other
// than the viewer, ready for read/unread scenarios. Returns everything a
// test needs to extend it further.
type readFixture struct {
	viewer *store.User
	author *store.User
	board  *store.Board
	post   *store.Post
}

func seedReadFixture(t *testing.T, st *store.Store) readFixture {
	t.Helper()
	ctx := context.Background()

	viewer := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "viewer", Handle: "viewer", Kind: "human"}
	require.NoError(t, st.DB.WithContext(ctx).Create(viewer).Error)

	author := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "author", Handle: "author", Kind: "human"}
	require.NoError(t, st.DB.WithContext(ctx).Create(author).Error)

	board := &store.Board{Slug: "general", Title: "General", Kind: "discussion", CreatedBy: author.ID}
	require.NoError(t, st.DB.WithContext(ctx).Create(board).Error)

	post := &store.Post{BoardID: board.ID, AuthorID: author.ID, Title: "hello", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(post).Error)

	return readFixture{viewer: viewer, author: author, board: board, post: post}
}

// addComment inserts a top-level comment on postID (a direct child of the
// post root in ltree terms). Every comment needs its OWN id as the last
// label of its path (not just the post's id) for ltree ancestor/descendant
// queries (the muted-comment-subtree carve-out) to tell siblings apart —
// the id is only known after insert, so this creates the row, then fixes
// up its path in a second update.
func addComment(t *testing.T, st *store.Store, postID, authorID string) *store.Comment {
	t.Helper()
	return addReply(t, st, postID, store.Ltree(postID), authorID)
}

// addReply inserts a comment whose parent is parentPath (a full ltree path,
// e.g. another comment's Path) rather than the post root directly — for
// building nested threads in tests.
func addReply(t *testing.T, st *store.Store, postID string, parentPath store.Ltree, authorID string) *store.Comment {
	t.Helper()
	ctx := context.Background()
	c := &store.Comment{
		PostID:   postID,
		AuthorID: authorID,
		Path:     parentPath, // placeholder until the row's own id is known
		BodyMD:   "a reply",
		Origin:   "user",
	}
	require.NoError(t, st.DB.WithContext(ctx).Create(c).Error)

	full := store.Ltree(string(parentPath) + "." + c.ID)
	require.NoError(t, st.DB.WithContext(ctx).Model(&store.Comment{}).
		Where("id = ?", c.ID).Update("path", full).Error)
	c.Path = full
	return c
}

func summaryFor(t *testing.T, st *store.Store, userID string) map[string]store.BoardUnread {
	t.Helper()
	rows, err := st.UnreadSummary(context.Background(), userID)
	require.NoError(t, err)
	out := make(map[string]store.BoardUnread, len(rows))
	for _, r := range rows {
		out[r.BoardID] = r
	}
	return out
}

func TestUnreadSummary_NeverViewedPostIsFullyUnread(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)

	summary := summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok, "expected board to appear with unread activity")
	// root post (never viewed) + 1 comment = 2 unread items.
	require.EqualValues(t, 2, got.UnreadCount)
	require.ElementsMatch(t, []string{fx.post.ID}, []string(got.PostIDs))
}

func TestUnreadSummary_MarkReadAdvancesWatermark(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)

	require.NoError(t, st.MarkPostRead(ctx, fx.viewer.ID, fx.post.ID))

	summary := summaryFor(t, st, fx.viewer.ID)
	_, stillUnread := summary[fx.board.ID]
	require.False(t, stillUnread, "marking the post read should clear its unread contribution")

	// A new comment created after the watermark must show up again.
	time.Sleep(5 * time.Millisecond)
	addComment(t, st, fx.post.ID, fx.author.ID)

	summary = summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok)
	require.EqualValues(t, 1, got.UnreadCount)
}

func TestUnreadSummary_MarkReadClearsOverrides(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	c1 := addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)

	require.NoError(t, st.MarkPostRead(ctx, fx.viewer.ID, fx.post.ID))
	require.NoError(t, st.MarkUnread(ctx, fx.viewer.ID, "comment", c1.ID))

	// Sanity: the override alone should make the board show up as unread
	// even though the watermark already covers c1.
	summary := summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok)
	require.EqualValues(t, 1, got.UnreadCount)

	// mark-read on the whole post clears that override too.
	require.NoError(t, st.MarkPostRead(ctx, fx.viewer.ID, fx.post.ID))
	summary = summaryFor(t, st, fx.viewer.ID)
	_, stillUnread := summary[fx.board.ID]
	require.False(t, stillUnread, "mark-read on the post must clear overrides scoped to it")

	var overrideCount int64
	require.NoError(t, st.DB.WithContext(ctx).Model(&store.UnreadOverride{}).
		Where("user_id = ? AND target_type = 'comment' AND target_id = ?", fx.viewer.ID, c1.ID).
		Count(&overrideCount).Error)
	require.Zero(t, overrideCount)
}

func TestUnreadSummary_CommentMarkReadOnlyClearsThatComment(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	c1 := addComment(t, st, fx.post.ID, fx.author.ID)
	c2 := addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)

	require.NoError(t, st.MarkPostRead(ctx, fx.viewer.ID, fx.post.ID))
	require.NoError(t, st.MarkUnread(ctx, fx.viewer.ID, "comment", c1.ID))
	require.NoError(t, st.MarkUnread(ctx, fx.viewer.ID, "comment", c2.ID))

	// Clearing just c1's override must leave c2's pin (and the unread
	// count) alone.
	require.NoError(t, st.ClearCommentOverride(ctx, fx.viewer.ID, c1.ID))

	summary := summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok)
	require.EqualValues(t, 1, got.UnreadCount)

	var c1Overrides, c2Overrides int64
	require.NoError(t, st.DB.WithContext(ctx).Model(&store.UnreadOverride{}).
		Where("user_id = ? AND target_type = 'comment' AND target_id = ?", fx.viewer.ID, c1.ID).
		Count(&c1Overrides).Error)
	require.NoError(t, st.DB.WithContext(ctx).Model(&store.UnreadOverride{}).
		Where("user_id = ? AND target_type = 'comment' AND target_id = ?", fx.viewer.ID, c2.ID).
		Count(&c2Overrides).Error)
	require.Zero(t, c1Overrides)
	require.Equal(t, int64(1), c2Overrides)
}

func TestUnreadSummary_OverrideBeatsWatermark(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	c1 := addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)

	// Viewer reads the whole post: watermark now covers c1, nothing unread.
	require.NoError(t, st.MarkPostRead(ctx, fx.viewer.ID, fx.post.ID))
	summary := summaryFor(t, st, fx.viewer.ID)
	_, unread := summary[fx.board.ID]
	require.False(t, unread)

	// Explicitly pin c1 unread again — despite created_at being well before
	// the watermark, the override must beat it.
	require.NoError(t, st.MarkUnread(ctx, fx.viewer.ID, "comment", c1.ID))

	summary = summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok, "override must reintroduce unread activity despite an advanced watermark")
	require.EqualValues(t, 1, got.UnreadCount)
	require.ElementsMatch(t, []string{fx.post.ID}, []string(got.PostIDs))
}

func TestUnreadSummary_MuteCarveOutFromBoardSubscription(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)

	// A second, muted post in the same board.
	mutedPost := &store.Post{BoardID: fx.board.ID, AuthorID: fx.author.ID, Title: "noisy", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(mutedPost).Error)
	addComment(t, st, mutedPost.ID, fx.author.ID)
	addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)
	_, err = st.SetSubscriptionMuted(ctx, fx.viewer.ID, "post", mutedPost.ID, true)
	require.NoError(t, err)

	summary := summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok)
	// Only fx.post's root + its one comment should count; mutedPost (root +
	// comment) must be entirely carved out despite the board subscription.
	require.EqualValues(t, 2, got.UnreadCount)
	require.ElementsMatch(t, []string{fx.post.ID}, []string(got.PostIDs))
}

func TestUnreadSummary_MutedCommentSubtreeCarvesOutRegardlessOfScope(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)

	branch := addComment(t, st, fx.post.ID, fx.author.ID)
	// A reply under branch, sharing branch's path prefix.
	nested := addReply(t, st, fx.post.ID, branch.Path, fx.author.ID)
	other := addComment(t, st, fx.post.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "post", fx.post.ID)
	require.NoError(t, err)
	_, err = st.SetSubscriptionMuted(ctx, fx.viewer.ID, "comment", branch.ID, true)
	require.NoError(t, err)

	summary := summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok)
	// root(1) + other(1) count; branch + nested (its subtree) are muted out.
	require.EqualValues(t, 2, got.UnreadCount)
	_, _ = nested, other
}

func TestUnreadSummary_ExcludesOwnContent(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)

	// Viewer authors their own post in the same board — its root must never
	// count as unread for themselves.
	ownPost := &store.Post{BoardID: fx.board.ID, AuthorID: fx.viewer.ID, Title: "mine", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(ownPost).Error)
	// The viewer's own comment on someone else's post must not count either.
	addComment(t, st, fx.post.ID, fx.viewer.ID)
	// But someone else replying on the viewer's own post DOES count.
	addComment(t, st, ownPost.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)

	summary := summaryFor(t, st, fx.viewer.ID)
	got, ok := summary[fx.board.ID]
	require.True(t, ok)
	// fx.post root (never viewed, authored by fx.author) = 1
	// fx.post's viewer-authored comment = excluded
	// ownPost root (authored by viewer) = excluded
	// ownPost's comment by fx.author = 1
	require.EqualValues(t, 2, got.UnreadCount)
	require.ElementsMatch(t, []string{fx.post.ID, ownPost.ID}, []string(got.PostIDs))
}

func TestUnreadSummary_AcrossMultipleBoardsAndPosts(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	addComment(t, st, fx.post.ID, fx.author.ID)

	board2 := &store.Board{Slug: "second", Title: "Second", Kind: "discussion", CreatedBy: fx.author.ID}
	require.NoError(t, st.DB.WithContext(ctx).Create(board2).Error)
	post2 := &store.Post{BoardID: board2.ID, AuthorID: fx.author.ID, Title: "p2", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(post2).Error)
	addComment(t, st, post2.ID, fx.author.ID)
	addComment(t, st, post2.ID, fx.author.ID)

	// A third board the viewer never subscribes to must never appear.
	board3 := &store.Board{Slug: "third", Title: "Third", Kind: "discussion", CreatedBy: fx.author.ID}
	require.NoError(t, st.DB.WithContext(ctx).Create(board3).Error)
	post3 := &store.Post{BoardID: board3.ID, AuthorID: fx.author.ID, Title: "p3", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(post3).Error)
	addComment(t, st, post3.ID, fx.author.ID)

	_, err := st.Subscribe(ctx, fx.viewer.ID, "board", fx.board.ID)
	require.NoError(t, err)
	_, err = st.Subscribe(ctx, fx.viewer.ID, "post", post2.ID)
	require.NoError(t, err)

	summary := summaryFor(t, st, fx.viewer.ID)
	require.Len(t, summary, 2, "only the two subscribed-into boards should appear")

	b1, ok := summary[fx.board.ID]
	require.True(t, ok)
	require.EqualValues(t, 2, b1.UnreadCount) // root + 1 comment

	b2, ok := summary[board2.ID]
	require.True(t, ok)
	require.EqualValues(t, 3, b2.UnreadCount) // root + 2 comments

	_, sawBoard3 := summary[board3.ID]
	require.False(t, sawBoard3)
}

func TestMarkReadOnCommentIsIdempotent(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	c1 := addComment(t, st, fx.post.ID, fx.author.ID)

	// Clearing an override that was never set must succeed silently.
	require.NoError(t, st.ClearCommentOverride(ctx, fx.viewer.ID, c1.ID))
	require.NoError(t, st.ClearCommentOverride(ctx, fx.viewer.ID, c1.ID))
}

func TestMarkUnreadIsIdempotent(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	fx := seedReadFixture(t, st)
	c1 := addComment(t, st, fx.post.ID, fx.author.ID)

	require.NoError(t, st.MarkUnread(ctx, fx.viewer.ID, "comment", c1.ID))
	require.NoError(t, st.MarkUnread(ctx, fx.viewer.ID, "comment", c1.ID))

	var count int64
	require.NoError(t, st.DB.WithContext(ctx).Model(&store.UnreadOverride{}).
		Where("user_id = ? AND target_type = 'comment' AND target_id = ?", fx.viewer.ID, c1.ID).
		Count(&count).Error)
	require.Equal(t, int64(1), count)
}
