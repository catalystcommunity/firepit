package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// notificationService is the stub NotificationService implementation (task
// B1). B7 replaces every method body below; see the package doc comment
// (doc.go) for the error-handling contract every replacement must follow.
type notificationService struct {
	store *store.Store
}

// NewNotificationService constructs the NotificationService implementation.
func NewNotificationService(st *store.Store) csil.NotificationService {
	return &notificationService{store: st}
}

func (s *notificationService) ListNotifications(ctx context.Context, req csil.ListNotificationsRequest) (csil.NotificationPage, error) {
	return csil.NotificationPage{}, Unimplemented("NotificationService.list-notifications")
}

func (s *notificationService) MarkNotificationRead(ctx context.Context, req csil.NotificationIDs) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("NotificationService.mark-notification-read")
}

func (s *notificationService) MarkAllRead(ctx context.Context, req csil.Empty) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("NotificationService.mark-all-read")
}
