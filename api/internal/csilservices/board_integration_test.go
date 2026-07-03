//go:build integration

package csilservices_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// nilUUID is syntactically valid for boards/users' uuid-typed id columns
// but never assigned by generate_ulid(), so it's a safe stand-in for
// "a well-formed id that doesn't exist" in not-found assertions.
const nilUUID = "00000000-0000-0000-0000-000000000000"

// boardTestEnv boots a real BoardService (task B3) against a testcontainers
// Postgres, running coredb's goose migrations first — the same pattern
// api/internal/store/store_integration_test.go and
// api/internal/server/server_integration_test.go already use.
type boardTestEnv struct {
	svc   csil.BoardService
	store *store.Store
}

func setupBoardTestEnv(t *testing.T) *boardTestEnv {
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
	st := store.New(gdb)

	return &boardTestEnv{svc: csilservices.NewBoardService(st), store: st}
}

// createUser inserts a users row directly via the store (BoardService
// itself never creates users — that's AuthService/B2's job) and returns it.
func (env *boardTestEnv) createUser(t *testing.T, handle string, roles ...string) *store.User {
	t.Helper()
	if roles == nil {
		roles = []string{}
	}
	u := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: handle,
		Handle:         handle,
		Kind:           "human",
		Roles:          store.StringArray(roles),
	}
	require.NoError(t, env.store.DB.Create(u).Error)
	return u
}

func asUser(u *store.User) context.Context {
	return reqctx.WithUser(context.Background(), u)
}

func anonymous() context.Context {
	return context.Background()
}

func appErrorCode(t *testing.T, err error) uint64 {
	t.Helper()
	var appErr *csilservices.AppError
	require.True(t, errors.As(err, &appErr), "expected an *AppError, got %T: %v", err, err)
	return appErr.Code
}

// --- create-board ---

func TestBoardServiceCreateBoard(t *testing.T) {
	env := setupBoardTestEnv(t)
	admin := env.createUser(t, "admin", "admin")
	member := env.createUser(t, "member")

	t.Run("anonymous is unauthenticated", func(t *testing.T) {
		_, err := env.svc.CreateBoard(anonymous(), csil.CreateBoardRequest{
			Slug: "general", Title: "General", Kind: "discussion",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeUnauthenticated, appErrorCode(t, err))
	})

	t.Run("non-admin is forbidden", func(t *testing.T) {
		_, err := env.svc.CreateBoard(asUser(member), csil.CreateBoardRequest{
			Slug: "general", Title: "General", Kind: "discussion",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("admin creates a board", func(t *testing.T) {
		desc := "General discussion"
		board, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
			Slug: "general", Title: "General", Description: &desc, Kind: "discussion",
		})
		require.NoError(t, err)
		require.Equal(t, "general", board.Slug)
		require.Equal(t, "General", board.Title)
		require.Equal(t, csil.UserID(admin.ID), board.CreatedBy)
		require.NotEmpty(t, board.Id)
		require.Nil(t, board.ArchivedAt)
	})

	t.Run("duplicate slug conflicts", func(t *testing.T) {
		_, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
			Slug: "general", Title: "General Again", Kind: "discussion",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeConflict, appErrorCode(t, err))
	})

	t.Run("invalid slug is a validation error", func(t *testing.T) {
		_, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
			Slug: "AB", Title: "Bad Slug", Kind: "discussion",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeValidation, appErrorCode(t, err))
	})

	t.Run("invalid kind is a validation error", func(t *testing.T) {
		_, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
			Slug: "some-board", Title: "Some Board", Kind: "bogus",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeValidation, appErrorCode(t, err))
	})
}

// --- get-board ---

func TestBoardServiceGetBoard(t *testing.T) {
	env := setupBoardTestEnv(t)
	admin := env.createUser(t, "admin", "admin")

	board, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
		Slug: "announcements", Title: "Announcements", Kind: "announce",
	})
	require.NoError(t, err)

	t.Run("anonymous can read a board by slug", func(t *testing.T) {
		got, err := env.svc.GetBoard(anonymous(), csil.BoardSlug("announcements"))
		require.NoError(t, err)
		require.Equal(t, board.Id, got.Id)
	})

	t.Run("unknown slug is not found", func(t *testing.T) {
		_, err := env.svc.GetBoard(anonymous(), csil.BoardSlug("does-not-exist"))
		require.Error(t, err)
		require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
	})

	t.Run("archived board is still readable", func(t *testing.T) {
		_, err := env.svc.ArchiveBoard(asUser(admin), board.Id)
		require.NoError(t, err)

		got, err := env.svc.GetBoard(anonymous(), csil.BoardSlug("announcements"))
		require.NoError(t, err)
		require.NotNil(t, got.ArchivedAt)
	})
}

// --- list-boards / pagination ---

func TestBoardServiceListBoardsPagination(t *testing.T) {
	env := setupBoardTestEnv(t)
	admin := env.createUser(t, "admin", "admin")

	// Titles chosen to sort deterministically: alpha, bravo, charlie, delta.
	titles := []string{"alpha", "bravo", "charlie", "delta"}
	for _, title := range titles {
		_, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
			Slug: title, Title: title, Kind: "discussion",
		})
		require.NoError(t, err)
	}
	// A fifth board, archived — must never appear in list-boards.
	archived, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
		Slug: "echo", Title: "echo", Kind: "discussion",
	})
	require.NoError(t, err)
	_, err = env.svc.ArchiveBoard(asUser(admin), archived.Id)
	require.NoError(t, err)

	limit := uint64(2)
	var seen []string
	var cursor *csil.PageCursor
	for i := 0; i < 10; i++ { // bounded loop guards against an infinite-cursor bug
		page, err := env.svc.ListBoards(anonymous(), csil.ListBoardsRequest{Limit: &limit, Cursor: cursor})
		require.NoError(t, err)
		for _, b := range page.Boards {
			seen = append(seen, b.Slug)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}

	require.Equal(t, titles, seen, "expected all active boards, in title order, archived board excluded")
}

// --- update-board ---

func TestBoardServiceUpdateBoard(t *testing.T) {
	env := setupBoardTestEnv(t)
	admin := env.createUser(t, "admin", "admin")
	maintainer := env.createUser(t, "maintainer")
	moderator := env.createUser(t, "moderator")
	outsider := env.createUser(t, "outsider")

	board, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
		Slug: "project-x", Title: "Project X", Kind: "discussion",
	})
	require.NoError(t, err)

	_, err = env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
		BoardId: board.Id, UserId: csil.UserID(maintainer.ID), Role: "maintainer",
	})
	require.NoError(t, err)
	_, err = env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
		BoardId: board.Id, UserId: csil.UserID(moderator.ID), Role: "moderator",
	})
	require.NoError(t, err)

	t.Run("admin can update", func(t *testing.T) {
		newTitle := "Project X (renamed)"
		got, err := env.svc.UpdateBoard(asUser(admin), csil.UpdateBoardRequest{Id: board.Id, Title: &newTitle})
		require.NoError(t, err)
		require.Equal(t, newTitle, got.Title)
	})

	t.Run("the board's maintainer can update", func(t *testing.T) {
		newTitle := "Project X (maintainer edit)"
		got, err := env.svc.UpdateBoard(asUser(maintainer), csil.UpdateBoardRequest{Id: board.Id, Title: &newTitle})
		require.NoError(t, err)
		require.Equal(t, newTitle, got.Title)
	})

	t.Run("a moderator cannot update (maintainer-vs-moderator)", func(t *testing.T) {
		newTitle := "Project X (moderator edit)"
		_, err := env.svc.UpdateBoard(asUser(moderator), csil.UpdateBoardRequest{Id: board.Id, Title: &newTitle})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("a non-member is forbidden", func(t *testing.T) {
		newTitle := "Project X (outsider edit)"
		_, err := env.svc.UpdateBoard(asUser(outsider), csil.UpdateBoardRequest{Id: board.Id, Title: &newTitle})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("anonymous is unauthenticated", func(t *testing.T) {
		newTitle := "Project X (anon edit)"
		_, err := env.svc.UpdateBoard(anonymous(), csil.UpdateBoardRequest{Id: board.Id, Title: &newTitle})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeUnauthenticated, appErrorCode(t, err))
	})

	t.Run("unknown board is not found", func(t *testing.T) {
		// A syntactically valid but non-existent uuid: boards.id is a real
		// Postgres uuid column, so a non-uuid-shaped id (e.g. "nope") would
		// fail at the SQL type-cast layer rather than reaching the "not
		// found" path this subtest wants to exercise.
		newTitle := "whatever"
		_, err := env.svc.UpdateBoard(asUser(admin), csil.UpdateBoardRequest{Id: csil.BoardID(nilUUID), Title: &newTitle})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
	})
}

// --- archive-board ---

func TestBoardServiceArchiveBoard(t *testing.T) {
	env := setupBoardTestEnv(t)
	admin := env.createUser(t, "admin", "admin")
	maintainer := env.createUser(t, "maintainer")

	board, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
		Slug: "to-archive", Title: "To Archive", Kind: "discussion",
	})
	require.NoError(t, err)
	_, err = env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
		BoardId: board.Id, UserId: csil.UserID(maintainer.ID), Role: "maintainer",
	})
	require.NoError(t, err)

	t.Run("the board's own maintainer cannot archive (admin-only)", func(t *testing.T) {
		_, err := env.svc.ArchiveBoard(asUser(maintainer), board.Id)
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("admin archives", func(t *testing.T) {
		_, err := env.svc.ArchiveBoard(asUser(admin), board.Id)
		require.NoError(t, err)
	})

	t.Run("archiving twice conflicts", func(t *testing.T) {
		_, err := env.svc.ArchiveBoard(asUser(admin), board.Id)
		require.Error(t, err)
		require.Equal(t, csilservices.CodeConflict, appErrorCode(t, err))
	})
}

// --- set-board-member / remove-board-member ---

func TestBoardServiceSetAndRemoveBoardMember(t *testing.T) {
	env := setupBoardTestEnv(t)
	admin := env.createUser(t, "admin", "admin")
	maintainer := env.createUser(t, "maintainer")
	moderator := env.createUser(t, "moderator")
	other := env.createUser(t, "other")

	boardA, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
		Slug: "board-a", Title: "Board A", Kind: "discussion",
	})
	require.NoError(t, err)
	boardB, err := env.svc.CreateBoard(asUser(admin), csil.CreateBoardRequest{
		Slug: "board-b", Title: "Board B", Kind: "discussion",
	})
	require.NoError(t, err)

	t.Run("only admin may set-board-member, even the board's own maintainer cannot", func(t *testing.T) {
		_, err := env.svc.SetBoardMember(asUser(maintainer), csil.SetBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(other.ID), Role: "moderator",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("admin assigns maintainer and moderator", func(t *testing.T) {
		_, err := env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(maintainer.ID), Role: "maintainer",
		})
		require.NoError(t, err)
		_, err = env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(moderator.ID), Role: "moderator",
		})
		require.NoError(t, err)
	})

	t.Run("assigning a role twice is idempotent (a repeat assignment succeeds)", func(t *testing.T) {
		_, err := env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(maintainer.ID), Role: "maintainer",
		})
		require.NoError(t, err)
	})

	t.Run("invalid role is a validation error", func(t *testing.T) {
		_, err := env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(other.ID), Role: "owner",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeValidation, appErrorCode(t, err))
	})

	t.Run("unknown user is not found", func(t *testing.T) {
		_, err := env.svc.SetBoardMember(asUser(admin), csil.SetBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(nilUUID), Role: "moderator",
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
	})

	t.Run("a moderator cannot remove anyone (maintainer-vs-moderator)", func(t *testing.T) {
		_, err := env.svc.RemoveBoardMember(asUser(moderator), csil.RemoveBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(other.ID),
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("a maintainer cannot manage members on a board they don't maintain", func(t *testing.T) {
		_, err := env.svc.RemoveBoardMember(asUser(maintainer), csil.RemoveBoardMemberRequest{
			BoardId: boardB.Id, UserId: csil.UserID(moderator.ID),
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeForbidden, appErrorCode(t, err))
	})

	t.Run("the board's maintainer removes its moderator", func(t *testing.T) {
		_, err := env.svc.RemoveBoardMember(asUser(maintainer), csil.RemoveBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(moderator.ID),
		})
		require.NoError(t, err)
	})

	t.Run("removing a role that no longer exists is not found", func(t *testing.T) {
		_, err := env.svc.RemoveBoardMember(asUser(maintainer), csil.RemoveBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(moderator.ID),
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
	})

	t.Run("admin removes the maintainer itself", func(t *testing.T) {
		_, err := env.svc.RemoveBoardMember(asUser(admin), csil.RemoveBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(maintainer.ID),
		})
		require.NoError(t, err)
	})

	t.Run("anonymous is unauthenticated", func(t *testing.T) {
		_, err := env.svc.RemoveBoardMember(anonymous(), csil.RemoveBoardMemberRequest{
			BoardId: boardA.Id, UserId: csil.UserID(other.ID),
		})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeUnauthenticated, appErrorCode(t, err))
	})
}
