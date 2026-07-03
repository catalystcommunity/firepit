//go:build integration

package store_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// TestNotificationServicePaginationAndAuthz drives
// csilservices.NewNotificationService (task B7) directly against a real
// Postgres: cursor pagination newest-first, the unread_only filter, and
// authz scoping (a caller only ever sees/marks their own notifications; an
// anonymous caller sees/marks nothing).
func TestNotificationServicePaginationAndAuthz(t *testing.T) {
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
	svc := csilservices.NewNotificationService(st)

	alice := mustCreateNotifTestUser(t, gdb, "alice")
	bob := mustCreateNotifTestUser(t, gdb, "bob")

	// Seed 5 notifications for alice, strictly increasing created_at so
	// ordering/pagination is deterministic, plus one for bob to prove
	// scoping.
	base := time.Now().UTC().Add(-time.Hour)
	var aliceIDs []string
	for i := 0; i < 5; i++ {
		n := store.Notification{
			UserID:     alice.ID,
			Event:      "new_post",
			TargetType: "post",
			TargetID:   mustNewULID(t, gdb),
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, gdb.Create(&n).Error)
		aliceIDs = append(aliceIDs, n.ID)
	}
	bobNotif := store.Notification{
		UserID:     bob.ID,
		Event:      "new_post",
		TargetType: "post",
		TargetID:   mustNewULID(t, gdb),
		CreatedAt:  base,
	}
	require.NoError(t, gdb.Create(&bobNotif).Error)

	t.Run("anonymous caller gets an empty page", func(t *testing.T) {
		page, err := svc.ListNotifications(ctx, csil.ListNotificationsRequest{})
		require.NoError(t, err)
		require.Empty(t, page.Notifications)
		require.Nil(t, page.NextCursor)
	})

	t.Run("paginates newest-first across multiple pages", func(t *testing.T) {
		aliceCtx := reqctx.WithUser(ctx, alice)

		limit := uint64(2)
		var seen []string
		var cursor *csil.PageCursor
		for i := 0; i < 10; i++ { // bounded loop guard against an infinite-cursor bug
			page, err := svc.ListNotifications(aliceCtx, csil.ListNotificationsRequest{Limit: &limit, Cursor: cursor})
			require.NoError(t, err)
			for _, n := range page.Notifications {
				seen = append(seen, string(n.Id))
			}
			if page.NextCursor == nil {
				break
			}
			cursor = page.NextCursor
		}

		require.Len(t, seen, 5, "must see exactly alice's 5 notifications, none of bob's, none twice")
		// newest-first: reverse of insertion order.
		want := []string{aliceIDs[4], aliceIDs[3], aliceIDs[2], aliceIDs[1], aliceIDs[0]}
		require.Equal(t, want, seen)
	})

	t.Run("unread_only filters out read notifications", func(t *testing.T) {
		aliceCtx := reqctx.WithUser(ctx, alice)

		unreadOnly := true
		page, err := svc.ListNotifications(aliceCtx, csil.ListNotificationsRequest{UnreadOnly: &unreadOnly})
		require.NoError(t, err)
		require.Len(t, page.Notifications, 5)

		_, err = svc.MarkNotificationRead(aliceCtx, csil.NotificationIDs{csil.NotificationID(aliceIDs[0])})
		require.NoError(t, err)

		page, err = svc.ListNotifications(aliceCtx, csil.ListNotificationsRequest{UnreadOnly: &unreadOnly})
		require.NoError(t, err)
		require.Len(t, page.Notifications, 4)
		for _, n := range page.Notifications {
			require.NotEqual(t, aliceIDs[0], string(n.Id))
		}
	})

	t.Run("mark-notification-read only affects the caller's own rows", func(t *testing.T) {
		aliceCtx := reqctx.WithUser(ctx, alice)

		_, err := svc.MarkNotificationRead(aliceCtx, csil.NotificationIDs{csil.NotificationID(bobNotif.ID)})
		require.NoError(t, err)

		var got store.Notification
		require.NoError(t, gdb.First(&got, "id = ?", bobNotif.ID).Error)
		require.Nil(t, got.ReadAt, "alice must not be able to mark bob's notification read")
	})

	t.Run("mark-all-read only affects the caller's own rows", func(t *testing.T) {
		bobCtx := reqctx.WithUser(ctx, bob)

		_, err := svc.MarkAllRead(bobCtx, csil.Empty{})
		require.NoError(t, err)

		var gotBob store.Notification
		require.NoError(t, gdb.First(&gotBob, "id = ?", bobNotif.ID).Error)
		require.NotNil(t, gotBob.ReadAt)

		unreadOnly := true
		aliceCtx := reqctx.WithUser(ctx, alice)
		page, err := svc.ListNotifications(aliceCtx, csil.ListNotificationsRequest{UnreadOnly: &unreadOnly})
		require.NoError(t, err)
		require.NotEmpty(t, page.Notifications, "bob's mark-all-read must not touch alice's unread notifications")
	})

	t.Run("anonymous mark-read and mark-all-read are no-ops, not errors", func(t *testing.T) {
		_, err := svc.MarkNotificationRead(ctx, csil.NotificationIDs{csil.NotificationID(aliceIDs[1])})
		require.NoError(t, err)
		_, err = svc.MarkAllRead(ctx, csil.Empty{})
		require.NoError(t, err)

		var got store.Notification
		require.NoError(t, gdb.First(&got, "id = ?", aliceIDs[1]).Error)
		require.Nil(t, got.ReadAt, "an anonymous caller must not be able to mark anyone's notification read")
	})
}

func mustCreateNotifTestUser(t *testing.T, db *gorm.DB, handlePrefix string) *store.User {
	t.Helper()
	// See notify_fanout_integration_test.go's testIDCounter doc comment: a
	// truncated generate_ulid() collides under rapid sequential calls, so
	// uniqueness here comes from the same process-wide counter instead.
	suffix := strconv.FormatInt(testIDCounter.Add(1), 10)
	u := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: handlePrefix + "-" + suffix,
		Handle:         handlePrefix + "-" + suffix,
		Kind:           "human",
		Roles:          []string{},
	}
	require.NoError(t, db.Create(u).Error)
	return u
}

func mustNewULID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var id string
	require.NoError(t, db.Raw("SELECT generate_ulid()").Scan(&id).Error)
	return id
}
