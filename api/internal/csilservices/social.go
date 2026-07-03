package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// socialService is the stub SocialService implementation (task B1). B9
// replaces every method body below; see the package doc comment (doc.go)
// for the error-handling contract every replacement must follow.
type socialService struct {
	store *store.Store
}

// NewSocialService constructs the SocialService implementation.
func NewSocialService(st *store.Store) csil.SocialService {
	return &socialService{store: st}
}

func (s *socialService) ListFriendGroups(ctx context.Context, req csil.Empty) (csil.FriendGroupList, error) {
	return csil.FriendGroupList{}, Unimplemented("SocialService.list-friend-groups")
}

func (s *socialService) CreateFriendGroup(ctx context.Context, req csil.CreateFriendGroupRequest) (csil.FriendGroup, error) {
	return csil.FriendGroup{}, Unimplemented("SocialService.create-friend-group")
}

func (s *socialService) DeleteFriendGroup(ctx context.Context, req csil.GroupID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("SocialService.delete-friend-group")
}

func (s *socialService) AddFriend(ctx context.Context, req csil.AddFriendRequest) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("SocialService.add-friend")
}

func (s *socialService) RemoveFriend(ctx context.Context, req csil.RemoveFriendRequest) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("SocialService.remove-friend")
}
