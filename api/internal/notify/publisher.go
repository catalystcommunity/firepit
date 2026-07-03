// DBPublisher is the real fan-out implementation of Publisher (task B7). See
// notify.go for the Event contract this file consumes and PLANDOC.md §4 for
// the design this implements: subscriber resolution (board + post + ancestor
// comments), mention resolution, and endorsement notification.
//
// Every query in this file runs against the tx passed into Publish — never
// against a separately held *gorm.DB — because notify.go's contract is that
// fan-out commits atomically with the content write that caused it.
// DBPublisher intentionally holds no state (no *store.Store, no DB handle)
// for exactly that reason: holding one would invite accidentally querying
// outside the caller's transaction.
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// Event.event values stored on the notifications row. new_post/new_comment/
// mention mirror the notification_event enum values already in the baseline
// migration (coredb/migrations/000001_baseline.sql). EventEndorsed does NOT
// — see the doc comment on fanOutEndorsement below for why, and
// coredb/migrations/000003_notification_event_endorsed.sql for the fix.
const (
	EventNewPost    = "new_post"
	EventNewComment = "new_comment"
	EventMention    = "mention"
	EventEndorsed   = "endorsed"
)

// DBPublisher is the production Publisher: it writes `notifications` rows
// computed from subscriptions/mention_grants/user_settings, inside the
// caller's transaction.
type DBPublisher struct{}

// NewDBPublisher constructs a DBPublisher. It's a zero-size type — every
// method takes the tx it needs as a parameter — so the zero value works
// fine too; the constructor exists for call-site symmetry with everything
// else in cmd/firepit-api/main.go being built via a New* function.
func NewDBPublisher() *DBPublisher { return &DBPublisher{} }

// Publish implements Publisher.
func (*DBPublisher) Publish(ctx context.Context, tx *gorm.DB, e Event) error {
	switch e.Kind {
	case KindContentCreated:
		if err := fanOutContent(ctx, tx, e); err != nil {
			return err
		}
		return fanOutMentions(ctx, tx, e)
	case KindContentEdited:
		// "Notification of subscribers does NOT re-fire" (notify.go) — only
		// mentions are re-scanned on an edit.
		return fanOutMentions(ctx, tx, e)
	case KindEndorsed:
		return fanOutEndorsement(ctx, tx, e)
	default:
		return fmt.Errorf("notify: unknown event kind %q", e.Kind)
	}
}

// contentTarget returns the (target_type, target_id, event) triple a
// KindContentCreated/KindContentEdited Event describes: a comment if
// CommentID is set, otherwise the post itself.
func contentTarget(e Event, commentEvent, postEvent string) (targetType, targetID, event string) {
	if e.CommentID != "" {
		return "comment", e.CommentID, commentEvent
	}
	return "post", e.PostID, postEvent
}

// subscriberCandidate is one row of the "who might care about this new
// content" query: a subscription that matches the board, the post, or (for
// a new comment) one of its ancestor comments.
type subscriberCandidate struct {
	SubscriptionID string `gorm:"column:subscription_id"`
	UserID         string `gorm:"column:user_id"`
	Muted          bool   `gorm:"column:muted"`
	// Priority ranks candidate sources so exactly one governs a user who
	// matches more than one (see the precedence doc comment on
	// resolveContentSubscribers).
	Priority int `gorm:"column:priority"`
}

// resolveContentSubscribers is the "one query" PLANDOC.md §4 asks for:
// every subscription matching the board, the post (only relevant once the
// post already exists, i.e. for a new comment), or an ancestor comment of a
// new comment (via the ltree `@>` containment operator against
// comments.path — see comments.path's doc comment in
// api/internal/store/comment.go). Ancestor lookup never needs to know how
// comments.path encodes a comment id as an ltree label: `@>` compares two
// ltree values directly, so whatever encoding populates the column (owned
// by ThreadService/B4) just works here unmodified.
//
// Precedence ("strongest source wins", PLANDOC.md §7's B7 acceptance
// criteria): a user can match more than one candidate (e.g. subscribed to
// the board AND to a specific ancestor comment, possibly with different
// `muted` flags — "muted carves holes out of a broader subscription",
// PLANDOC.md §4). Exactly one candidate governs both (a) whether the user
// is notified at all and (b) which subscription_id the notification row
// records, chosen as the MOST SPECIFIC match:
//
//	comment (nearer ancestor first) > post > board
//
// Priority is board=0, post=1, comment=2+nlevel(ancestor's own path) — a
// deeper ancestor (closer to the new comment) has a longer path and so a
// higher priority number, i.e. wins over a shallower one. If the winning
// candidate is muted, the user is suppressed for this event even if a
// weaker (broader) candidate was unmuted — that's the "carve a hole"
// behavior: a targeted mute overrides a broader subscribe.
func resolveContentSubscribers(ctx context.Context, tx *gorm.DB, e Event) ([]subscriberCandidate, error) {
	isComment := e.CommentID != ""

	var b strings.Builder
	args := []any{e.BoardID}
	b.WriteString(`WITH candidates AS (
    SELECT 'board'::text AS target_type, ?::uuid AS target_id, 0 AS priority`)

	if isComment {
		b.WriteString(`
    UNION ALL
    SELECT 'post'::text, ?::uuid, 1`)
		args = append(args, e.PostID)

		b.WriteString(`
    UNION ALL
    SELECT 'comment'::text, c.id, 2 + nlevel(c.path)
    FROM comments c, comments new_c
    WHERE new_c.id = ? AND c.path @> new_c.path AND c.id <> new_c.id`)
		args = append(args, e.CommentID)
	}

	b.WriteString(`
)
SELECT s.id AS subscription_id, s.user_id AS user_id, s.muted AS muted, c.priority AS priority
FROM subscriptions s
JOIN candidates c ON s.target_type = c.target_type AND s.target_id = c.target_id
WHERE s.user_id <> ?`)
	args = append(args, e.ActorID)

	var rows []subscriberCandidate
	if err := tx.WithContext(ctx).Raw(b.String(), args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("notify: resolving content subscribers: %w", err)
	}
	return rows, nil
}

// strongestPerUser reduces candidate rows to the single highest-priority
// row per user (see resolveContentSubscribers's precedence doc comment).
func strongestPerUser(rows []subscriberCandidate) map[string]subscriberCandidate {
	winners := make(map[string]subscriberCandidate, len(rows))
	for _, r := range rows {
		if cur, ok := winners[r.UserID]; !ok || r.Priority > cur.Priority {
			winners[r.UserID] = r
		}
	}
	return winners
}

// fanOutContent inserts new_post/new_comment notifications for every
// subscriber resolved by resolveContentSubscribers whose winning
// subscription isn't muted. Only called for KindContentCreated — edits
// never re-fire subscriber notifications (notify.go).
func fanOutContent(ctx context.Context, tx *gorm.DB, e Event) error {
	targetType, targetID, eventName := contentTarget(e, EventNewComment, EventNewPost)

	rows, err := resolveContentSubscribers(ctx, tx, e)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	var toInsert []store.Notification
	for userID, w := range strongestPerUser(rows) {
		if w.Muted {
			continue
		}
		subID := w.SubscriptionID
		actorID := e.ActorID
		postID := e.PostID
		toInsert = append(toInsert, store.Notification{
			UserID:         userID,
			Event:          eventName,
			SubscriptionID: &subID,
			ActorID:        &actorID,
			TargetType:     targetType,
			TargetID:       targetID,
			PostID:         &postID,
			CreatedAt:      now,
		})
	}
	if len(toInsert) == 0 {
		return nil
	}
	if err := tx.WithContext(ctx).Create(&toInsert).Error; err != nil {
		return fmt.Errorf("notify: inserting content notifications: %w", err)
	}
	return nil
}

// fanOutMentions parses @handles out of e.Body, resolves them to users, and
// notifies each one whose mention_policy allows it — skipping the actor and
// skipping any user already notified of a mention on this exact target
// (target_type/target_id), which is what makes this safe to call again on
// KindContentEdited: only newly-added handles produce a new row.
func fanOutMentions(ctx context.Context, tx *gorm.DB, e Event) error {
	handles := ParseMentions(e.Body)
	if len(handles) == 0 {
		return nil
	}

	targetType, targetID, _ := contentTarget(e, "", "")

	var users []store.User
	if err := tx.WithContext(ctx).
		Where("lower(handle) IN ?", handles).
		Find(&users).Error; err != nil {
		return fmt.Errorf("notify: resolving mentioned users: %w", err)
	}
	if len(users) == 0 {
		return nil
	}

	var alreadyNotified []string
	if err := tx.WithContext(ctx).
		Model(&store.Notification{}).
		Where("event = ? AND target_type = ? AND target_id = ?", EventMention, targetType, targetID).
		Pluck("user_id", &alreadyNotified).Error; err != nil {
		return fmt.Errorf("notify: checking existing mention notifications: %w", err)
	}
	already := make(map[string]bool, len(alreadyNotified))
	for _, id := range alreadyNotified {
		already[id] = true
	}

	now := time.Now().UTC()
	var toInsert []store.Notification
	for _, u := range users {
		if u.ID == e.ActorID || already[u.ID] {
			continue
		}
		allowed, err := mentionAllowed(ctx, tx, e, u.ID)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		actorID := e.ActorID
		postID := e.PostID
		toInsert = append(toInsert, store.Notification{
			UserID:     u.ID,
			Event:      EventMention,
			ActorID:    &actorID,
			TargetType: targetType,
			TargetID:   targetID,
			PostID:     &postID,
			CreatedAt:  now,
		})
	}
	if len(toInsert) == 0 {
		return nil
	}
	if err := tx.WithContext(ctx).Create(&toInsert).Error; err != nil {
		return fmt.Errorf("notify: inserting mention notifications: %w", err)
	}
	return nil
}

// mentionAllowed applies mentionedUserID's mention_policy (PLANDOC.md §4,
// §9 decision 4). A missing user_settings row means the column defaults
// apply (mention_policy 'subscribed') — user_settings rows aren't
// guaranteed to exist for every user (SettingsService/B9 creates them
// lazily), so "no row" is a normal case, not a fault.
func mentionAllowed(ctx context.Context, tx *gorm.DB, e Event, mentionedUserID string) (bool, error) {
	policy := "subscribed"
	var settings store.UserSettings
	err := tx.WithContext(ctx).First(&settings, "user_id = ?", mentionedUserID).Error
	switch {
	case err == nil:
		policy = settings.MentionPolicy
	case store.IsNotFound(err):
		// default applies
	default:
		return false, fmt.Errorf("notify: loading mention policy: %w", err)
	}

	granted, err := hasMentionGrant(ctx, tx, mentionedUserID, e.ActorID)
	if err != nil {
		return false, err
	}

	if policy == "subscribed" && !granted {
		granted, err = subscribedToContentScope(ctx, tx, e, mentionedUserID)
		if err != nil {
			return false, err
		}
	}

	return mentionGateAllows(policy, granted), nil
}

// mentionGateAllows is the pure mention_policy decision (PLANDOC.md §4, §9
// decision 4), separated from mentionAllowed's DB fetches so it's directly
// unit-testable:
//
//   - "everyone": always notify.
//   - "nobody": never notify.
//   - "authorized": notify only if the mentioned user granted the actor
//     (mention_grants row) — inScopeOrGranted must already reflect ONLY the
//     grant for this policy to behave as documented; see mentionAllowed,
//     which only widens inScopeOrGranted to include subscribed-context for
//     policy=="subscribed".
//   - "subscribed" (default): notify if the mentioned user granted the
//     actor OR subscribes to the board/post/ancestor-comment scope the
//     mention appears in (mentionAllowed folds both into
//     inScopeOrGranted before calling this).
//   - anything else (defensive default, e.g. a future policy value this
//     code doesn't know about yet): never notify — denied mentions still
//     render as plain text (PLANDOC.md §4), so failing closed is safe.
func mentionGateAllows(policy string, inScopeOrGranted bool) bool {
	switch policy {
	case "everyone":
		return true
	case "nobody":
		return false
	case "authorized", "subscribed":
		return inScopeOrGranted
	default:
		return false
	}
}

// hasMentionGrant reports whether grantedUserID may always mention-notify
// userID (mention_grants: "who may ALWAYS mention-notify me").
func hasMentionGrant(ctx context.Context, tx *gorm.DB, userID, grantedUserID string) (bool, error) {
	var count int64
	err := tx.WithContext(ctx).Model(&store.MentionGrant{}).
		Where("user_id = ? AND granted_user_id = ?", userID, grantedUserID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("notify: checking mention grant: %w", err)
	}
	return count > 0, nil
}

// subscribedToContentScope reports whether userID holds ANY subscription
// (board, post, or an ancestor comment of a new comment) matching e's
// content — the "subscribed" mention policy's context test. Deliberately
// ignores the `muted` flag: fan-out mutes are about suppressing passive
// notification noise, but a mention is a deliberate, direct address by
// another person, and PLANDOC.md §4 only asks whether the mentioned user
// "subscribes to the ... scope", not whether they'd currently be notified
// of ordinary activity there. This is a judgment call, documented here
// since PLANDOC doesn't spell it out.
func subscribedToContentScope(ctx context.Context, tx *gorm.DB, e Event, userID string) (bool, error) {
	isComment := e.CommentID != ""

	var b strings.Builder
	args := []any{userID, e.BoardID, e.PostID}
	b.WriteString(`
SELECT count(*) FROM subscriptions s
WHERE s.user_id = ? AND (
  (s.target_type = 'board' AND s.target_id = ?)
  OR (s.target_type = 'post' AND s.target_id = ?)`)

	if isComment {
		b.WriteString(`
  OR (s.target_type = 'comment' AND s.target_id IN (
        SELECT c.id FROM comments c, comments new_c
        WHERE new_c.id = ? AND c.path @> new_c.path
      ))`)
		args = append(args, e.CommentID)
	}
	b.WriteString(`
)`)

	var count int64
	if err := tx.WithContext(ctx).Raw(b.String(), args...).Scan(&count).Error; err != nil {
		return false, fmt.Errorf("notify: checking mention subscribed-scope: %w", err)
	}
	return count > 0, nil
}

// fanOutEndorsement notifies the endorsed content's author, gated by
// notify_on_endorse (PLANDOC.md §4/§9 decision 3). Blocked on a schema gap:
// notification_event (coredb/migrations/000001_baseline.sql) only defines
// new_post/new_comment/mention/github_event — no value for an endorsement.
// Fixed additively in
// coredb/migrations/000003_notification_event_endorsed.sql (adds
// 'endorsed'); see that file's doc comment. EventEndorsed is only a valid
// value to insert once that migration has run.
func fanOutEndorsement(ctx context.Context, tx *gorm.DB, e Event) error {
	var authorID string
	switch e.TargetType {
	case "post":
		var post store.Post
		if err := tx.WithContext(ctx).Select("author_id").First(&post, "id = ?", e.TargetID).Error; err != nil {
			return fmt.Errorf("notify: loading endorsed post author: %w", err)
		}
		authorID = post.AuthorID
	case "comment":
		var comment store.Comment
		if err := tx.WithContext(ctx).Select("author_id").First(&comment, "id = ?", e.TargetID).Error; err != nil {
			return fmt.Errorf("notify: loading endorsed comment author: %w", err)
		}
		authorID = comment.AuthorID
	default:
		return fmt.Errorf("notify: KindEndorsed event with unknown target_type %q", e.TargetType)
	}

	// Belt-and-suspenders: EndorsementService (B5) is expected to already
	// block self-endorsement outright, so this should be unreachable, but
	// fan-out never notifies the actor regardless of what called it.
	if authorID == e.ActorID {
		return nil
	}

	notifyOnEndorse := true
	var settings store.UserSettings
	err := tx.WithContext(ctx).First(&settings, "user_id = ?", authorID).Error
	switch {
	case err == nil:
		notifyOnEndorse = settings.NotifyOnEndorse
	case store.IsNotFound(err):
		// default applies
	default:
		return fmt.Errorf("notify: loading notify_on_endorse setting: %w", err)
	}
	if !notifyOnEndorse {
		return nil
	}

	actorID := e.ActorID
	postID := e.PostID
	notif := store.Notification{
		UserID:     authorID,
		Event:      EventEndorsed,
		ActorID:    &actorID,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		PostID:     &postID,
		CreatedAt:  time.Now().UTC(),
	}
	if err := tx.WithContext(ctx).Create(&notif).Error; err != nil {
		return fmt.Errorf("notify: inserting endorsement notification: %w", err)
	}
	return nil
}
