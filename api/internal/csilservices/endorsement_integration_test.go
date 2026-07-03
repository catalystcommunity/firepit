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
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// nilUUID (declared in board_integration_test.go) is reused here as the
// stand-in for "a well-formed id that doesn't exist" in not-found assertions.

// capturingPublisher (declared in thread_integration_test.go) is reused
// here to assert Endorse emits exactly one notify.KindEndorsed per actual
// new endorsement (never on an idempotent re-endorse).

// endorsementTestEnv boots a real Postgres (coredb migrations 000001 +
// 000002, so user_reputation exists), a *store.Store against it, and the
// real EndorsementService (task B5) wired to a capturingPublisher — the
// same testcontainers pattern
// api/internal/store/store_integration_test.go,
// api/internal/server/server_integration_test.go, and
// api/internal/csilservices/board_integration_test.go already use.
type endorsementTestEnv struct {
	st  *store.Store
	svc csil.EndorsementService
	pub *capturingPublisher
	db  *gorm.DB
}

func setupEndorsementTestEnv(t *testing.T) *endorsementTestEnv {
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

	pub := &capturingPublisher{}
	svc := csilservices.NewEndorsementService(st, pub)

	return &endorsementTestEnv{st: st, svc: svc, pub: pub, db: gdb}
}

func (env *endorsementTestEnv) createUser(t *testing.T, domain, handle string) *store.User {
	t.Helper()
	u := &store.User{LinkkeysDomain: domain, LinkkeysUserID: handle, Handle: handle, Kind: "human"}
	require.NoError(t, env.db.Create(u).Error)
	return u
}

func (env *endorsementTestEnv) createBoard(t *testing.T, slug, createdBy string) *store.Board {
	t.Helper()
	b := &store.Board{Slug: slug, Title: slug, Kind: "discussion", CreatedBy: createdBy}
	require.NoError(t, env.db.Create(b).Error)
	return b
}

func (env *endorsementTestEnv) createPost(t *testing.T, boardID, authorID string) *store.Post {
	t.Helper()
	p := &store.Post{BoardID: boardID, AuthorID: authorID, Title: "t", BodyMD: "b", Origin: "user", LastActivityAt: time.Now().UTC()}
	require.NoError(t, env.db.Create(p).Error)
	return p
}

func (env *endorsementTestEnv) createComment(t *testing.T, postID, authorID string) *store.Comment {
	t.Helper()
	c := &store.Comment{PostID: postID, AuthorID: authorID, Path: store.Ltree(postID), BodyMD: "c", Origin: "user"}
	require.NoError(t, env.db.Create(c).Error)
	return c
}

func (env *endorsementTestEnv) setBoardRole(t *testing.T, boardID, userID, role string) {
	t.Helper()
	require.NoError(t, env.db.Create(&store.BoardMember{BoardID: boardID, UserID: userID, Role: role}).Error)
}

func (env *endorsementTestEnv) trustDomain(t *testing.T, domain, addedBy string) {
	t.Helper()
	require.NoError(t, env.db.Create(&store.TrustedDomain{Domain: domain, AddedBy: addedBy}).Error)
}

func (env *endorsementTestEnv) friendOf(t *testing.T, ownerID, memberID string) {
	t.Helper()
	g := &store.FriendGroup{OwnerID: ownerID, Name: "friends"}
	require.NoError(t, env.db.Create(g).Error)
	require.NoError(t, env.db.Create(&store.FriendGroupMember{GroupID: g.ID, MemberUserID: memberID}).Error)
}

func (env *endorsementTestEnv) reputationOf(t *testing.T, userID string) int {
	t.Helper()
	count, err := env.st.ReputationCount(context.Background(), env.db, userID)
	require.NoError(t, err)
	return count
}

// asUser / anonymous helpers are shared with board_integration_test.go.

// TestEndorsementOrderingMatrix covers B5's acceptance criterion
// verbatim: an ordering matrix of friend vs. maintainer vs. trusted-domain
// reputation vs. plain ("nobody"), from both a viewer with friends and an
// anonymous viewer.
func TestEndorsementOrderingMatrix(t *testing.T) {
	env := setupEndorsementTestEnv(t)

	author := env.createUser(t, "example.com", "author")
	viewer := env.createUser(t, "example.com", "viewer")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)

	friendUser := env.createUser(t, "example.com", "friend")
	maintainerUser := env.createUser(t, "example.com", "maintainer")
	trustedRepUser := env.createUser(t, "example.com", "trusted-rep")
	plainUser := env.createUser(t, "example.com", "plain")

	// viewer considers friendUser a friend.
	env.friendOf(t, viewer.ID, friendUser.ID)

	// maintainerUser holds a board role on the target's board.
	env.setBoardRole(t, board.ID, maintainerUser.ID, "maintainer")

	// trustedRepUser has accumulated general reputation: some other user
	// from a trusted domain endorsed one of trustedRepUser's OWN posts
	// (elsewhere, nothing to do with the post under test here).
	trustedDomainUser := env.createUser(t, "trusted.example", "trusted-endorser")
	env.trustDomain(t, "trusted.example", author.ID)
	otherPost := env.createPost(t, board.ID, trustedRepUser.ID)
	_, err := env.svc.Endorse(asUser(trustedDomainUser), csil.EndorseRequest{TargetType: "post", TargetId: otherPost.ID})
	require.NoError(t, err)
	require.Equal(t, 1, env.reputationOf(t, trustedRepUser.ID), "trustedRepUser should have gained reputation from a trusted-domain endorsement of their own post")

	// Now all four endorse the post under test, in a fixed order so the
	// created_at tiebreak is deterministic if two ever land in the same
	// tier.
	for _, u := range []*store.User{plainUser, trustedRepUser, maintainerUser, friendUser} {
		_, err := env.svc.Endorse(asUser(u), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)
	}

	t.Run("viewer with a friend sees friend, then maintainer, then trusted-rep, then plain", func(t *testing.T) {
		list, err := env.svc.ListEndorsements(asUser(viewer), csil.TargetRef{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)
		require.Len(t, list.Endorsements, 4)

		var order []string
		for _, e := range list.Endorsements {
			order = append(order, string(e.UserId))
		}
		require.Equal(t, []string{friendUser.ID, maintainerUser.ID, trustedRepUser.ID, plainUser.ID}, order)

		// Maintainer's endorsement is role-badged; nobody else's is.
		for _, e := range list.Endorsements {
			if string(e.UserId) == maintainerUser.ID {
				require.NotNil(t, e.RoleBadge)
				require.Equal(t, "maintainer", *e.RoleBadge)
			} else {
				require.Nil(t, e.RoleBadge)
			}
		}
	})

	t.Run("anonymous viewer gets reputation-only ordering (no friend tier)", func(t *testing.T) {
		list, err := env.svc.ListEndorsements(context.Background(), csil.TargetRef{TargetType: "post", TargetId: post.ID})
		require.NoError(t, err)
		require.Len(t, list.Endorsements, 4)

		var order []string
		for _, e := range list.Endorsements {
			order = append(order, string(e.UserId))
		}
		// friendUser has no board role/reputation of their own, so without
		// the friend tier they fall back among the "plain" endorsers,
		// ordered by endorsement creation time (plainUser endorsed first).
		require.Equal(t, []string{maintainerUser.ID, trustedRepUser.ID, plainUser.ID, friendUser.ID}, order)
	})
}

// TestEndorseIdempotent covers "re-endorsing is a no-op success": a second
// endorse call by the same user on the same target returns the same
// endorsement, and does not double-count reputation or double-publish the
// notification.
func TestEndorseIdempotent(t *testing.T) {
	env := setupEndorsementTestEnv(t)

	author := env.createUser(t, "example.com", "author")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)

	endorser := env.createUser(t, "trusted.example", "endorser")
	env.trustDomain(t, "trusted.example", author.ID)

	first, err := env.svc.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)

	second, err := env.svc.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)

	require.Equal(t, first.Id, second.Id, "re-endorsing must return the same endorsement row")
	require.Equal(t, 1, env.reputationOf(t, author.ID), "reputation must not double-count a no-op re-endorse")
	require.Len(t, env.pub.events, 1, "notify.KindEndorsed must fire exactly once, not once per idempotent call")

	list, err := env.svc.ListEndorsements(context.Background(), csil.TargetRef{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)
	require.Len(t, list.Endorsements, 1, "idempotent re-endorse must not create a second row")
}

// TestEndorseRejectsOwnContent covers "cannot endorse own ... content".
func TestEndorseRejectsOwnContent(t *testing.T) {
	env := setupEndorsementTestEnv(t)

	author := env.createUser(t, "example.com", "author")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)

	_, err := env.svc.Endorse(asUser(author), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.Error(t, err)
	var appErr *csilservices.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, csilservices.CodeForbidden, appErr.Code)
}

// TestEndorseRejectsDeletedContent covers "cannot endorse ... deleted
// content", for both a tombstoned post and a tombstoned comment.
func TestEndorseRejectsDeletedContent(t *testing.T) {
	env := setupEndorsementTestEnv(t)

	author := env.createUser(t, "example.com", "author")
	endorser := env.createUser(t, "example.com", "endorser")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)

	now := time.Now().UTC()
	require.NoError(t, env.db.Model(&store.Post{}).Where("id = ?", post.ID).Update("deleted_at", now).Error)

	_, err := env.svc.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.Error(t, err)
	var appErr *csilservices.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, csilservices.CodeConflict, appErr.Code)

	livePost := env.createPost(t, board.ID, author.ID)
	comment := env.createComment(t, livePost.ID, author.ID)
	require.NoError(t, env.db.Model(&store.Comment{}).Where("id = ?", comment.ID).Update("deleted_at", now).Error)

	_, err = env.svc.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "comment", TargetId: comment.ID})
	require.Error(t, err)
	appErr = nil
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, csilservices.CodeConflict, appErr.Code)
}

// TestEndorseRejectsMissingTarget covers "target must exist".
func TestEndorseRejectsMissingTarget(t *testing.T) {
	env := setupEndorsementTestEnv(t)
	endorser := env.createUser(t, "example.com", "endorser")

	_, err := env.svc.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: nilUUID})
	require.Error(t, err)
	var appErr *csilservices.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, csilservices.CodeNotFound, appErr.Code)
}

// TestRetractIdempotentAndReputationDecrements covers retract's
// idempotency and the reputation cache's decrement-on-retract semantics.
func TestRetractIdempotentAndReputationDecrements(t *testing.T) {
	env := setupEndorsementTestEnv(t)

	author := env.createUser(t, "example.com", "author")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)

	endorser := env.createUser(t, "trusted.example", "endorser")
	env.trustDomain(t, "trusted.example", author.ID)

	_, err := env.svc.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)
	require.Equal(t, 1, env.reputationOf(t, author.ID))

	_, err = env.svc.Retract(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)
	require.Equal(t, 0, env.reputationOf(t, author.ID), "retracting a trusted-domain endorsement must decrement reputation")

	list, err := env.svc.ListEndorsements(context.Background(), csil.TargetRef{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)
	require.Empty(t, list.Endorsements)

	// Retracting again (nothing left to retract) is a no-op success, not
	// an error, and must not drive reputation negative.
	_, err = env.svc.Retract(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)
	require.Equal(t, 0, env.reputationOf(t, author.ID))
}
