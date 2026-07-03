package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// subscriptionService implements SubscriptionService (task B6): subscribe,
// unsubscribe, and mute are all target-validated and idempotent (see the
// doc comments on the store methods this delegates to — Store.Subscribe,
// Store.SetSubscriptionMuted, Store.Unsubscribe).
type subscriptionService struct {
	store *store.Store
}

// NewSubscriptionService constructs the SubscriptionService implementation.
func NewSubscriptionService(st *store.Store) csil.SubscriptionService {
	return &subscriptionService{store: st}
}

// validSubscriptionTargetType reports whether t is one of the three kinds a
// subscription may point at (csil/types/common.csil's TargetType is shared
// across services; "board" is only meaningful here and in read/unread, it's
// never a valid endorsement/revision target, but that's enforced by those
// services, not this one).
func validSubscriptionTargetType(t csil.TargetType) bool {
	switch t {
	case "board", "post", "comment":
		return true
	default:
		return false
	}
}

// currentUserID resolves the caller for a fallible (ServiceError-bearing)
// op, returning Unauthenticated if there's no session — every op in this
// service requires a logged-in caller (there's no such thing as an
// anonymous subscription).
func currentUserID(ctx context.Context) (string, *AppError) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return "", Unauthenticated("login required")
	}
	return u.ID, nil
}

func (s *subscriptionService) Subscribe(ctx context.Context, req csil.TargetRef) (csil.Subscription, error) {
	userID, aerr := currentUserID(ctx)
	if aerr != nil {
		return csil.Subscription{}, aerr
	}
	if !validSubscriptionTargetType(req.TargetType) {
		return csil.Subscription{}, Validation("target_type", "target_type must be board, post, or comment")
	}
	exists, err := s.store.TargetExists(ctx, string(req.TargetType), req.TargetId)
	if err != nil {
		return csil.Subscription{}, Internal("checking subscription target: " + err.Error())
	}
	if !exists {
		return csil.Subscription{}, NotFound(string(req.TargetType), "subscription target not found")
	}

	sub, err := s.store.Subscribe(ctx, userID, string(req.TargetType), req.TargetId)
	if err != nil {
		return csil.Subscription{}, Internal("subscribing: " + err.Error())
	}
	return encodeSubscription(sub), nil
}

func (s *subscriptionService) Unsubscribe(ctx context.Context, req csil.TargetRef) (csil.Empty, error) {
	userID, aerr := currentUserID(ctx)
	if aerr != nil {
		return csil.Empty{}, aerr
	}
	if !validSubscriptionTargetType(req.TargetType) {
		return csil.Empty{}, Validation("target_type", "target_type must be board, post, or comment")
	}
	// No target-existence check here, deliberately: unsubscribing from a
	// target that's since been deleted (or never existed) must still
	// succeed — unsubscribe is idempotent both ways (PLANDOC.md §7 B6).
	if err := s.store.Unsubscribe(ctx, userID, string(req.TargetType), req.TargetId); err != nil {
		return csil.Empty{}, Internal("unsubscribing: " + err.Error())
	}
	return csil.Empty{}, nil
}

func (s *subscriptionService) SetMuted(ctx context.Context, req csil.SetMutedRequest) (csil.Subscription, error) {
	userID, aerr := currentUserID(ctx)
	if aerr != nil {
		return csil.Subscription{}, aerr
	}
	if !validSubscriptionTargetType(req.TargetType) {
		return csil.Subscription{}, Validation("target_type", "target_type must be board, post, or comment")
	}
	exists, err := s.store.TargetExists(ctx, string(req.TargetType), req.TargetId)
	if err != nil {
		return csil.Subscription{}, Internal("checking mute target: " + err.Error())
	}
	if !exists {
		return csil.Subscription{}, NotFound(string(req.TargetType), "mute target not found")
	}

	sub, err := s.store.SetSubscriptionMuted(ctx, userID, string(req.TargetType), req.TargetId, req.Muted)
	if err != nil {
		return csil.Subscription{}, Internal("setting mute: " + err.Error())
	}
	return encodeSubscription(sub), nil
}

func (s *subscriptionService) ListSubscriptions(ctx context.Context, req csil.Empty) (csil.SubscriptionList, error) {
	// list-subscriptions has no declared ServiceError arm: an anonymous
	// caller isn't an error, it's a caller with no subscriptions.
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.SubscriptionList{}, nil
	}
	subs, err := s.store.ListSubscriptions(ctx, u.ID)
	if err != nil {
		return csil.SubscriptionList{}, err
	}
	out := make([]csil.Subscription, 0, len(subs))
	for i := range subs {
		out = append(out, encodeSubscription(&subs[i]))
	}
	return csil.SubscriptionList{Subscriptions: out}, nil
}

func encodeSubscription(sub *store.Subscription) csil.Subscription {
	return csil.Subscription{
		Id:         csil.SubscriptionID(sub.ID),
		TargetType: csil.TargetType(sub.TargetType),
		TargetId:   sub.TargetID,
		Muted:      sub.Muted,
		CreatedAt:  sub.CreatedAt,
	}
}
