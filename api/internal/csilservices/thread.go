package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// threadService is the stub ThreadService implementation (task B1). B4
// replaces every method body below; see the package doc comment (doc.go)
// for the error-handling contract every replacement must follow.
type threadService struct {
	store *store.Store
}

// NewThreadService constructs the ThreadService implementation.
func NewThreadService(st *store.Store) csil.ThreadService {
	return &threadService{store: st}
}

func (s *threadService) ListPosts(ctx context.Context, req csil.ListPostsRequest) (csil.PostPage, error) {
	return csil.PostPage{}, Unimplemented("ThreadService.list-posts")
}

func (s *threadService) GetThread(ctx context.Context, req csil.GetThreadRequest) (csil.Thread, error) {
	return csil.Thread{}, Unimplemented("ThreadService.get-thread")
}

func (s *threadService) CreatePost(ctx context.Context, req csil.CreatePostRequest) (csil.Post, error) {
	return csil.Post{}, Unimplemented("ThreadService.create-post")
}

func (s *threadService) CreateComment(ctx context.Context, req csil.CreateCommentRequest) (csil.Comment, error) {
	return csil.Comment{}, Unimplemented("ThreadService.create-comment")
}

func (s *threadService) EditPost(ctx context.Context, req csil.EditPostRequest) (csil.Post, error) {
	return csil.Post{}, Unimplemented("ThreadService.edit-post")
}

func (s *threadService) EditComment(ctx context.Context, req csil.EditCommentRequest) (csil.Comment, error) {
	return csil.Comment{}, Unimplemented("ThreadService.edit-comment")
}

func (s *threadService) ListRevisions(ctx context.Context, req csil.TargetRef) (csil.RevisionList, error) {
	return csil.RevisionList{}, Unimplemented("ThreadService.list-revisions")
}

func (s *threadService) DeletePost(ctx context.Context, req csil.PostID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("ThreadService.delete-post")
}

func (s *threadService) DeleteComment(ctx context.Context, req csil.CommentID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("ThreadService.delete-comment")
}
