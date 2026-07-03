package csilservices

import (
	"context"
	"fmt"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// settingsService implements SettingsService (task B9). See doc.go for the
// error-handling contract and reqctx.User's doc comment (in
// api/internal/store/context.go) for why "who's calling" is read via the
// store package rather than api/internal/server.
type settingsService struct {
	store *store.Store
}

// NewSettingsService constructs the SettingsService implementation.
func NewSettingsService(st *store.Store) csil.SettingsService {
	return &settingsService{store: st}
}

// validMentionPolicies is the fixed enum csil/types declares
// MentionPolicy as a bare string alias for (csil/firepit.csil, PLANDOC.md
// §4) rather than a generated CSIL enum with its own Validate() check —
// this is the one place server-side that enforces it.
var validMentionPolicies = map[csil.MentionPolicy]bool{
	"subscribed": true,
	"everyone":   true,
	"authorized": true,
	"nobody":     true,
}

// get-settings has no declared ServiceError arm (csil/firepit.csil), so per
// doc.go's contract any error it returns becomes an opaque transport-level
// failure — there's no typed channel to say "please log in" through. In
// practice this endpoint is only ever called by a client that already
// completed AuthService.begin-login, so hitting it unauthenticated is a
// client bug, not a normal path; the plain (non-*AppError) error below
// reflects that: it's logged server-side and never shown to the caller
// verbatim.
func (s *settingsService) GetSettings(ctx context.Context, req csil.Empty) (csil.UserSettings, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.UserSettings{}, fmt.Errorf("csilservices: get-settings called without an authenticated session")
	}
	settings, err := s.store.GetOrCreateUserSettings(ctx, u.ID)
	if err != nil {
		return csil.UserSettings{}, err
	}
	return toCSILUserSettings(settings), nil
}

// update-settings DOES have a declared ServiceError arm, so unlike
// get-settings above, "no session" is an expected failure this method can
// name via Unauthenticated.
func (s *settingsService) UpdateSettings(ctx context.Context, req csil.UpdateSettingsRequest) (csil.UserSettings, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.UserSettings{}, Unauthenticated("a valid session is required")
	}
	if req.MentionPolicy != nil && !validMentionPolicies[*req.MentionPolicy] {
		return csil.UserSettings{}, Validation("mention_policy", fmt.Sprintf("unknown mention policy %q", *req.MentionPolicy))
	}

	var mentionPolicy *string
	if req.MentionPolicy != nil {
		v := string(*req.MentionPolicy)
		mentionPolicy = &v
	}

	settings, err := s.store.UpdateUserSettings(ctx, u.ID, mentionPolicy, req.NotifyOnEndorse)
	if err != nil {
		return csil.UserSettings{}, err
	}
	return toCSILUserSettings(settings), nil
}

// list-mention-grants has no declared ServiceError arm; see GetSettings's
// doc comment for why "no session" becomes a plain error here.
func (s *settingsService) ListMentionGrants(ctx context.Context, req csil.Empty) (csil.MentionGrantList, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.MentionGrantList{}, fmt.Errorf("csilservices: list-mention-grants called without an authenticated session")
	}
	grants, err := s.store.ListMentionGrants(ctx, u.ID)
	if err != nil {
		return csil.MentionGrantList{}, err
	}
	out := make([]csil.MentionGrant, len(grants))
	for i, g := range grants {
		out[i] = csil.MentionGrant{UserId: csil.UserID(g.GrantedUserID), CreatedAt: g.CreatedAt}
	}
	return csil.MentionGrantList{Grants: out}, nil
}

func (s *settingsService) GrantMention(ctx context.Context, req csil.UserID) (csil.Empty, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("a valid session is required")
	}
	target := string(req)
	if target == u.ID {
		return csil.Empty{}, Validation("user_id", "you can't grant yourself mention access")
	}
	if _, err := s.store.GetUser(ctx, target); err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("user", "user not found")
		}
		return csil.Empty{}, err
	}
	if err := s.store.GrantMention(ctx, u.ID, target); err != nil {
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

func (s *settingsService) RevokeMention(ctx context.Context, req csil.UserID) (csil.Empty, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("a valid session is required")
	}
	if err := s.store.RevokeMention(ctx, u.ID, string(req)); err != nil {
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

// toCSILUserSettings converts a store row to the wire type. UpdatedAt is
// optional on the wire type (omitted only for a zero-value UserSettings{}
// literal, which real rows never are), so it's always populated here.
func toCSILUserSettings(settings *store.UserSettings) csil.UserSettings {
	updatedAt := settings.UpdatedAt
	return csil.UserSettings{
		MentionPolicy:   csil.MentionPolicy(settings.MentionPolicy),
		NotifyOnEndorse: settings.NotifyOnEndorse,
		UpdatedAt:       &updatedAt,
	}
}
