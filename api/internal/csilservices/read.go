package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// readService implements ReadService (task B6): the hybrid watermark +
// override read model (PLANDOC.md §4). None of these three ops has a
// declared ServiceError arm (csil/firepit.csil), so every method here
// treats "nothing to do" (no session, unknown target, nothing pinned) as a
// normal success rather than an error — see doc.go's contract for
// infallible ops. A returned Go error is reserved for a genuine store
// failure (DB outage), which the dispatcher logs and maps to a
// transport-level failure.
//
// # Read-model semantics settled here
//
//   - mark-read on a POST: advances read_marks.last_read_at to now() for
//     (user, post) AND clears every unread_overrides pin scoped to that
//     post — both the post's own pin and every comment under it. Rationale:
//     "mark this whole post read" is a stronger, broader action than
//     clearing one item, so it should also undo any narrower "keep unread"
//     pins inside it; leaving a stale pin around after the user just told
//     the system "I've read this thread" would make mark-read look broken.
//   - mark-read on a COMMENT: clears only that single comment's
//     unread_overrides pin (if any). It deliberately does NOT touch the
//     post's watermark or any other comment's pin — marking one comment
//     read must not silently mark sibling/later comments read too. If the
//     comment had no pin, this is a no-op success (mark-read is
//     idempotent).
//   - mark-unread on a POST or COMMENT: inserts an unread_overrides pin
//     (ON CONFLICT DO NOTHING — idempotent). This is the only way content
//     already covered by the watermark becomes unread again; the pin beats
//     the watermark unconditionally until cleared (see
//     store.unreadSummaryQuery's override_posts/override_comments CTEs).
//   - unread-summary: see store.UnreadSummary's doc comment for the full
//     query and its plan.
type readService struct {
	store *store.Store
}

// NewReadService constructs the ReadService implementation.
func NewReadService(st *store.Store) csil.ReadService {
	return &readService{store: st}
}

func (s *readService) MarkRead(ctx context.Context, req csil.TargetRef) (csil.Empty, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, nil
	}

	switch req.TargetType {
	case "post":
		exists, err := s.store.TargetExists(ctx, "post", req.TargetId)
		if err != nil {
			return csil.Empty{}, err
		}
		if !exists {
			return csil.Empty{}, nil
		}
		if err := s.store.MarkPostRead(ctx, u.ID, req.TargetId); err != nil {
			return csil.Empty{}, err
		}
	case "comment":
		exists, err := s.store.TargetExists(ctx, "comment", req.TargetId)
		if err != nil {
			return csil.Empty{}, err
		}
		if !exists {
			return csil.Empty{}, nil
		}
		if err := s.store.ClearCommentOverride(ctx, u.ID, req.TargetId); err != nil {
			return csil.Empty{}, err
		}
	default:
		// "board" (or anything else) isn't a valid mark-read target — read
		// state is tracked per post/comment only. No-op, not an error.
	}
	return csil.Empty{}, nil
}

func (s *readService) MarkUnread(ctx context.Context, req csil.TargetRef) (csil.Empty, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, nil
	}

	switch req.TargetType {
	case "post", "comment":
	default:
		return csil.Empty{}, nil
	}

	exists, err := s.store.TargetExists(ctx, string(req.TargetType), req.TargetId)
	if err != nil {
		return csil.Empty{}, err
	}
	if !exists {
		return csil.Empty{}, nil
	}
	if err := s.store.MarkUnread(ctx, u.ID, string(req.TargetType), req.TargetId); err != nil {
		return csil.Empty{}, err
	}
	return csil.Empty{}, nil
}

func (s *readService) UnreadSummary(ctx context.Context, req csil.Empty) (csil.UnreadSummary, error) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return csil.UnreadSummary{}, nil
	}

	rows, err := s.store.UnreadSummary(ctx, u.ID)
	if err != nil {
		return csil.UnreadSummary{}, err
	}

	boards := make([]csil.BoardUnread, 0, len(rows))
	for _, r := range rows {
		postIDs := make([]csil.PostID, 0, len(r.PostIDs))
		for _, id := range r.PostIDs {
			postIDs = append(postIDs, csil.PostID(id))
		}
		boards = append(boards, csil.BoardUnread{
			BoardId:       csil.BoardID(r.BoardID),
			UnreadCount:   uint64(r.UnreadCount),
			UnreadPostIds: postIDs,
		})
	}
	return csil.UnreadSummary{Boards: boards}, nil
}
