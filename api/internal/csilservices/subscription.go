package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// subscriptionService is the stub SubscriptionService implementation (task
// B1). B6 replaces every method body below; see the package doc comment
// (doc.go) for the error-handling contract every replacement must follow.
type subscriptionService struct {
	store *store.Store
}

// NewSubscriptionService constructs the SubscriptionService implementation.
func NewSubscriptionService(st *store.Store) csil.SubscriptionService {
	return &subscriptionService{store: st}
}

func (s *subscriptionService) Subscribe(ctx context.Context, req csil.TargetRef) (csil.Subscription, error) {
	return csil.Subscription{}, Unimplemented("SubscriptionService.subscribe")
}

func (s *subscriptionService) Unsubscribe(ctx context.Context, req csil.TargetRef) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("SubscriptionService.unsubscribe")
}

func (s *subscriptionService) SetMuted(ctx context.Context, req csil.SetMutedRequest) (csil.Subscription, error) {
	return csil.Subscription{}, Unimplemented("SubscriptionService.set-muted")
}

func (s *subscriptionService) ListSubscriptions(ctx context.Context, req csil.Empty) (csil.SubscriptionList, error) {
	return csil.SubscriptionList{}, Unimplemented("SubscriptionService.list-subscriptions")
}
