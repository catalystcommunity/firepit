//go:build integration

package csilservices_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

func TestCategoriesFilterRouteAndDelete(t *testing.T) {
	threadSvc, st, _ := threadServiceEnv(t)
	categorySvc := csilservices.NewCategoryService(st)
	admin := mkUser(t, st, "category-admin", "admin")
	origin := mkBoard(t, st, "origin-board", admin)
	target := mkBoard(t, st, "target-board", admin)
	origin.CategoryLimit = 1
	require.NoError(t, st.DB.Save(origin).Error)

	shared, err := categorySvc.CreateCategory(asUser(admin), csil.CreateCategoryRequest{
		Slug: "release", Name: "Release", CrossBoardPosting: true,
		BoardIds: []csil.BoardID{csil.BoardID(origin.ID), csil.BoardID(target.ID)},
	})
	require.NoError(t, err)
	local, err := categorySvc.CreateCategory(asUser(admin), csil.CreateCategoryRequest{
		Slug: "local", Name: "Local", BoardIds: []csil.BoardID{csil.BoardID(origin.ID)},
	})
	require.NoError(t, err)
	allCategories, err := categorySvc.ListCategories(context.Background(), csil.Empty{})
	require.NoError(t, err)
	require.Len(t, allCategories.Categories, 2)

	sharedPost, err := threadSvc.CreatePost(asUser(admin), csil.CreatePostRequest{
		BoardId: csil.BoardID(origin.ID), CategoryIds: []csil.CategoryID{shared.Id}, Title: "Shared", BodyMd: "body",
	})
	require.NoError(t, err)
	_, err = threadSvc.CreatePost(asUser(admin), csil.CreatePostRequest{
		BoardId: csil.BoardID(origin.ID), Title: "No category", BodyMd: "body",
	})
	require.NoError(t, err)

	_, err = threadSvc.CreatePost(asUser(admin), csil.CreatePostRequest{
		BoardId: csil.BoardID(origin.ID), CategoryIds: []csil.CategoryID{shared.Id, local.Id}, Title: "Too many", BodyMd: "body",
	})
	requireAppErrorCode(t, err, csilservices.CodeValidation)

	page, err := threadSvc.ListPosts(context.Background(), csil.ListPostsRequest{BoardId: csil.BoardID(target.ID)})
	require.NoError(t, err)
	require.Len(t, page.Posts, 1)
	require.Equal(t, sharedPost.Id, page.Posts[0].Id)

	includeUncategorized := true
	page, err = threadSvc.ListPosts(context.Background(), csil.ListPostsRequest{
		BoardId: csil.BoardID(origin.ID), CategoryIds: []csil.CategoryID{}, IncludeUncategorized: &includeUncategorized,
	})
	require.NoError(t, err)
	require.Len(t, page.Posts, 1)
	require.Empty(t, page.Posts[0].CategoryIds)

	subscriber := mkUser(t, st, "shared-board-subscriber")
	require.NoError(t, st.DB.Create(&store.Subscription{
		UserID: subscriber.ID, TargetType: "board", TargetID: target.ID,
	}).Error)
	notifyingThreadSvc := csilservices.NewThreadService(st, notify.NewDBPublisher())
	notifiedPost, err := notifyingThreadSvc.CreatePost(asUser(admin), csil.CreatePostRequest{
		BoardId: csil.BoardID(origin.ID), CategoryIds: []csil.CategoryID{shared.Id}, Title: "Shared notification", BodyMd: "body",
	})
	require.NoError(t, err)
	var notification store.Notification
	require.NoError(t, st.DB.First(&notification, "user_id = ? AND post_id = ?", subscriber.ID, string(notifiedPost.Id)).Error)

	unread, err := st.UnreadSummary(context.Background(), subscriber.ID)
	require.NoError(t, err)
	require.Len(t, unread, 1)
	require.Equal(t, target.ID, unread[0].BoardID)

	_, err = categorySvc.DeleteCategory(asUser(admin), csil.DeleteCategoryRequest{Id: shared.Id})
	requireAppErrorCode(t, err, csilservices.CodeValidation)
	_, err = categorySvc.DeleteCategory(asUser(admin), csil.DeleteCategoryRequest{Id: shared.Id, RemoveFromPosts: true})
	require.NoError(t, err)

	thread, err := threadSvc.GetThread(context.Background(), csil.GetThreadRequest{PostId: sharedPost.Id})
	require.NoError(t, err)
	require.Empty(t, thread.Post.CategoryIds, "deleted category must leave the post Uncategorized")
	page, err = threadSvc.ListPosts(context.Background(), csil.ListPostsRequest{BoardId: csil.BoardID(target.ID)})
	require.NoError(t, err)
	require.Empty(t, page.Posts, "the post must stop routing when the shared category is deleted")
}
