package csilservices

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// Pure validation/authz-gate unit tests: every case here returns before
// socialService touches s.store, so a nil *store.Store is safe. DB-backed
// behavior (CRUD, privacy, cascade delete) is covered by the testcontainers
// integration suite in api/internal/store/social_integration_test.go.

func newSocialServiceForTest() *socialService {
	return &socialService{store: nil}
}

func TestCreateFriendGroupRejectsEmptyName(t *testing.T) {
	svc := newSocialServiceForTest()
	ctx := store.WithCurrentUser(context.Background(), &store.User{ID: "user-1"})

	_, err := svc.CreateFriendGroup(ctx, csil.CreateFriendGroupRequest{Name: "   "})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeValidation, appErr.Code)
	require.Equal(t, "name", appErr.Field)
}

func TestCreateFriendGroupRejectsOverlongName(t *testing.T) {
	svc := newSocialServiceForTest()
	ctx := store.WithCurrentUser(context.Background(), &store.User{ID: "user-1"})

	_, err := svc.CreateFriendGroup(ctx, csil.CreateFriendGroupRequest{Name: strings.Repeat("a", maxFriendGroupNameLen+1)})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeValidation, appErr.Code)
	require.Equal(t, "name", appErr.Field)
}

func TestValidateFriendGroupNameBoundaries(t *testing.T) {
	require.Nil(t, validateFriendGroupName("a"), "1 char is the documented minimum")
	require.Nil(t, validateFriendGroupName(strings.Repeat("a", maxFriendGroupNameLen)), "100 chars is the documented maximum")

	require.NotNil(t, validateFriendGroupName(""), "0 chars is below the minimum")
	require.NotNil(t, validateFriendGroupName(strings.Repeat("a", maxFriendGroupNameLen+1)), "101 chars is above the maximum")
}

func TestCreateFriendGroupRequiresSession(t *testing.T) {
	svc := newSocialServiceForTest()

	_, err := svc.CreateFriendGroup(context.Background(), csil.CreateFriendGroupRequest{Name: "circle"})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestDeleteFriendGroupRequiresSession(t *testing.T) {
	svc := newSocialServiceForTest()

	_, err := svc.DeleteFriendGroup(context.Background(), csil.GroupID("group-1"))

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestAddFriendRejectsSelfAdd(t *testing.T) {
	svc := newSocialServiceForTest()
	ctx := store.WithCurrentUser(context.Background(), &store.User{ID: "user-1"})

	_, err := svc.AddFriend(ctx, csil.AddFriendRequest{GroupId: "group-1", UserId: "user-1"})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeValidation, appErr.Code)
	require.Equal(t, "user_id", appErr.Field)
}

func TestAddFriendRequiresSession(t *testing.T) {
	svc := newSocialServiceForTest()

	_, err := svc.AddFriend(context.Background(), csil.AddFriendRequest{GroupId: "group-1", UserId: "user-2"})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestRemoveFriendRequiresSession(t *testing.T) {
	svc := newSocialServiceForTest()

	_, err := svc.RemoveFriend(context.Background(), csil.RemoveFriendRequest{GroupId: "group-1", UserId: "user-2"})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestListFriendGroupsRequiresSession(t *testing.T) {
	svc := newSocialServiceForTest()

	_, err := svc.ListFriendGroups(context.Background(), csil.Empty{})

	require.Error(t, err)
	var appErr *AppError
	require.NotErrorAs(t, err, &appErr, "list-friend-groups has no ServiceError arm; see settings_test.go's equivalent get-settings case")
}

func TestToCSILFriendGroupEmptyMembersIsNotNil(t *testing.T) {
	g := toCSILFriendGroup(store.FriendGroupWithMembers{
		FriendGroup:   store.FriendGroup{ID: "group-1", Name: "circle"},
		MemberUserIDs: nil,
	})

	require.Equal(t, csil.GroupID("group-1"), g.Id)
	require.NotNil(t, g.Members)
	require.Empty(t, g.Members)
}
