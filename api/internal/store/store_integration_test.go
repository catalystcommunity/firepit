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
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// TestStoreRoundTrip runs coredb's goose migrations up against a
// testcontainers Postgres, round-trips a minimal row through every gorm
// model in api/internal/store (insert, read back, compare), then runs
// goose back down to zero and confirms the schema is gone.
func TestStoreRoundTrip(t *testing.T) {
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

	// goose up: baseline schema exists.
	require.NoError(t, coredb.Up(dsn))

	gdb, err := store.Open(dsn)
	require.NoError(t, err)

	roundTripModels(t, gdb)

	// goose down to zero: schema is gone cleanly.
	require.NoError(t, coredb.Reset(dsn))

	var tableCount int64
	require.NoError(t, gdb.Raw(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name != 'goose_db_version'
	`).Scan(&tableCount).Error)
	require.Zero(t, tableCount, "expected no user tables left after goose down to zero")
}

func roundTripModels(t *testing.T, db *gorm.DB) {
	t.Helper()

	user := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: "user-1",
		Handle:         "alice",
		DisplayName:    "Alice",
		Kind:           "human",
		Roles:          []string{"member"},
	}
	require.NoError(t, db.Create(user).Error)
	require.NotEmpty(t, user.ID)

	var gotUser store.User
	require.NoError(t, db.First(&gotUser, "id = ?", user.ID).Error)
	require.Equal(t, user.Handle, gotUser.Handle)
	require.Equal(t, user.LinkkeysDomain, gotUser.LinkkeysDomain)
	require.Equal(t, []string(user.Roles), []string(gotUser.Roles))

	editor := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: "user-2",
		Handle:         "bob",
		Kind:           "human",
	}
	require.NoError(t, db.Create(editor).Error)

	settings := &store.UserSettings{
		UserID:          user.ID,
		MentionPolicy:   "subscribed",
		NotifyOnEndorse: true,
	}
	require.NoError(t, db.Create(settings).Error)
	var gotSettings store.UserSettings
	require.NoError(t, db.First(&gotSettings, "user_id = ?", user.ID).Error)
	require.Equal(t, "subscribed", gotSettings.MentionPolicy)

	grant := &store.MentionGrant{UserID: user.ID, GrantedUserID: editor.ID}
	require.NoError(t, db.Create(grant).Error)
	var gotGrant store.MentionGrant
	require.NoError(t, db.First(&gotGrant, "user_id = ? AND granted_user_id = ?", user.ID, editor.ID).Error)
	require.Equal(t, editor.ID, gotGrant.GrantedUserID)

	group := &store.FriendGroup{OwnerID: user.ID, Name: "close friends"}
	require.NoError(t, db.Create(group).Error)
	var gotGroup store.FriendGroup
	require.NoError(t, db.First(&gotGroup, "id = ?", group.ID).Error)
	require.Equal(t, "close friends", gotGroup.Name)

	groupMember := &store.FriendGroupMember{GroupID: group.ID, MemberUserID: editor.ID}
	require.NoError(t, db.Create(groupMember).Error)
	var gotGroupMember store.FriendGroupMember
	require.NoError(t, db.First(&gotGroupMember, "group_id = ? AND member_user_id = ?", group.ID, editor.ID).Error)
	require.Equal(t, editor.ID, gotGroupMember.MemberUserID)

	board := &store.Board{
		Slug:      "general",
		Title:     "General",
		Kind:      "discussion",
		CreatedBy: user.ID,
	}
	require.NoError(t, db.Create(board).Error)
	var gotBoard store.Board
	require.NoError(t, db.First(&gotBoard, "id = ?", board.ID).Error)
	require.Equal(t, "general", gotBoard.Slug)

	boardMember := &store.BoardMember{BoardID: board.ID, UserID: user.ID, Role: "maintainer"}
	require.NoError(t, db.Create(boardMember).Error)
	var gotBoardMember store.BoardMember
	require.NoError(t, db.First(&gotBoardMember, "board_id = ? AND user_id = ?", board.ID, user.ID).Error)
	require.Equal(t, "maintainer", gotBoardMember.Role)

	trustedDomain := &store.TrustedDomain{Domain: "example.com", AddedBy: user.ID}
	require.NoError(t, db.Create(trustedDomain).Error)
	var gotTrustedDomain store.TrustedDomain
	require.NoError(t, db.First(&gotTrustedDomain, "domain = ?", "example.com").Error)
	require.Equal(t, user.ID, gotTrustedDomain.AddedBy)

	post := &store.Post{
		BoardID:        board.ID,
		AuthorID:       user.ID,
		Title:          "Hello",
		BodyMD:         "hello world",
		Origin:         "user",
		OriginRef:      datatypes.JSON(`{}`),
		LastActivityAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(post).Error)
	var gotPost store.Post
	require.NoError(t, db.First(&gotPost, "id = ?", post.ID).Error)
	require.Equal(t, "Hello", gotPost.Title)

	comment := &store.Comment{
		PostID:   post.ID,
		AuthorID: editor.ID,
		Path:     store.Ltree(post.ID),
		BodyMD:   "a reply",
		Origin:   "user",
	}
	require.NoError(t, db.Create(comment).Error)
	var gotComment store.Comment
	require.NoError(t, db.First(&gotComment, "id = ?", comment.ID).Error)
	require.Equal(t, "a reply", gotComment.BodyMD)
	require.Equal(t, comment.Path, gotComment.Path)

	revision := &store.Revision{
		TargetType: "post",
		TargetID:   post.ID,
		EditorID:   user.ID,
		PrevBodyMD: "hello world (before edit)",
	}
	require.NoError(t, db.Create(revision).Error)
	var gotRevision store.Revision
	require.NoError(t, db.First(&gotRevision, "id = ?", revision.ID).Error)
	require.Equal(t, post.ID, gotRevision.TargetID)

	endorsement := &store.Endorsement{
		UserID:     editor.ID,
		TargetType: "post",
		TargetID:   post.ID,
	}
	require.NoError(t, db.Create(endorsement).Error)
	var gotEndorsement store.Endorsement
	require.NoError(t, db.First(&gotEndorsement, "id = ?", endorsement.ID).Error)
	require.Equal(t, editor.ID, gotEndorsement.UserID)

	subscription := &store.Subscription{
		UserID:     user.ID,
		TargetType: "board",
		TargetID:   board.ID,
	}
	require.NoError(t, db.Create(subscription).Error)
	var gotSubscription store.Subscription
	require.NoError(t, db.First(&gotSubscription, "id = ?", subscription.ID).Error)
	require.Equal(t, board.ID, gotSubscription.TargetID)

	readMark := &store.ReadMark{
		UserID:     user.ID,
		PostID:     post.ID,
		LastReadAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(readMark).Error)
	var gotReadMark store.ReadMark
	require.NoError(t, db.First(&gotReadMark, "user_id = ? AND post_id = ?", user.ID, post.ID).Error)
	require.Equal(t, post.ID, gotReadMark.PostID)

	unreadOverride := &store.UnreadOverride{
		UserID:     user.ID,
		TargetType: "post",
		TargetID:   post.ID,
	}
	require.NoError(t, db.Create(unreadOverride).Error)
	var gotUnreadOverride store.UnreadOverride
	require.NoError(t, db.First(&gotUnreadOverride,
		"user_id = ? AND target_type = ? AND target_id = ?", user.ID, "post", post.ID).Error)
	require.Equal(t, post.ID, gotUnreadOverride.TargetID)

	notification := &store.Notification{
		UserID:         user.ID,
		Event:          "new_comment",
		SubscriptionID: &subscription.ID,
		ActorID:        &editor.ID,
		TargetType:     "comment",
		TargetID:       comment.ID,
		PostID:         &post.ID,
	}
	require.NoError(t, db.Create(notification).Error)
	var gotNotification store.Notification
	require.NoError(t, db.First(&gotNotification, "id = ?", notification.ID).Error)
	require.Equal(t, "new_comment", gotNotification.Event)
	require.Equal(t, comment.ID, gotNotification.TargetID)

	mapping := &store.GithubMapping{
		BoardID:    board.ID,
		Repo:       "catalystcommunity/firepit",
		Events:     []string{"issues", "release"},
		SecretRef:  "vault://firepit/github-webhook",
		ThreadMode: "post_per_issue",
		CreatedBy:  user.ID,
	}
	require.NoError(t, db.Create(mapping).Error)
	var gotMapping store.GithubMapping
	require.NoError(t, db.First(&gotMapping, "id = ?", mapping.ID).Error)
	require.Equal(t, "catalystcommunity/firepit", gotMapping.Repo)
	require.Equal(t, []string(mapping.Events), []string(gotMapping.Events))

	session := &store.Session{
		UserID:    user.ID,
		TokenHash: "hash-of-token",
		ExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
	}
	require.NoError(t, db.Create(session).Error)
	var gotSession store.Session
	require.NoError(t, db.First(&gotSession, "id = ?", session.ID).Error)
	require.Equal(t, "hash-of-token", gotSession.TokenHash)
}
