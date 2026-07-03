//go:build integration

// Service-layer integration coverage for ThreadService: authz, validation,
// revision snapshots, tombstone redaction, and notify.Publisher emission —
// everything above the store layer (api/internal/store/thread_integration_test.go
// covers tree ordering/cursor stability/counts at that layer).
//
// NOTE: ./tools.sh test-integration only globs api/internal/store/... and
// api/internal/server/...; it doesn't yet cover api/internal/csilservices.
// Run this file directly:
//
//	go test -tags=integration ./internal/csilservices/...
package csilservices_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// capturingPublisher records every notify.Event handed to it so tests can
// assert ThreadService publishes the right event, inside the same
// transaction, without needing the real B7 fan-out.
type capturingPublisher struct {
	mu     sync.Mutex
	events []notify.Event
}

func (p *capturingPublisher) Publish(_ context.Context, _ *gorm.DB, e notify.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
	return nil
}

func (p *capturingPublisher) all() []notify.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]notify.Event, len(p.events))
	copy(out, p.events)
	return out
}

// threadServiceEnv boots a testcontainers Postgres, migrates it, and
// returns a ThreadService plus direct store access for seeding/assertions
// and the capturingPublisher backing it.
func threadServiceEnv(t *testing.T) (csil.ThreadService, *store.Store, *capturingPublisher) {
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
	t.Cleanup(func() { require.NoError(t, pgContainer.Terminate(ctx)) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, coredb.Up(dsn))

	gdb, err := store.Open(dsn)
	require.NoError(t, err)
	st := store.New(gdb)

	pub := &capturingPublisher{}
	return csilservices.NewThreadService(st, pub), st, pub
}

func mkUser(t *testing.T, st *store.Store, handle string, roles ...string) *store.User {
	t.Helper()
	u := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: handle, Handle: handle, Kind: "human", Roles: roles}
	require.NoError(t, st.DB.Create(u).Error)
	return u
}

func mkBoard(t *testing.T, st *store.Store, slug string, creator *store.User) *store.Board {
	t.Helper()
	b := &store.Board{Slug: slug, Title: slug, Kind: "discussion", CreatedBy: creator.ID}
	require.NoError(t, st.DB.Create(b).Error)
	return b
}

// asUser is shared with board_integration_test.go.

func TestCreatePost_ValidationAndAuthz(t *testing.T) {
	svc, st, pub := threadServiceEnv(t)
	author := mkUser(t, st, "author")
	board := mkBoard(t, st, "general", author)

	t.Run("anonymous rejected", func(t *testing.T) {
		_, err := svc.CreatePost(context.Background(), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "t", BodyMd: "b"})
		requireAppErrorCode(t, err, csilservices.CodeUnauthenticated)
	})

	t.Run("blank title rejected", func(t *testing.T) {
		_, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "   ", BodyMd: "b"})
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("title over the product cap rejected", func(t *testing.T) {
		long := make([]byte, 201)
		for i := range long {
			long[i] = 'x'
		}
		_, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: string(long), BodyMd: "b"})
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("unknown board", func(t *testing.T) {
		_, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: "0198a1b2-3c4d-4e5f-8a9b-0123456789ab", Title: "t", BodyMd: "b"})
		requireAppErrorCode(t, err, csilservices.CodeNotFound)
	})

	t.Run("archived board rejected", func(t *testing.T) {
		archived := time.Now().UTC()
		archivedBoard := &store.Board{Slug: "archived", Title: "archived", Kind: "discussion", CreatedBy: author.ID, ArchivedAt: &archived}
		require.NoError(t, st.DB.Create(archivedBoard).Error)

		_, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(archivedBoard.ID), Title: "t", BodyMd: "b"})
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("happy path publishes ContentCreated", func(t *testing.T) {
		post, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "  Hello  ", BodyMd: "hello world"})
		require.NoError(t, err)
		require.Equal(t, "Hello", string(post.Title), "title must be trimmed")
		require.Equal(t, csil.UserID(author.ID), post.AuthorId)
		require.NotEmpty(t, post.Id)
		require.False(t, post.LastActivityAt.IsZero())

		events := pub.all()
		require.NotEmpty(t, events)
		last := events[len(events)-1]
		require.Equal(t, notify.KindContentCreated, last.Kind)
		require.Equal(t, author.ID, last.ActorID)
		require.Equal(t, board.ID, last.BoardID)
		require.Equal(t, string(post.Id), last.PostID)
		require.Empty(t, last.CommentID)
		require.Equal(t, "hello world", last.Body)
	})
}

func TestCreateComment_ValidationAndAuthz(t *testing.T) {
	svc, st, pub := threadServiceEnv(t)
	author := mkUser(t, st, "author")
	board := mkBoard(t, st, "general", author)
	post, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "t", BodyMd: "b"})
	require.NoError(t, err)

	t.Run("anonymous rejected", func(t *testing.T) {
		_, err := svc.CreateComment(context.Background(), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "reply"})
		requireAppErrorCode(t, err, csilservices.CodeUnauthenticated)
	})

	t.Run("blank body rejected", func(t *testing.T) {
		_, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "   "})
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("unknown post", func(t *testing.T) {
		_, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: "0198a1b2-3c4d-4e5f-8a9b-0123456789ab", BodyMd: "reply"})
		requireAppErrorCode(t, err, csilservices.CodeNotFound)
	})

	var top csil.Comment
	t.Run("happy path top-level publishes ContentCreated", func(t *testing.T) {
		var err error
		top, err = svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "top level"})
		require.NoError(t, err)
		require.Nil(t, top.ParentCommentId)

		events := pub.all()
		last := events[len(events)-1]
		require.Equal(t, notify.KindContentCreated, last.Kind)
		require.Equal(t, string(post.Id), last.PostID)
		require.Equal(t, string(top.Id), last.CommentID)
		require.Equal(t, board.ID, last.BoardID)
	})

	t.Run("nested reply carries parent", func(t *testing.T) {
		nested, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{
			PostId: post.Id, ParentCommentId: &top.Id, BodyMd: "nested reply",
		})
		require.NoError(t, err)
		require.NotNil(t, nested.ParentCommentId)
		require.Equal(t, top.Id, *nested.ParentCommentId)
	})

	t.Run("parent from a different post rejected", func(t *testing.T) {
		otherPost, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "other", BodyMd: "b"})
		require.NoError(t, err)
		_, err = svc.CreateComment(asUser(author), csil.CreateCommentRequest{
			PostId: otherPost.Id, ParentCommentId: &top.Id, BodyMd: "cross-post reply",
		})
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("reply to a deleted parent rejected", func(t *testing.T) {
		toDelete, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "will be deleted"})
		require.NoError(t, err)
		_, err = svc.DeleteComment(asUser(author), toDelete.Id)
		require.NoError(t, err)

		_, err = svc.CreateComment(asUser(author), csil.CreateCommentRequest{
			PostId: post.Id, ParentCommentId: &toDelete.Id, BodyMd: "reply to tombstone",
		})
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("comment bumps post comment_count and last_activity_at", func(t *testing.T) {
		before, err := st.GetPost(context.Background(), st.DB, string(post.Id))
		require.NoError(t, err)

		_, err = svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "bump check"})
		require.NoError(t, err)

		after, err := st.GetPost(context.Background(), st.DB, string(post.Id))
		require.NoError(t, err)
		require.Greater(t, after.CommentCount, before.CommentCount)
		require.True(t, after.LastActivityAt.After(before.LastActivityAt) || after.LastActivityAt.Equal(before.LastActivityAt))
	})
}

func TestEditPostAuthorOnlyWithRevisionSnapshot(t *testing.T) {
	svc, st, pub := threadServiceEnv(t)
	author := mkUser(t, st, "author")
	stranger := mkUser(t, st, "stranger")
	board := mkBoard(t, st, "general", author)
	post, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "orig title", BodyMd: "orig body"})
	require.NoError(t, err)

	t.Run("non-author forbidden", func(t *testing.T) {
		_, err := svc.EditPost(asUser(stranger), csil.EditPostRequest{Id: post.Id, Title: "hacked", BodyMd: "hacked"})
		requireAppErrorCode(t, err, csilservices.CodeForbidden)
	})

	edited, err := svc.EditPost(asUser(author), csil.EditPostRequest{Id: post.Id, Title: "new title", BodyMd: "new body"})
	require.NoError(t, err)
	require.Equal(t, "new title", string(edited.Title))
	require.NotNil(t, edited.EditedAt)

	revisions, err := svc.ListRevisions(context.Background(), csil.TargetRef{TargetType: "post", TargetId: string(post.Id)})
	require.NoError(t, err)
	require.Len(t, revisions.Revisions, 1)
	require.NotNil(t, revisions.Revisions[0].PrevTitle)
	require.Equal(t, "orig title", *revisions.Revisions[0].PrevTitle)
	require.Equal(t, "orig body", revisions.Revisions[0].PrevBodyMd)

	events := pub.all()
	last := events[len(events)-1]
	require.Equal(t, notify.KindContentEdited, last.Kind)
	require.Equal(t, "new body", last.Body)

	// A second edit must snapshot the state right before ITSELF (i.e. "new
	// body"), and list-revisions must return both, most recent first.
	_, err = svc.EditPost(asUser(author), csil.EditPostRequest{Id: post.Id, Title: "third title", BodyMd: "third body"})
	require.NoError(t, err)
	revisions, err = svc.ListRevisions(context.Background(), csil.TargetRef{TargetType: "post", TargetId: string(post.Id)})
	require.NoError(t, err)
	require.Len(t, revisions.Revisions, 2)
	require.Equal(t, "new body", revisions.Revisions[0].PrevBodyMd)
	require.Equal(t, "orig body", revisions.Revisions[1].PrevBodyMd)
}

func TestEditComment_AuthorOnlyWithRevisionSnapshot(t *testing.T) {
	svc, st, _ := threadServiceEnv(t)
	author := mkUser(t, st, "author")
	stranger := mkUser(t, st, "stranger")
	board := mkBoard(t, st, "general", author)
	post, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "t", BodyMd: "b"})
	require.NoError(t, err)
	comment, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "orig comment body"})
	require.NoError(t, err)

	_, err = svc.EditComment(asUser(stranger), csil.EditCommentRequest{Id: comment.Id, BodyMd: "hacked"})
	requireAppErrorCode(t, err, csilservices.CodeForbidden)

	edited, err := svc.EditComment(asUser(author), csil.EditCommentRequest{Id: comment.Id, BodyMd: "new comment body"})
	require.NoError(t, err)
	require.Equal(t, "new comment body", edited.BodyMd)

	revisions, err := svc.ListRevisions(context.Background(), csil.TargetRef{TargetType: "comment", TargetId: string(comment.Id)})
	require.NoError(t, err)
	require.Len(t, revisions.Revisions, 1)
	require.Nil(t, revisions.Revisions[0].PrevTitle, "comments have no title")
	require.Equal(t, "orig comment body", revisions.Revisions[0].PrevBodyMd)
}

func TestDeletePost_Authz(t *testing.T) {
	svc, st, _ := threadServiceEnv(t)
	author := mkUser(t, st, "author")
	stranger := mkUser(t, st, "stranger")
	moderator := mkUser(t, st, "moderator")
	admin := mkUser(t, st, "admin-user", "admin")
	board := mkBoard(t, st, "general", author)
	require.NoError(t, st.DB.Create(&store.BoardMember{BoardID: board.ID, UserID: moderator.ID, Role: "moderator"}).Error)

	mkPost := func() csil.Post {
		p, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "t", BodyMd: "b"})
		require.NoError(t, err)
		return p
	}

	t.Run("stranger forbidden", func(t *testing.T) {
		p := mkPost()
		_, err := svc.DeletePost(asUser(stranger), p.Id)
		requireAppErrorCode(t, err, csilservices.CodeForbidden)
	})

	t.Run("author allowed", func(t *testing.T) {
		p := mkPost()
		_, err := svc.DeletePost(asUser(author), p.Id)
		require.NoError(t, err)
	})

	t.Run("board moderator allowed", func(t *testing.T) {
		p := mkPost()
		_, err := svc.DeletePost(asUser(moderator), p.Id)
		require.NoError(t, err)
	})

	t.Run("instance admin allowed", func(t *testing.T) {
		p := mkPost()
		_, err := svc.DeletePost(asUser(admin), p.Id)
		require.NoError(t, err)
	})

	t.Run("idempotent double delete", func(t *testing.T) {
		p := mkPost()
		_, err := svc.DeletePost(asUser(author), p.Id)
		require.NoError(t, err)
		_, err = svc.DeletePost(asUser(author), p.Id)
		require.NoError(t, err, "deleting an already-deleted post is a no-op success")
	})

	t.Run("unknown post", func(t *testing.T) {
		_, err := svc.DeletePost(asUser(author), "0198a1b2-3c4d-4e5f-8a9b-0123456789ab")
		requireAppErrorCode(t, err, csilservices.CodeNotFound)
	})
}

func TestGetThread_TombstoneRedaction(t *testing.T) {
	svc, st, _ := threadServiceEnv(t)
	author := mkUser(t, st, "author")
	board := mkBoard(t, st, "general", author)
	post, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "t", BodyMd: "b"})
	require.NoError(t, err)

	root, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "root secret body"})
	require.NoError(t, err)
	child, err := svc.CreateComment(asUser(author), csil.CreateCommentRequest{PostId: post.Id, ParentCommentId: &root.Id, BodyMd: "child body"})
	require.NoError(t, err)

	_, err = svc.DeleteComment(asUser(author), root.Id)
	require.NoError(t, err)

	thread, err := svc.GetThread(context.Background(), csil.GetThreadRequest{PostId: post.Id})
	require.NoError(t, err)
	require.Len(t, thread.Comments, 2)

	byID := map[csil.CommentID]csil.Comment{}
	for _, c := range thread.Comments {
		byID[c.Id] = c
	}

	gotRoot := byID[root.Id]
	require.NotNil(t, gotRoot.DeletedAt)
	require.NotEqual(t, "root secret body", gotRoot.BodyMd, "tombstoned comment's body must be redacted")
	require.Equal(t, csil.UserID(""), gotRoot.AuthorId, "tombstoned comment's author must be redacted")

	gotChild := byID[child.Id]
	require.Nil(t, gotChild.DeletedAt)
	require.Equal(t, "child body", gotChild.BodyMd, "surviving child's body must be untouched")
	require.NotNil(t, gotChild.ParentCommentId)
	require.Equal(t, root.Id, *gotChild.ParentCommentId, "child's structural link to its (now tombstoned) parent must survive")
}

func TestGetThread_UnknownPostReturnsEmptyThread(t *testing.T) {
	svc, _, _ := threadServiceEnv(t)
	thread, err := svc.GetThread(context.Background(), csil.GetThreadRequest{PostId: "0198a1b2-3c4d-4e5f-8a9b-0123456789ab"})
	require.NoError(t, err, "get-thread has no declared ServiceError arm; an unknown post must not error")
	require.Empty(t, thread.Post.Id)
	require.Empty(t, thread.Comments)
}

// TestListPostsAndGetThread_PopulateAuthorHandle covers the CSIL schema gap
// fixed alongside resolve-user: Post/Comment now carry an optional
// author_handle, denormalized server-side via a single batched
// store.GetUsersByIDs lookup (never one query per row — see
// ListPosts/GetThread's doc comments) so the UI can render a real name
// instead of hashing the bare author id into a fake label.
func TestListPostsAndGetThread_PopulateAuthorHandle(t *testing.T) {
	svc, st, _ := threadServiceEnv(t)
	author := mkUser(t, st, "carol")
	replier := mkUser(t, st, "dave")
	board := mkBoard(t, st, "general", author)

	post, err := svc.CreatePost(asUser(author), csil.CreatePostRequest{BoardId: csil.BoardID(board.ID), Title: "t", BodyMd: "b"})
	require.NoError(t, err)
	require.NotNil(t, post.AuthorHandle)
	require.Equal(t, "carol", *post.AuthorHandle)

	comment, err := svc.CreateComment(asUser(replier), csil.CreateCommentRequest{PostId: post.Id, BodyMd: "reply"})
	require.NoError(t, err)
	require.NotNil(t, comment.AuthorHandle)
	require.Equal(t, "dave", *comment.AuthorHandle)

	page, err := svc.ListPosts(context.Background(), csil.ListPostsRequest{BoardId: csil.BoardID(board.ID)})
	require.NoError(t, err)
	require.Len(t, page.Posts, 1)
	require.NotNil(t, page.Posts[0].AuthorHandle)
	require.Equal(t, "carol", *page.Posts[0].AuthorHandle)

	thread, err := svc.GetThread(context.Background(), csil.GetThreadRequest{PostId: post.Id})
	require.NoError(t, err)
	require.NotNil(t, thread.Post.AuthorHandle)
	require.Equal(t, "carol", *thread.Post.AuthorHandle)
	require.Len(t, thread.Comments, 1)
	require.NotNil(t, thread.Comments[0].AuthorHandle)
	require.Equal(t, "dave", *thread.Comments[0].AuthorHandle)
}

// requireAppErrorCode is shared with integration_integration_test.go.
