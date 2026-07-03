//go:build integration

package store_test

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// TestNotificationFanOut drives notify.DBPublisher (task B7) against a real
// Postgres, seeding subscriptions/settings/grants directly via the store
// models and calling Publish inside an explicit transaction — exactly the
// shape ThreadService/EndorsementService are expected to use (notify.go's
// contract: "Writers call Publisher.Publish inside the same database
// transaction as the content write").
//
// Every subtest below builds its own board/post/users so scenarios can't
// interfere with each other despite sharing one container and connection
// (matches api/internal/store's other integration test's per-run
// isolation-by-fresh-ULID approach).
func TestNotificationFanOut(t *testing.T) {
	ctx := context.Background()
	db := newFanOutTestDB(t, ctx)

	t.Run("board subscriber is notified of a new post", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")
		f.subscribe(alice.ID, "board", f.board.ID, false)

		post := f.createPost(f.actor.ID, "hello")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		notifs := f.notificationsFor(alice.ID)
		require.Len(t, notifs, 1)
		require.Equal(t, notify.EventNewPost, notifs[0].Event)
		require.Equal(t, "post", notifs[0].TargetType)
		require.Equal(t, post.ID, notifs[0].TargetID)
		require.Equal(t, f.actor.ID, *notifs[0].ActorID)
	})

	t.Run("comment subscriber deep in the tree is notified of a descendant reply", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")

		post := f.createPost(f.actor.ID, "root")
		c1 := f.createComment(post.ID, nil, f.actor.ID, "level 1")
		c2 := f.createComment(post.ID, &c1, f.actor.ID, "level 2")
		c3 := f.createComment(post.ID, &c2, f.actor.ID, "level 3")

		// alice subscribes to the top comment's subtree, several levels
		// above the eventual reply.
		f.subscribe(alice.ID, "comment", c1.ID, false)

		reply := f.createComment(post.ID, &c3, f.actor.ID, "deep reply")
		f.publishContentCreated(notify.Event{
			Kind:      notify.KindContentCreated,
			ActorID:   f.actor.ID,
			BoardID:   f.board.ID,
			PostID:    post.ID,
			CommentID: reply.ID,
			Body:      reply.BodyMD,
		})

		notifs := f.notificationsFor(alice.ID)
		require.Len(t, notifs, 1)
		require.Equal(t, notify.EventNewComment, notifs[0].Event)
		require.Equal(t, "comment", notifs[0].TargetType)
		require.Equal(t, reply.ID, notifs[0].TargetID)
	})

	t.Run("a muted post subscription silences an otherwise-matching board subscription", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")
		f.subscribe(alice.ID, "board", f.board.ID, false)

		post := f.createPost(f.actor.ID, "root")
		// alice doesn't want this specific post's noise even though she's
		// subscribed to the whole board.
		f.subscribe(alice.ID, "post", post.ID, true)

		reply := f.createComment(post.ID, nil, f.actor.ID, "a reply")
		f.publishContentCreated(notify.Event{
			Kind:      notify.KindContentCreated,
			ActorID:   f.actor.ID,
			BoardID:   f.board.ID,
			PostID:    post.ID,
			CommentID: reply.ID,
			Body:      reply.BodyMD,
		})

		require.Empty(t, f.notificationsFor(alice.ID))
	})

	t.Run("the actor is never notified of their own content", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		f.subscribe(f.actor.ID, "board", f.board.ID, false)

		post := f.createPost(f.actor.ID, "hello")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		require.Empty(t, f.notificationsFor(f.actor.ID))
	})

	t.Run("board and post subscriptions for the same user dedupe to one notification, strongest wins", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")

		post := f.createPost(f.actor.ID, "root")
		boardSub := f.subscribe(alice.ID, "board", f.board.ID, false)
		postSub := f.subscribe(alice.ID, "post", post.ID, false)

		reply := f.createComment(post.ID, nil, f.actor.ID, "a reply")
		f.publishContentCreated(notify.Event{
			Kind:      notify.KindContentCreated,
			ActorID:   f.actor.ID,
			BoardID:   f.board.ID,
			PostID:    post.ID,
			CommentID: reply.ID,
			Body:      reply.BodyMD,
		})

		notifs := f.notificationsFor(alice.ID)
		require.Len(t, notifs, 1, "board+post subscription must dedupe to a single row")
		// post is more specific than board, so it must be the recorded
		// source, not board.
		require.NotNil(t, notifs[0].SubscriptionID)
		require.Equal(t, postSub.ID, *notifs[0].SubscriptionID)
		require.NotEqual(t, boardSub.ID, *notifs[0].SubscriptionID)
	})

	t.Run("mention: subscribed policy notifies when the mentioned user is in scope", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice") // default mention_policy: subscribed
		f.subscribe(alice.ID, "board", f.board.ID, false)

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle+" welcome")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		notifs := f.mentionNotificationsFor(alice.ID)
		require.Len(t, notifs, 1)
		require.Equal(t, "post", notifs[0].TargetType)
		require.Equal(t, post.ID, notifs[0].TargetID)
	})

	t.Run("mention: subscribed policy denies out-of-scope without a grant", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice") // not subscribed to anything, no grant

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle+" welcome")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		require.Empty(t, f.mentionNotificationsFor(alice.ID))
	})

	t.Run("mention: subscribed policy notifies out-of-scope when granted", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice") // not subscribed to anything
		f.grantMention(alice.ID, f.actor.ID)

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle+" welcome")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		require.Len(t, f.mentionNotificationsFor(alice.ID), 1)
	})

	t.Run("mention: authorized policy requires a grant even if subscribed", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")
		f.setMentionPolicy(alice.ID, "authorized")
		f.subscribe(alice.ID, "board", f.board.ID, false) // in scope, but no grant

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle+" welcome")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})
		require.Empty(t, f.mentionNotificationsFor(alice.ID), "authorized policy must ignore scope without a grant")

		f.grantMention(alice.ID, f.actor.ID)
		post2 := f.createPost(f.actor.ID, "hi again @"+alice.Handle)
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post2.ID,
			Body:    post2.BodyMD,
		})
		require.Len(t, f.mentionNotificationsFor(alice.ID), 1, "authorized policy must notify once granted")
	})

	t.Run("mention: nobody policy never notifies", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")
		f.setMentionPolicy(alice.ID, "nobody")
		f.subscribe(alice.ID, "board", f.board.ID, false)
		f.grantMention(alice.ID, f.actor.ID)

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle+" welcome")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		require.Empty(t, f.mentionNotificationsFor(alice.ID))
	})

	t.Run("mention: everyone policy always notifies", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")
		f.setMentionPolicy(alice.ID, "everyone")
		// no subscription, no grant

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle+" welcome")
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})

		require.Len(t, f.mentionNotificationsFor(alice.ID), 1)
	})

	t.Run("mention: an edit only notifies newly added handles, not ones already notified", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		alice := f.user("alice")
		bob := f.user("bob")
		f.setMentionPolicy(alice.ID, "everyone")
		f.setMentionPolicy(bob.ID, "everyone")

		post := f.createPost(f.actor.ID, "hi @"+alice.Handle)
		f.publishContentCreated(notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})
		require.Len(t, f.mentionNotificationsFor(alice.ID), 1)
		require.Empty(t, f.mentionNotificationsFor(bob.ID))

		editedBody := "hi @" + alice.Handle + " and @" + bob.Handle
		require.NoError(t, db.Model(&store.Post{}).Where("id = ?", post.ID).Update("body_md", editedBody).Error)
		f.publish(notify.Event{
			Kind:    notify.KindContentEdited,
			ActorID: f.actor.ID,
			BoardID: f.board.ID,
			PostID:  post.ID,
			Body:    editedBody,
		})

		require.Len(t, f.mentionNotificationsFor(alice.ID), 1, "alice was already notified for this target; edit must not duplicate")
		require.Len(t, f.mentionNotificationsFor(bob.ID), 1, "bob is newly mentioned by the edit and must be notified")
	})

	t.Run("endorsement notifies the content author", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		author := f.user("author")
		endorser := f.user("endorser")
		post := f.createPost(author.ID, "root")

		f.publish(notify.Event{
			Kind:       notify.KindEndorsed,
			ActorID:    endorser.ID,
			BoardID:    f.board.ID,
			PostID:     post.ID,
			TargetType: "post",
			TargetID:   post.ID,
		})

		notifs := f.notificationsFor(author.ID)
		require.Len(t, notifs, 1)
		require.Equal(t, notify.EventEndorsed, notifs[0].Event)
		require.Equal(t, endorser.ID, *notifs[0].ActorID)
	})

	t.Run("endorsement respects notify_on_endorse opt-out", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		author := f.user("author")
		endorser := f.user("endorser")
		f.setNotifyOnEndorse(author.ID, false)
		post := f.createPost(author.ID, "root")

		f.publish(notify.Event{
			Kind:       notify.KindEndorsed,
			ActorID:    endorser.ID,
			BoardID:    f.board.ID,
			PostID:     post.ID,
			TargetType: "post",
			TargetID:   post.ID,
		})

		require.Empty(t, f.notificationsFor(author.ID))
	})

	t.Run("endorsement never notifies self-endorsement", func(t *testing.T) {
		f := newFanOutFixture(t, db)
		author := f.user("author")
		post := f.createPost(author.ID, "root")

		f.publish(notify.Event{
			Kind:       notify.KindEndorsed,
			ActorID:    author.ID,
			BoardID:    f.board.ID,
			PostID:     post.ID,
			TargetType: "post",
			TargetID:   post.ID,
		})

		require.Empty(t, f.notificationsFor(author.ID))
	})
}

// --- fixture plumbing -------------------------------------------------

// newFanOutTestDB boots a fresh testcontainers Postgres with coredb's
// migrations applied and returns a connected *gorm.DB. Mirrors
// store_integration_test.go's container setup.
func newFanOutTestDB(t *testing.T, ctx context.Context) *gorm.DB {
	t.Helper()

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
	return gdb
}

// fanOutFixture bundles one board + one "actor" (post/comment author and
// content creator for a subtest) plus helpers for seeding subscriptions,
// settings, and grants, and for driving/asserting a Publish call.
type fanOutFixture struct {
	t     *testing.T
	db    *gorm.DB
	ctx   context.Context
	board store.Board
	actor store.User
}

// testIDCounter backs unique() with a process-wide monotonic counter rather
// than a truncated ULID: a naive "take the first few hex characters of
// generate_ulid()'s output" scheme collapses onto its slow-changing
// millisecond timestamp prefix under the burst of calls a single test
// function makes, causing exactly the kind of spurious
// users_linkkeys_domain_linkkeys_user_id_key collisions this replaces.
var testIDCounter atomic.Int64

func newFanOutFixture(t *testing.T, db *gorm.DB) *fanOutFixture {
	t.Helper()
	f := &fanOutFixture{t: t, db: db, ctx: context.Background()}
	actor := f.user("actor")
	f.actor = actor

	board := store.Board{Slug: f.unique("board"), Title: "Board", Kind: "discussion", CreatedBy: actor.ID}
	require.NoError(t, db.Create(&board).Error)
	f.board = board
	return f
}

func (f *fanOutFixture) unique(prefix string) string {
	return prefix + "-" + strconv.FormatInt(testIDCounter.Add(1), 10)
}

func (f *fanOutFixture) newULID() string {
	f.t.Helper()
	var id string
	require.NoError(f.t, f.db.Raw("SELECT generate_ulid()").Scan(&id).Error)
	return id
}

func (f *fanOutFixture) user(handlePrefix string) store.User {
	f.t.Helper()
	u := store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: f.unique(handlePrefix),
		Handle:         f.unique(handlePrefix),
		Kind:           "human",
		Roles:          []string{},
	}
	require.NoError(f.t, f.db.Create(&u).Error)
	return u
}

func (f *fanOutFixture) createPost(authorID, body string) store.Post {
	f.t.Helper()
	p := store.Post{
		BoardID:        f.board.ID,
		AuthorID:       authorID,
		Title:          "post",
		BodyMD:         body,
		Origin:         "user",
		LastActivityAt: time.Now().UTC(),
	}
	require.NoError(f.t, f.db.Create(&p).Error)
	return p
}

// createComment inserts a comment row with an explicitly computed ltree
// path (parent's path + this comment's own id as a label, or just this
// comment's id at the root) so ancestor queries have real data to match
// against. ltree labels can't contain hyphens, so each ULID's hyphens are
// replaced with underscores to form a valid label — this test's own
// encoding choice (ThreadService/B4 owns the real one), but
// notify.DBPublisher's ancestor query only ever compares path VALUES via
// `@>`, never decodes a label back into an id, so any well-formed
// materialized-path encoding exercises the same query logic.
func (f *fanOutFixture) createComment(postID string, parent *store.Comment, authorID, body string) store.Comment {
	f.t.Helper()
	id := f.newULID()
	label := strings.ReplaceAll(id, "-", "_")
	path := label
	var parentID *string
	if parent != nil {
		path = string(parent.Path) + "." + label
		pid := parent.ID
		parentID = &pid
	}
	c := store.Comment{
		ID:              id,
		PostID:          postID,
		ParentCommentID: parentID,
		AuthorID:        authorID,
		Path:            store.Ltree(path),
		BodyMD:          body,
		Origin:          "user",
	}
	require.NoError(f.t, f.db.Create(&c).Error)
	return c
}

func (f *fanOutFixture) subscribe(userID, targetType, targetID string, muted bool) store.Subscription {
	f.t.Helper()
	s := store.Subscription{UserID: userID, TargetType: targetType, TargetID: targetID, Muted: muted}
	require.NoError(f.t, f.db.Create(&s).Error)
	return s
}

func (f *fanOutFixture) grantMention(userID, grantedUserID string) {
	f.t.Helper()
	g := store.MentionGrant{UserID: userID, GrantedUserID: grantedUserID}
	require.NoError(f.t, f.db.Create(&g).Error)
}

func (f *fanOutFixture) setMentionPolicy(userID, policy string) {
	f.t.Helper()
	f.upsertSettings(userID, func(s *store.UserSettings) { s.MentionPolicy = policy })
}

func (f *fanOutFixture) setNotifyOnEndorse(userID string, on bool) {
	f.t.Helper()
	f.upsertSettings(userID, func(s *store.UserSettings) { s.NotifyOnEndorse = on })
}

func (f *fanOutFixture) upsertSettings(userID string, mutate func(*store.UserSettings)) {
	f.t.Helper()
	settings := store.UserSettings{UserID: userID, MentionPolicy: "subscribed", NotifyOnEndorse: true}
	mutate(&settings)
	// Deliberately raw SQL, not gorm's struct-based Create/Save, after two
	// gorm gotchas bit this in turn:
	//   - Save() no-ops (0 rows, no error) rather than inserting, because a
	//     non-zero primary key makes it emit an UPDATE, and there's no
	//     existing row the first time a subtest calls this for a given
	//     user.
	//   - Create(), even with Clauses(clause.OnConflict{...}).Select("*"),
	//     still silently dropped an explicit NotifyOnEndorse=false back to
	//     the column's `default:true` — gorm's zero-value-omission handling
	//     for defaulted columns won out over Select("*") in practice here.
	// Raw SQL sidesteps both: this is test-seeding code, not the
	// production write path (which never upserts user_settings — B9 owns
	// that), so there's no contract to keep in sync with.
	require.NoError(f.t, f.db.Exec(`
		INSERT INTO user_settings (user_id, mention_policy, notify_on_endorse, updated_at)
		VALUES (?, ?, ?, timezone('utc', now()))
		ON CONFLICT (user_id) DO UPDATE SET
			mention_policy = EXCLUDED.mention_policy,
			notify_on_endorse = EXCLUDED.notify_on_endorse,
			updated_at = EXCLUDED.updated_at
	`, settings.UserID, settings.MentionPolicy, settings.NotifyOnEndorse).Error)
}

// publish runs e through notify.DBPublisher inside its own transaction,
// exactly like a real writer would.
func (f *fanOutFixture) publish(e notify.Event) {
	f.t.Helper()
	tx := f.db.Begin()
	require.NoError(f.t, tx.Error)
	if err := notify.NewDBPublisher().Publish(f.ctx, tx, e); err != nil {
		require.NoError(f.t, tx.Rollback().Error)
		f.t.Fatalf("Publish failed: %v", err)
	}
	require.NoError(f.t, tx.Commit().Error)
}

// publishContentCreated is publish, named for readability at content-fan-out
// call sites (it's the same call as any other event kind).
func (f *fanOutFixture) publishContentCreated(e notify.Event) { f.publish(e) }

func (f *fanOutFixture) notificationsFor(userID string) []store.Notification {
	f.t.Helper()
	var rows []store.Notification
	require.NoError(f.t, f.db.Where("user_id = ?", userID).Order("created_at, id").Find(&rows).Error)
	return rows
}

func (f *fanOutFixture) mentionNotificationsFor(userID string) []store.Notification {
	f.t.Helper()
	var rows []store.Notification
	require.NoError(f.t, f.db.Where("user_id = ? AND event = ?", userID, notify.EventMention).
		Order("created_at, id").Find(&rows).Error)
	return rows
}
