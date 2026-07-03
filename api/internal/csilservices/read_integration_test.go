//go:build integration

package csilservices_test

// See subscription_integration_test.go's package-level note: these tests
// aren't run by ./tools.sh test-integration (only internal/store and
// internal/server are), so verify with
// `go test -tags=integration ./internal/csilservices/...` directly. The
// full override-beats-watermark / mute-carve-out / summary-correctness
// matrix lives in api/internal/store/read_test.go, which IS gated by
// tools.sh; these tests cover the ReadService translation layer on top of
// it (anonymous no-ops, target validation, shaping store.BoardUnread into
// csil.UnreadSummary).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

func (env *b6TestEnv) createComment(t *testing.T, postID, authorID string) *store.Comment {
	t.Helper()
	ctx := context.Background()
	c := &store.Comment{
		PostID:   postID,
		AuthorID: authorID,
		Path:     store.Ltree(postID),
		BodyMD:   "a reply",
		Origin:   "user",
	}
	require.NoError(t, env.store.DB.WithContext(ctx).Create(c).Error)
	full := store.Ltree(postID + "." + c.ID)
	require.NoError(t, env.store.DB.WithContext(ctx).Model(&store.Comment{}).Where("id = ?", c.ID).Update("path", full).Error)
	c.Path = full
	return c
}

func TestReadServiceMarkReadAndUnreadSummary(t *testing.T) {
	env := setupB6Env(t)
	viewer := env.createUser(t, "viewer")
	author := env.createUser(t, "author")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)
	env.createComment(t, post.ID, author.ID)

	_, err := env.subs.Subscribe(asUser(viewer), csil.TargetRef{TargetType: "board", TargetId: board.ID})
	require.NoError(t, err)

	t.Run("never viewed: whole post is unread", func(t *testing.T) {
		summary, err := env.reads.UnreadSummary(asUser(viewer), csil.Empty{})
		require.NoError(t, err)
		require.Len(t, summary.Boards, 1)
		require.Equal(t, csil.BoardID(board.ID), summary.Boards[0].BoardId)
		require.EqualValues(t, 2, summary.Boards[0].UnreadCount) // root + 1 comment
		require.ElementsMatch(t, []csil.PostID{csil.PostID(post.ID)}, summary.Boards[0].UnreadPostIds)
	})

	t.Run("mark-read on the post clears it", func(t *testing.T) {
		_, err := env.reads.MarkRead(asUser(viewer), csil.TargetRef{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)

		summary, err := env.reads.UnreadSummary(asUser(viewer), csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, summary.Boards, "boards with nothing unread must be omitted, not zero")
	})

	t.Run("mark-unread on a comment pins it unread again", func(t *testing.T) {
		c := env.createComment(t, post.ID, author.ID)
		_, err := env.reads.MarkRead(asUser(viewer), csil.TargetRef{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)

		_, err = env.reads.MarkUnread(asUser(viewer), csil.TargetRef{TargetType: "comment", TargetId: c.ID})
		require.NoError(t, err)

		summary, err := env.reads.UnreadSummary(asUser(viewer), csil.Empty{})
		require.NoError(t, err)
		require.Len(t, summary.Boards, 1)
		require.EqualValues(t, 1, summary.Boards[0].UnreadCount)
	})

	t.Run("anonymous caller: no-op, no error", func(t *testing.T) {
		_, err := env.reads.MarkRead(anonymous(), csil.TargetRef{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)
		_, err = env.reads.MarkUnread(anonymous(), csil.TargetRef{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)
		summary, err := env.reads.UnreadSummary(anonymous(), csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, summary.Boards)
	})

	t.Run("unknown target is a no-op, not an error (infallible op)", func(t *testing.T) {
		_, err := env.reads.MarkRead(asUser(viewer), csil.TargetRef{TargetType: "post", TargetId: nilUUID})
		require.NoError(t, err)
		_, err = env.reads.MarkUnread(asUser(viewer), csil.TargetRef{TargetType: "comment", TargetId: nilUUID})
		require.NoError(t, err)
	})
}
