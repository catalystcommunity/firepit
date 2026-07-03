package csilservices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// threadService is the ThreadService implementation (task B4): posts,
// threaded comments, edits with revision snapshots, and soft-delete
// tombstones. See the package doc comment (doc.go) for the error-handling
// contract every method here follows, and thread_cursor.go for the
// list-posts pagination scheme.
type threadService struct {
	store  *store.Store
	notify notify.Publisher
}

// NewThreadService constructs the ThreadService implementation. pub is the
// notification fan-out seam (task B7) — write paths that create or edit
// content call pub.Publish inside the same transaction as the content
// write itself, so a notification never commits without the content that
// caused it (or vice versa). Wire notify.Noop{} until B7 lands.
func NewThreadService(st *store.Store, pub notify.Publisher) csil.ThreadService {
	return &threadService{store: st, notify: pub}
}

// Content bounds. The wire schema (csil/types/threads.csil) already caps
// title at 512 bytes and body at 100000 bytes — those limits are enforced
// unconditionally by the generated codec's caller-invoked Validate()
// methods (api/internal/csil/validation.gen.go) wherever a caller runs
// them, and are load-bearing regardless of what this package does. maxTitleLen
// here is a *stricter*, product-level bound (PLANDOC's task brief calls for
// 1-200) layered on top — a title of 201-512 bytes still round-trips the
// wire fine but is rejected here as a ServiceError. maxBodyLen matches the
// wire cap exactly; it's asserted explicitly anyway (rather than relying on
// callers to invoke Validate()) so a blank-after-trim body is also caught.
const (
	maxTitleLen = 200
	maxBodyLen  = 100000
)

// tombstonePlaceholder replaces a deleted post/comment's title/body when
// serialized to the wire. It's non-empty (rather than "") so it still
// satisfies the wire schema's `.size (1..N)` minimum on body_md/title,
// which a strict client-side mirror of that validation might otherwise
// reject on a live decode.
const tombstonePlaceholder = "[deleted]"

// instanceAdminRole is the users.roles value that grants instance-wide
// moderation rights, per csil/types/auth.csil's UserProfile.roles doc
// comment ("currently just \"admin\"").
const instanceAdminRole = "admin"

// ListPosts returns a cursor-paginated, activity-ordered page of a board's
// posts. Has no declared ServiceError arm (csil/firepit.csil), so a
// malformed board_id or cursor never fails the request outright — an
// unknown board_id just yields an empty page (a real "no posts" case looks
// identical from the wire's perspective, and this op has no channel to
// distinguish them), and an undecodable cursor is treated as "no cursor"
// (start from the first page) rather than surfaced as an internal failure
// for what's very likely a client-side bug, not a server fault.
func (s *threadService) ListPosts(ctx context.Context, req csil.ListPostsRequest) (csil.PostPage, error) {
	limit := postPageLimit(req.Limit)

	var after *store.PostListCursor
	if req.Cursor != nil {
		if c, err := decodePostCursor(*req.Cursor); err == nil {
			after = c.toStoreCursor()
		}
	}

	posts, err := s.store.ListPostsByBoard(ctx, s.store.DB, string(req.BoardId), after, limit+1)
	if err != nil {
		return csil.PostPage{}, err
	}

	var next *csil.PageCursor
	if len(posts) > limit {
		posts = posts[:limit]
		last := posts[len(posts)-1]
		c := encodePostCursor(postCursor{ActivityAt: last.LastActivityAt, ID: last.ID})
		next = &c
	}

	out := csil.PostPage{Posts: make([]csil.Post, 0, len(posts)), NextCursor: next}
	for i := range posts {
		out.Posts = append(out.Posts, toCSILPost(&posts[i]))
	}
	return out, nil
}

// GetThread returns a post plus its whole comment tree in one indexed
// query (store.ListCommentsByPost), depth-first ordered via the comments'
// ltree path. Has no declared ServiceError arm, so an unknown post_id
// yields a zero-value Thread{} (Post.Id == "") rather than an error — the
// only channel available to an infallible op. A soft-deleted
// (tombstoned) post is still returned (with title/body redacted) so its
// surviving replies aren't orphaned from a direct permalink; only
// list-posts excludes deleted posts from the activity feed.
func (s *threadService) GetThread(ctx context.Context, req csil.GetThreadRequest) (csil.Thread, error) {
	post, err := s.store.GetPost(ctx, s.store.DB, string(req.PostId))
	if err != nil {
		if store.IsNotFound(err) {
			return csil.Thread{}, nil
		}
		return csil.Thread{}, err
	}

	comments, err := s.store.ListCommentsByPost(ctx, post.ID)
	if err != nil {
		return csil.Thread{}, err
	}

	out := csil.Thread{Post: toCSILPost(post), Comments: make([]csil.Comment, 0, len(comments))}
	for i := range comments {
		out.Comments = append(out.Comments, toCSILComment(&comments[i]))
	}
	return out, nil
}

// CreatePost starts a new thread on a board: validates title/body,
// confirms the board exists and isn't archived, inserts the post, and
// publishes a KindContentCreated event — all in one transaction.
func (s *threadService) CreatePost(ctx context.Context, req csil.CreatePostRequest) (csil.Post, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Post{}, Unauthenticated("must be logged in to create a post")
	}
	title, body, verr := validateTitleAndBody(req.Title, req.BodyMd)
	if verr != nil {
		return csil.Post{}, verr
	}

	var post store.Post
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var board store.Board
		if err := tx.First(&board, "id = ?", string(req.BoardId)).Error; err != nil {
			if store.IsNotFound(err) {
				return NotFound("board", "board not found")
			}
			return err
		}
		if board.ArchivedAt != nil {
			return Validation("board_id", "board is archived; new posts are not accepted")
		}

		post = store.Post{
			BoardID:        board.ID,
			AuthorID:       user.ID,
			Title:          title,
			BodyMD:         body,
			LastActivityAt: time.Now().UTC(),
		}
		if err := s.store.CreatePost(ctx, tx, &post); err != nil {
			return err
		}

		return s.notify.Publish(ctx, tx, notify.Event{
			Kind:    notify.KindContentCreated,
			ActorID: user.ID,
			BoardID: board.ID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})
	})
	if txErr != nil {
		return csil.Post{}, asAppError(txErr)
	}
	return toCSILPost(&post), nil
}

// CreateComment replies to a post, optionally nested under an existing
// comment (parent_comment_id absent => a top-level reply to the post
// itself). Validates body, that the post exists/isn't deleted/isn't on an
// archived board, and — when nested — that the parent comment belongs to
// the same post and isn't itself deleted. Computes the new comment's ltree
// path (store.ChildPath), bumps the post's comment_count/last_activity_at,
// and publishes a KindContentCreated event — all in one transaction.
func (s *threadService) CreateComment(ctx context.Context, req csil.CreateCommentRequest) (csil.Comment, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Comment{}, Unauthenticated("must be logged in to comment")
	}
	body, verr := validateBody(req.BodyMd)
	if verr != nil {
		return csil.Comment{}, verr
	}

	var comment store.Comment
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var post store.Post
		if err := tx.First(&post, "id = ?", string(req.PostId)).Error; err != nil {
			if store.IsNotFound(err) {
				return NotFound("post", "post not found")
			}
			return err
		}
		if post.DeletedAt != nil {
			return Validation("post_id", "cannot reply to a deleted post")
		}

		var board store.Board
		if err := tx.First(&board, "id = ?", post.BoardID).Error; err != nil {
			return err
		}
		if board.ArchivedAt != nil {
			return Validation("post_id", "board is archived; new comments are not accepted")
		}

		var parentPath store.Ltree
		var parentID *string
		if req.ParentCommentId != nil {
			var parent store.Comment
			if err := tx.First(&parent, "id = ?", string(*req.ParentCommentId)).Error; err != nil {
				if store.IsNotFound(err) {
					return NotFound("comment", "parent comment not found")
				}
				return err
			}
			if parent.PostID != post.ID {
				return Validation("parent_comment_id", "parent comment belongs to a different post")
			}
			if parent.DeletedAt != nil {
				return Validation("parent_comment_id", "cannot reply to a deleted comment")
			}
			parentPath = parent.Path
			pid := parent.ID
			parentID = &pid
		}

		newID, err := store.NewCommentID()
		if err != nil {
			return err
		}
		comment = store.Comment{
			ID:              newID,
			PostID:          post.ID,
			ParentCommentID: parentID,
			AuthorID:        user.ID,
			Path:            store.ChildPath(parentPath, newID),
			BodyMD:          body,
		}
		if err := s.store.CreateComment(ctx, tx, &comment); err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := s.store.BumpPostActivity(ctx, tx, post.ID, now, 1); err != nil {
			return err
		}

		return s.notify.Publish(ctx, tx, notify.Event{
			Kind:      notify.KindContentCreated,
			ActorID:   user.ID,
			BoardID:   post.BoardID,
			PostID:    post.ID,
			CommentID: comment.ID,
			Body:      comment.BodyMD,
		})
	})
	if txErr != nil {
		return csil.Comment{}, asAppError(txErr)
	}
	return toCSILComment(&comment), nil
}

// EditPost is author-only. Snapshots the prior title/body into a Revision,
// applies the edit, sets edited_at, and publishes a KindContentEdited
// event (carrying the NEW body, so mention fan-out can parse newly added
// @handles) — all in one transaction.
func (s *threadService) EditPost(ctx context.Context, req csil.EditPostRequest) (csil.Post, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Post{}, Unauthenticated("must be logged in to edit a post")
	}
	title, body, verr := validateTitleAndBody(req.Title, req.BodyMd)
	if verr != nil {
		return csil.Post{}, verr
	}

	var post store.Post
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&post, "id = ?", string(req.Id)).Error; err != nil {
			if store.IsNotFound(err) {
				return NotFound("post", "post not found")
			}
			return err
		}
		if post.DeletedAt != nil {
			return NotFound("post", "post not found")
		}
		if post.AuthorID != user.ID {
			return Forbidden("only the author can edit this post")
		}

		if err := s.store.CreateRevision(ctx, tx, &store.Revision{
			TargetType: "post",
			TargetID:   post.ID,
			EditorID:   user.ID,
			PrevTitle:  &post.Title,
			PrevBodyMD: post.BodyMD,
		}); err != nil {
			return err
		}

		now := time.Now().UTC()
		post.Title = title
		post.BodyMD = body
		post.EditedAt = &now
		if err := s.store.UpdatePost(ctx, tx, &post); err != nil {
			return err
		}

		return s.notify.Publish(ctx, tx, notify.Event{
			Kind:    notify.KindContentEdited,
			ActorID: user.ID,
			BoardID: post.BoardID,
			PostID:  post.ID,
			Body:    post.BodyMD,
		})
	})
	if txErr != nil {
		return csil.Post{}, asAppError(txErr)
	}
	return toCSILPost(&post), nil
}

// EditComment is author-only. Snapshots the prior body into a Revision
// (comments have no title, so Revision.PrevTitle is left absent), applies
// the edit, sets edited_at, and publishes a KindContentEdited event — all
// in one transaction.
func (s *threadService) EditComment(ctx context.Context, req csil.EditCommentRequest) (csil.Comment, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Comment{}, Unauthenticated("must be logged in to edit a comment")
	}
	body, verr := validateBody(req.BodyMd)
	if verr != nil {
		return csil.Comment{}, verr
	}

	var comment store.Comment
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&comment, "id = ?", string(req.Id)).Error; err != nil {
			if store.IsNotFound(err) {
				return NotFound("comment", "comment not found")
			}
			return err
		}
		if comment.DeletedAt != nil {
			return NotFound("comment", "comment not found")
		}
		if comment.AuthorID != user.ID {
			return Forbidden("only the author can edit this comment")
		}

		var post store.Post
		if err := tx.First(&post, "id = ?", comment.PostID).Error; err != nil {
			return err
		}

		if err := s.store.CreateRevision(ctx, tx, &store.Revision{
			TargetType: "comment",
			TargetID:   comment.ID,
			EditorID:   user.ID,
			PrevBodyMD: comment.BodyMD,
		}); err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := s.store.UpdateCommentBody(ctx, tx, comment.ID, body, now); err != nil {
			return err
		}
		comment.BodyMD = body
		comment.EditedAt = &now

		return s.notify.Publish(ctx, tx, notify.Event{
			Kind:      notify.KindContentEdited,
			ActorID:   user.ID,
			BoardID:   post.BoardID,
			PostID:    post.ID,
			CommentID: comment.ID,
			Body:      comment.BodyMD,
		})
	})
	if txErr != nil {
		return csil.Comment{}, asAppError(txErr)
	}
	return toCSILComment(&comment), nil
}

// ListRevisions returns a target's edit history, most recent first.
// History is readable by anyone who can read the item (PLANDOC.md §4), so
// this deliberately performs no authorization check of its own — same
// posture as GetThread. Has no declared ServiceError arm; an unrecognized
// target_type/target_id just yields an empty list.
func (s *threadService) ListRevisions(ctx context.Context, req csil.TargetRef) (csil.RevisionList, error) {
	revisions, err := s.store.ListRevisions(ctx, s.store.DB, string(req.TargetType), req.TargetId)
	if err != nil {
		return csil.RevisionList{}, err
	}
	out := csil.RevisionList{Revisions: make([]csil.Revision, 0, len(revisions))}
	for i := range revisions {
		out.Revisions = append(out.Revisions, toCSILRevision(&revisions[i]))
	}
	return out, nil
}

// DeletePost soft-deletes a post. Allowed for the post's author, a
// maintainer/moderator of its board, or an instance admin (users.roles
// contains "admin") — read directly off board_members/users, not cached.
// Deleting an already-deleted post is a no-op success (idempotent) once
// the caller is confirmed authorized, not a NotFound — the post still
// exists, just tombstoned.
func (s *threadService) DeletePost(ctx context.Context, req csil.PostID) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("must be logged in to delete a post")
	}

	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var post store.Post
		if err := tx.First(&post, "id = ?", string(req)).Error; err != nil {
			if store.IsNotFound(err) {
				return NotFound("post", "post not found")
			}
			return err
		}

		allowed, err := canModerateBoard(ctx, tx, user, post.BoardID, post.AuthorID)
		if err != nil {
			return err
		}
		if !allowed {
			return Forbidden("not authorized to delete this post")
		}
		if post.DeletedAt != nil {
			return nil
		}
		return s.store.SoftDeletePost(ctx, tx, post.ID, time.Now().UTC())
	})
	if txErr != nil {
		return csil.Empty{}, asAppError(txErr)
	}
	return csil.Empty{}, nil
}

// DeleteComment soft-deletes a comment (tombstone: descendants and the
// row's structure survive untouched — see store.SoftDeleteComment).
// Authorization mirrors DeletePost: author, the post's board
// maintainer/moderator, or an instance admin.
func (s *threadService) DeleteComment(ctx context.Context, req csil.CommentID) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("must be logged in to delete a comment")
	}

	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var comment store.Comment
		if err := tx.First(&comment, "id = ?", string(req)).Error; err != nil {
			if store.IsNotFound(err) {
				return NotFound("comment", "comment not found")
			}
			return err
		}

		var post store.Post
		if err := tx.First(&post, "id = ?", comment.PostID).Error; err != nil {
			return err
		}

		allowed, err := canModerateBoard(ctx, tx, user, post.BoardID, comment.AuthorID)
		if err != nil {
			return err
		}
		if !allowed {
			return Forbidden("not authorized to delete this comment")
		}
		if comment.DeletedAt != nil {
			return nil
		}
		return s.store.SoftDeleteComment(ctx, tx, comment.ID, time.Now().UTC())
	})
	if txErr != nil {
		return csil.Empty{}, asAppError(txErr)
	}
	return csil.Empty{}, nil
}

// canModerateBoard reports whether user may delete a piece of content
// authored by authorID on boardID: the author themself, an instance admin,
// or a maintainer/moderator of that specific board. Reads board_members
// and users.roles directly (via tx, so the check is part of the same
// transaction as the delete) rather than any cached reputation/role
// concept — delete authorization is not the endorsement-reputation cache
// EndorsementService maintains (PLANDOC.md §4), it's a straight role check.
func canModerateBoard(ctx context.Context, tx *gorm.DB, user *store.User, boardID, authorID string) (bool, error) {
	if user.ID == authorID {
		return true, nil
	}
	for _, role := range user.Roles {
		if role == instanceAdminRole {
			return true, nil
		}
	}
	var count int64
	err := tx.WithContext(ctx).Model(&store.BoardMember{}).
		Where("board_id = ? AND user_id = ? AND role IN ?", boardID, user.ID, []string{"maintainer", "moderator"}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// validateTitleAndBody trims title (a single-line field with no meaningful
// leading/trailing whitespace) and enforces maxTitleLen/maxBodyLen on both,
// rejecting a blank-after-trim value that byte-length checks alone would
// miss (e.g. a title that's all spaces). body_md is returned untrimmed —
// unlike title, leading whitespace in markdown can be meaningful (indented
// code blocks) — only its blank-after-trim check uses the trimmed form.
func validateTitleAndBody(rawTitle, rawBody string) (title, body string, err *AppError) {
	title = strings.TrimSpace(rawTitle)
	if len(title) < 1 {
		return "", "", Validation("title", "title must not be blank")
	}
	if len(title) > maxTitleLen {
		return "", "", Validation("title", fmt.Sprintf("title must be at most %d characters", maxTitleLen))
	}
	body, verr := validateBody(rawBody)
	if verr != nil {
		return "", "", verr
	}
	return title, body, nil
}

// validateBody enforces the body_md bounds shared by every create/edit op.
func validateBody(rawBody string) (string, *AppError) {
	if len(strings.TrimSpace(rawBody)) < 1 {
		return "", Validation("body_md", "body must not be blank")
	}
	if len(rawBody) > maxBodyLen {
		return "", Validation("body_md", fmt.Sprintf("body must be at most %d characters", maxBodyLen))
	}
	return rawBody, nil
}

// asAppError adapts the error a Transaction closure returned to the
// *AppError every fallible method here must return: an *AppError produced
// deliberately (NotFound/Forbidden/Validation/...) passes through as-is;
// anything else (a genuine DB failure) becomes Internal rather than
// leaking a raw driver error to the caller. Only call this when err != nil.
func asAppError(err error) *AppError {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	return Internal(err.Error())
}

// toCSILPost converts a store.Post to its wire representation, redacting
// title/body/author when the post is a tombstone (deleted_at set) — see
// tombstonePlaceholder's doc comment for why a placeholder, not "".
func toCSILPost(p *store.Post) csil.Post {
	out := csil.Post{
		Id:             csil.PostID(p.ID),
		BoardId:        csil.BoardID(p.BoardID),
		AuthorId:       csil.UserID(p.AuthorID),
		Title:          p.Title,
		BodyMd:         p.BodyMD,
		Origin:         csil.OriginKind(p.Origin),
		CommentCount:   uint64(p.CommentCount),
		LastActivityAt: p.LastActivityAt,
		EditedAt:       p.EditedAt,
		DeletedAt:      p.DeletedAt,
		CreatedAt:      p.CreatedAt,
	}
	if len(p.OriginRef) > 0 {
		ref := string(p.OriginRef)
		out.OriginRef = &ref
	}
	if p.DeletedAt != nil {
		out.Title = tombstonePlaceholder
		out.BodyMd = tombstonePlaceholder
		out.AuthorId = ""
	}
	return out
}

// toCSILComment converts a store.Comment to its wire representation,
// redacting body/author (structure — id/post_id/parent_comment_id/path
// ordering/created_at — stays intact) when the comment is a tombstone.
func toCSILComment(c *store.Comment) csil.Comment {
	out := csil.Comment{
		Id:        csil.CommentID(c.ID),
		PostId:    csil.PostID(c.PostID),
		AuthorId:  csil.UserID(c.AuthorID),
		BodyMd:    c.BodyMD,
		Origin:    csil.OriginKind(c.Origin),
		EditedAt:  c.EditedAt,
		DeletedAt: c.DeletedAt,
		CreatedAt: c.CreatedAt,
	}
	if c.ParentCommentID != nil {
		pid := csil.CommentID(*c.ParentCommentID)
		out.ParentCommentId = &pid
	}
	if len(c.OriginRef) > 0 {
		ref := string(c.OriginRef)
		out.OriginRef = &ref
	}
	if c.DeletedAt != nil {
		out.BodyMd = tombstonePlaceholder
		out.AuthorId = ""
	}
	return out
}

// toCSILRevision converts a store.Revision to its wire representation.
func toCSILRevision(r *store.Revision) csil.Revision {
	return csil.Revision{
		Id:         csil.RevisionID(r.ID),
		TargetType: csil.TargetType(r.TargetType),
		TargetId:   r.TargetID,
		EditorId:   csil.UserID(r.EditorID),
		PrevTitle:  r.PrevTitle,
		PrevBodyMd: r.PrevBodyMD,
		CreatedAt:  r.CreatedAt,
	}
}
