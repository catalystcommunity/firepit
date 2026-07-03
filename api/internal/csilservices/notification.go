package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// defaultNotificationPageLimit and maxNotificationPageLimit mirror
// ListNotificationsRequest's CSIL declaration (csil/types/notifications.csil:
// `? limit: uint .default 50 .le 200`). The generated decoder does NOT
// apply that default itself (see api/internal/csil/codec.gen.go's
// csilDecListNotificationsRequest: an absent field just leaves the pointer
// nil) — every caller of a request with an optional-with-default field is
// responsible for applying it, and this is where list-notifications does.
const (
	defaultNotificationPageLimit = 50
	maxNotificationPageLimit     = 200
)

// notificationService is the NotificationService implementation (task B7).
// See the package doc comment (doc.go) for the general
// stub-to-real-implementation and error-handling contract.
type notificationService struct {
	store *store.Store
}

// NewNotificationService constructs the NotificationService implementation.
func NewNotificationService(st *store.Store) csil.NotificationService {
	return &notificationService{store: st}
}

// ListNotifications returns the caller's own notifications, newest first,
// cursor-paginated. list-notifications has no declared ServiceError arm
// (csil/firepit.csil), so an anonymous caller (no session) isn't an error —
// it just can't own any notifications, so it gets an empty page.
func (s *notificationService) ListNotifications(ctx context.Context, req csil.ListNotificationsRequest) (csil.NotificationPage, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.NotificationPage{}, nil
	}

	limit := defaultNotificationPageLimit
	if req.Limit != nil {
		limit = int(*req.Limit)
	}
	if limit <= 0 {
		limit = defaultNotificationPageLimit
	}
	if limit > maxNotificationPageLimit {
		limit = maxNotificationPageLimit
	}

	unreadOnly := false
	if req.UnreadOnly != nil {
		unreadOnly = *req.UnreadOnly
	}

	cursor := ""
	if req.Cursor != nil {
		cursor = string(*req.Cursor)
	}

	result, err := s.store.ListNotifications(ctx, user.ID, cursor, limit, unreadOnly)
	if err != nil {
		// No declared error arm: per doc.go, a genuinely unexpected DB
		// failure is exactly the case reserved for returning an error here
		// (the dispatcher maps it to a transport-level failure and logs the
		// real cause server-side).
		return csil.NotificationPage{}, err
	}

	page := csil.NotificationPage{
		Notifications: make([]csil.Notification, len(result.Notifications)),
	}
	for i, n := range result.Notifications {
		page.Notifications[i] = toCSILNotification(n)
	}
	if result.NextCursor != "" {
		cursor := csil.PageCursor(result.NextCursor)
		page.NextCursor = &cursor
	}
	return page, nil
}

// MarkNotificationRead marks the given notification ids read, scoped to the
// caller's own rows (store.MarkNotificationsRead's WHERE clause bakes the
// ownership check in — an id the caller doesn't own is silently skipped
// rather than erroring, consistent with firepit's "don't leak existence"
// convention). An anonymous caller owns nothing, so this is a no-op.
func (s *notificationService) MarkNotificationRead(ctx context.Context, req csil.NotificationIDs) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok || len(req) == 0 {
		return csil.Empty{}, nil
	}

	ids := make([]string, len(req))
	for i, id := range req {
		ids[i] = string(id)
	}

	if err := s.store.MarkNotificationsRead(ctx, user.ID, ids); err != nil {
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

// MarkAllRead marks every one of the caller's currently-unread notifications
// read. An anonymous caller has nothing to mark, so this is a no-op.
func (s *notificationService) MarkAllRead(ctx context.Context, req csil.Empty) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, nil
	}

	if err := s.store.MarkAllNotificationsRead(ctx, user.ID); err != nil {
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

// toCSILNotification converts a store row to its wire representation.
func toCSILNotification(n store.Notification) csil.Notification {
	out := csil.Notification{
		Id:         csil.NotificationID(n.ID),
		Event:      csil.NotificationEvent(n.Event),
		TargetType: csil.TargetType(n.TargetType),
		TargetId:   n.TargetID,
		CreatedAt:  n.CreatedAt,
		ReadAt:     n.ReadAt,
	}
	if n.ActorID != nil {
		actorID := csil.UserID(*n.ActorID)
		out.ActorId = &actorID
	}
	if n.PostID != nil {
		out.PostId = csil.PostID(*n.PostID)
	}
	return out
}
