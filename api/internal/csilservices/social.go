package csilservices

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// socialService implements SocialService (task B9). Friend groups are
// PRIVATE to their owner (PLANDOC.md §4, §7 B9's acceptance criterion) —
// every method below resolves "which user" from reqctx.User and
// scopes every store call to that user's own ID; there is no path here
// that reads or mutates another user's group. A group ID that belongs to
// someone else reads back as NotFound, never Forbidden, so a caller can
// never learn that a given group ID exists at all (see doc.go's NotFound
// contract, and api/internal/store/social.go's package-level comment for
// the store-layer half of this invariant). Don't add a lookup here that
// takes a raw GroupID and skips store's owner-scoped methods — that would
// reopen the privacy leak this file exists to prevent.
type socialService struct {
	store *store.Store
}

// NewSocialService constructs the SocialService implementation.
func NewSocialService(st *store.Store) csil.SocialService {
	return &socialService{store: st}
}

// maxFriendGroupNameLen enforces the 1-100 character range from PLANDOC.md
// §7 task B9. csil/types' generated FriendGroup.Validate()/
// CreateFriendGroupRequest.Validate() already cap Name at 128 (the CSIL
// schema's own, looser bound); this is the stricter, product-specified
// limit enforced in the service layer on top of that.
const maxFriendGroupNameLen = 100

// socialCurrentUserID resolves the caller for every fallible op below (all
// of which have a declared ServiceError arm in csil/firepit.csil) — so "no
// session" is always an expected failure named via Unauthenticated, never a
// plain error. Named socialService-specifically (rather than a generic
// currentUserID) since every other B-wave service in this package is
// likely to want an equivalent helper of its own, and package-level names
// must be unique across every file here.
func socialCurrentUserID(ctx context.Context) (string, *AppError) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return "", Unauthenticated("a valid session is required")
	}
	return u.ID, nil
}

// validateFriendGroupName enforces the 1-100 character range (name is
// assumed already whitespace-trimmed by the caller), returning a Validation
// AppError naming the "name" field on failure. Pulled out as a pure
// function so it's unit-testable at its exact boundaries without a store.
func validateFriendGroupName(name string) *AppError {
	if len(name) < 1 || len(name) > maxFriendGroupNameLen {
		return Validation("name", "name must be between 1 and 100 characters")
	}
	return nil
}

// list-friend-groups has no declared ServiceError arm; see
// SettingsService.GetSettings's doc comment for why "no session" becomes a
// plain error rather than a typed one here.
func (s *socialService) ListFriendGroups(ctx context.Context, req csil.Empty) (csil.FriendGroupList, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.FriendGroupList{}, fmt.Errorf("csilservices: list-friend-groups called without an authenticated session")
	}
	groups, err := s.store.ListFriendGroupsByOwner(ctx, u.ID)
	if err != nil {
		return csil.FriendGroupList{}, err
	}
	out := make([]csil.FriendGroup, len(groups))
	for i, g := range groups {
		out[i] = toCSILFriendGroup(g)
	}
	return csil.FriendGroupList{Groups: out}, nil
}

func (s *socialService) CreateFriendGroup(ctx context.Context, req csil.CreateFriendGroupRequest) (csil.FriendGroup, error) {
	userID, appErr := socialCurrentUserID(ctx)
	if appErr != nil {
		return csil.FriendGroup{}, appErr
	}
	name := strings.TrimSpace(req.Name)
	if appErr := validateFriendGroupName(name); appErr != nil {
		return csil.FriendGroup{}, appErr
	}

	group, err := s.store.CreateFriendGroup(ctx, userID, name)
	if err != nil {
		if errors.Is(err, store.ErrFriendGroupNameTaken) {
			return csil.FriendGroup{}, Conflict("you already have a friend group with that name")
		}
		return csil.FriendGroup{}, err
	}
	return csil.FriendGroup{
		Id:        csil.GroupID(group.ID),
		Name:      group.Name,
		Members:   []csil.UserID{},
		CreatedAt: group.CreatedAt,
	}, nil
}

func (s *socialService) DeleteFriendGroup(ctx context.Context, req csil.GroupID) (csil.Empty, error) {
	userID, appErr := socialCurrentUserID(ctx)
	if appErr != nil {
		return csil.Empty{}, appErr
	}
	if err := s.store.DeleteFriendGroup(ctx, userID, string(req)); err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("friend_group", "friend group not found")
		}
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

func (s *socialService) AddFriend(ctx context.Context, req csil.AddFriendRequest) (csil.Empty, error) {
	userID, appErr := socialCurrentUserID(ctx)
	if appErr != nil {
		return csil.Empty{}, appErr
	}
	if string(req.UserId) == userID {
		return csil.Empty{}, Validation("user_id", "you can't add yourself to a friend group")
	}
	if _, err := s.store.GetUser(ctx, string(req.UserId)); err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("user", "user not found")
		}
		return csil.Empty{}, err
	}
	if err := s.store.AddFriendGroupMember(ctx, userID, string(req.GroupId), string(req.UserId)); err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("friend_group", "friend group not found")
		}
		if errors.Is(err, store.ErrSelfFriend) {
			// Reached only if a future caller skips the self-check above
			// (e.g. a direct store user) — see store.ErrSelfFriend's doc
			// comment.
			return csil.Empty{}, Validation("user_id", "you can't add yourself to a friend group")
		}
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

func (s *socialService) RemoveFriend(ctx context.Context, req csil.RemoveFriendRequest) (csil.Empty, error) {
	userID, appErr := socialCurrentUserID(ctx)
	if appErr != nil {
		return csil.Empty{}, appErr
	}
	if err := s.store.RemoveFriendGroupMember(ctx, userID, string(req.GroupId), string(req.UserId)); err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("friend_group", "friend group not found")
		}
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

// toCSILFriendGroup converts a store row (with its members already loaded)
// to the wire type.
func toCSILFriendGroup(g store.FriendGroupWithMembers) csil.FriendGroup {
	members := make([]csil.UserID, len(g.MemberUserIDs))
	for i, m := range g.MemberUserIDs {
		members[i] = csil.UserID(m)
	}
	return csil.FriendGroup{
		Id:        csil.GroupID(g.ID),
		Name:      g.Name,
		Members:   members,
		CreatedAt: g.CreatedAt,
	}
}
