package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Notification mirrors the `notifications` table — a fan-out target.
// SubscriptionID/PostID are convenience denormalizations for the inbox
// query; TargetType/TargetID is the thing that actually triggered it.
type Notification struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	UserID         string     `gorm:"column:user_id;type:uuid;not null"`
	Event          string     `gorm:"column:event;type:notification_event;not null"`
	SubscriptionID *string    `gorm:"column:subscription_id;type:uuid"`
	ActorID        *string    `gorm:"column:actor_id;type:uuid"`
	TargetType     string     `gorm:"column:target_type;not null"`
	TargetID       string     `gorm:"column:target_id;type:uuid;not null"`
	PostID         *string    `gorm:"column:post_id;type:uuid"`
	ReadAt         *time.Time `gorm:"column:read_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
}

func (Notification) TableName() string { return "notifications" }

// notificationCursor is the decoded form of a NotificationService
// list-notifications PageCursor: the (created_at, id) of the last row of
// the previous page. Notifications are listed newest-first
// (created_at DESC, id DESC as a tiebreaker for same-timestamp rows), so
// the next page is every row strictly less than this pair in that order.
type notificationCursor struct {
	CreatedAt time.Time
	ID        string
}

// EncodeNotificationCursor renders a cursor for the row (createdAt, id) —
// the last row on the current page — as an opaque token for
// NotificationPage.next_cursor.
func EncodeNotificationCursor(createdAt time.Time, id string) string {
	raw := fmt.Sprintf("%s|%s", createdAt.UTC().Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeNotificationCursor reverses EncodeNotificationCursor. A malformed
// cursor (anything not produced by EncodeNotificationCursor, e.g. a client
// bug or a stale/tampered token) returns ok=false rather than an error:
// list-notifications has no declared ServiceError arm (csil/firepit.csil),
// so callers treat an invalid cursor as "no cursor" (start from the newest
// page) rather than failing the whole request.
func DecodeNotificationCursor(cursor string) (notificationCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return notificationCursor{}, false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return notificationCursor{}, false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || parts[1] == "" {
		return notificationCursor{}, false
	}
	return notificationCursor{CreatedAt: createdAt, ID: parts[1]}, true
}

// ListNotificationsResult is one page of a user's notifications, newest
// first, plus the cursor for the next page (empty if this was the last
// page).
type ListNotificationsResult struct {
	Notifications []Notification
	NextCursor    string
}

// ListNotifications returns userID's notifications newest-first, optionally
// filtered to unread only, cursor-paginated. limit is the caller's already
// clamped page size (NotificationService applies the CSIL schema's
// `.default 50 .le 200`); ListNotifications itself doesn't second-guess it.
func (s *Store) ListNotifications(ctx context.Context, userID string, cursor string, limit int, unreadOnly bool) (ListNotificationsResult, error) {
	q := s.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		Limit(limit + 1) // fetch one extra row to know whether there's a next page

	if unreadOnly {
		q = q.Where("read_at IS NULL")
	}

	if cursor != "" {
		if c, ok := DecodeNotificationCursor(cursor); ok {
			q = q.Where("(created_at, id) < (?, ?)", c.CreatedAt, c.ID)
		}
		// An undecodable cursor is silently ignored (see
		// DecodeNotificationCursor's doc comment) rather than treated as an
		// error.
	}

	var rows []Notification
	if err := q.Find(&rows).Error; err != nil {
		return ListNotificationsResult{}, err
	}

	result := ListNotificationsResult{Notifications: rows}
	if len(rows) > limit {
		last := rows[limit-1]
		result.Notifications = rows[:limit]
		result.NextCursor = EncodeNotificationCursor(last.CreatedAt, last.ID)
	}
	return result, nil
}

// MarkNotificationsRead marks the given notification ids read for userID,
// updating only rows userID actually owns — ids belonging to someone else
// (or that don't exist) are silently skipped rather than erroring, matching
// mark-notification-read's no-declared-error-arm shape (csil/firepit.csil)
// and firepit's "don't leak existence" convention (see
// csilservices/errors.go's NotFound doc comment: same idea applied to a
// bulk update instead of a single lookup).
func (s *Store) MarkNotificationsRead(ctx context.Context, userID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.DB.WithContext(ctx).
		Model(&Notification{}).
		Where("user_id = ? AND id IN ? AND read_at IS NULL", userID, ids).
		Update("read_at", time.Now().UTC()).Error
}

// MarkAllNotificationsRead marks every currently-unread notification for
// userID read.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID string) error {
	return s.DB.WithContext(ctx).
		Model(&Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", time.Now().UTC()).Error
}
