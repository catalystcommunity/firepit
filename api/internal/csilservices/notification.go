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

	// One batched actor lookup for the whole page (never a per-row query) —
	// most rows carry an actor_id, some don't (see Notification.actor_id's
	// doc comment), GetUsersByIDs tolerates the blanks fine.
	actorIDs := make([]string, 0, len(result.Notifications))
	for _, n := range result.Notifications {
		if n.ActorID != nil {
			actorIDs = append(actorIDs, *n.ActorID)
		}
	}
	actors, err := s.store.GetUsersByIDs(ctx, actorIDs)
	if err != nil {
		return csil.NotificationPage{}, err
	}

	page := csil.NotificationPage{
		Notifications: make([]csil.Notification, len(result.Notifications)),
	}
	for i, n := range result.Notifications {
		var actor *store.User
		if n.ActorID != nil {
			if u, ok := actors[*n.ActorID]; ok {
				actor = &u
			}
		}
		page.Notifications[i] = toCSILNotification(n, actor)
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

// toCSILNotification converts a store row to its wire representation. actor
// is the resolved store.User for n.ActorID (nil if n.ActorID is nil, or the
// batched lookup found no matching row) — ListNotifications resolves this
// once per page (GetUsersByIDs), never per row.
func toCSILNotification(n store.Notification, actor *store.User) csil.Notification {
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
	if actor != nil {
		if actor.Handle != "" {
			handle := actor.Handle
			out.ActorHandle = &handle
		}
		if actor.DisplayName != "" {
			displayName := actor.DisplayName
			out.ActorDisplayName = &displayName
		}
	}
	if n.PostID != nil {
		out.PostId = csil.PostID(*n.PostID)
	}
	return out
}
