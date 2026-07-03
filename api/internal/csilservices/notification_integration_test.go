//go:build integration

// Service-layer integration coverage tying EndorsementService's real fan-out
// (notify.DBPublisher, not the capturingPublisher test double every other
// EndorsementService integration test uses) to NotificationService: the
// CSIL schema gap this task fixes was that NotificationEvent's wire type
// had no "endorsed" member even though the DB enum (coredb/migrations/
// 000003_notification_event_endorsed.sql) and the fan-out publisher
// (api/internal/notify/publisher.go's EventEndorsed) already produced it —
// so an endorsement notification could be written but never faithfully
// round-tripped back out through NotificationService. This test exercises
// the whole path: Endorse -> real DB fan-out -> ListNotifications, and also
// covers the actor_handle/actor_display_name denormalization added
// alongside it.
package csilservices_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

type notificationTestEnv struct {
	store        *store.Store
	endorsements csil.EndorsementService
	notification csil.NotificationService
}

func setupNotificationTestEnv(t *testing.T) *notificationTestEnv {
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

	pub := notify.NewDBPublisher()
	return &notificationTestEnv{
		store:        st,
		endorsements: csilservices.NewEndorsementService(st, pub),
		notification: csilservices.NewNotificationService(st),
	}
}

func (env *notificationTestEnv) createUser(t *testing.T, handle, displayName string) *store.User {
	t.Helper()
	u := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: handle,
		Handle:         handle,
		DisplayName:    displayName,
		Kind:           "human",
		Roles:          store.StringArray{},
	}
	require.NoError(t, env.store.DB.Create(u).Error)
	return u
}

func (env *notificationTestEnv) createBoard(t *testing.T, slug, createdBy string) *store.Board {
	t.Helper()
	b := &store.Board{Slug: slug, Title: slug, Kind: "discussion", CreatedBy: createdBy}
	require.NoError(t, env.store.DB.Create(b).Error)
	return b
}

func (env *notificationTestEnv) createPost(t *testing.T, boardID, authorID string) *store.Post {
	t.Helper()
	p := &store.Post{BoardID: boardID, AuthorID: authorID, Title: "t", BodyMD: "b", Origin: "user", LastActivityAt: time.Now().UTC()}
	require.NoError(t, env.store.DB.Create(p).Error)
	return p
}

// TestEndorsedNotification_RoundTripsThroughNotificationService is this
// task's headline regression test: an endorsement's fan-out notification
// (event "endorsed") must come back out of NotificationService.
// ListNotifications with that exact event value, plus the endorser's
// actor_handle/actor_display_name populated via the batched lookup added
// alongside it.
func TestEndorsedNotification_RoundTripsThroughNotificationService(t *testing.T) {
	env := setupNotificationTestEnv(t)

	author := env.createUser(t, "author", "Author Anders")
	endorser := env.createUser(t, "endorser", "Endorser Edwards")
	board := env.createBoard(t, "general", author.ID)
	post := env.createPost(t, board.ID, author.ID)

	_, err := env.endorsements.Endorse(asUser(endorser), csil.EndorseRequest{TargetType: "post", TargetId: post.ID})
	require.NoError(t, err)

	page, err := env.notification.ListNotifications(asUser(author), csil.ListNotificationsRequest{})
	require.NoError(t, err)
	require.Len(t, page.Notifications, 1)

	n := page.Notifications[0]
	require.Equal(t, csil.NotificationEvent("endorsed"), n.Event)
	require.NotNil(t, n.ActorId)
	require.Equal(t, csil.UserID(endorser.ID), *n.ActorId)
	require.NotNil(t, n.ActorHandle)
	require.Equal(t, "endorser", *n.ActorHandle)
	require.NotNil(t, n.ActorDisplayName)
	require.Equal(t, "Endorser Edwards", *n.ActorDisplayName)
}
