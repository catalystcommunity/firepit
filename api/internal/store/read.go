package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReadMark mirrors the `read_marks` table: a per-post read watermark,
// advanced when the user views the thread (or explicitly mark-reads it).
type ReadMark struct {
	UserID     string    `gorm:"column:user_id;type:uuid;primaryKey"`
	PostID     string    `gorm:"column:post_id;type:uuid;primaryKey"`
	LastReadAt time.Time `gorm:"column:last_read_at;not null"`
}

func (ReadMark) TableName() string { return "read_marks" }

// UnreadOverride mirrors the `unread_overrides` table: an explicit "keep
// this unread" pin that beats the watermark until cleared.
type UnreadOverride struct {
	UserID     string    `gorm:"column:user_id;type:uuid;primaryKey"`
	TargetType string    `gorm:"column:target_type;primaryKey"`
	TargetID   string    `gorm:"column:target_id;type:uuid;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (UnreadOverride) TableName() string { return "unread_overrides" }

// MarkPostRead advances userID's read_marks watermark for postID to now and
// clears every unread_overrides pin scoped to that post (the post itself,
// plus every comment under it) — see doc.go's read-model semantics note:
// "mark read" on a whole post always wins over any manual "keep unread"
// pins inside it. Both writes happen in one transaction so a crash between
// them can't leave the watermark advanced but a stale override still
// pinning something unread (or vice versa).
func (s *Store) MarkPostRead(ctx context.Context, userID, postID string) error {
	now := time.Now().UTC()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "post_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_read_at"}),
		}).Create(&ReadMark{UserID: userID, PostID: postID, LastReadAt: now}).Error; err != nil {
			return err
		}
		return tx.
			Where("user_id = ?", userID).
			Where(`(target_type = 'post' AND target_id = ?) OR
			        (target_type = 'comment' AND target_id IN (SELECT id FROM comments WHERE post_id = ?))`,
				postID, postID).
			Delete(&UnreadOverride{}).Error
	})
}

// ClearCommentOverride clears userID's unread_overrides pin (if any) on
// commentID specifically, without touching the post's watermark or any
// other comment's override — see doc.go's read-model semantics note: a
// mark-read on a single comment only ever un-pins that one comment; it
// never implies the rest of the post (or thread) was read too. A no-op
// (nothing was pinned) is success, not an error — mark-read is idempotent.
func (s *Store) ClearCommentOverride(ctx context.Context, userID, commentID string) error {
	return s.DB.WithContext(ctx).
		Where("user_id = ? AND target_type = 'comment' AND target_id = ?", userID, commentID).
		Delete(&UnreadOverride{}).Error
}

// MarkUnread pins (targetType, targetID) unread for userID regardless of
// the post's watermark, until it's explicitly cleared again (MarkPostRead
// or ClearCommentOverride). Idempotent: pinning something already pinned is
// a no-op, not an error.
func (s *Store) MarkUnread(ctx context.Context, userID, targetType, targetID string) error {
	return s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&UnreadOverride{UserID: userID, TargetType: targetType, TargetID: targetID}).Error
}

// BoardUnread is one row of the unread-summary aggregation: a board's total
// unread item count (posts + comments, not just posts) plus the specific
// posts that carry at least one unread item, for csilservices to shape into
// csil.UnreadSummary/csil.BoardUnread. Boards with no unread rows simply
// don't appear in the result set (nothing to GROUP BY), matching
// UnreadSummary's "omitted rather than zero" contract.
type BoardUnread struct {
	BoardID     string         `gorm:"column:board_id"`
	UnreadCount int64          `gorm:"column:unread_count"`
	PostIDs     pq.StringArray `gorm:"column:post_ids;type:text[]"`
}

// unreadSummaryQuery is the ONE query behind ReadService.unread-summary (the
// poll endpoint) — see the doc comment on UnreadSummary below for the full
// plan and index-usage rationale; keep this comment and that one in sync if
// the query changes.
//
// Shape, in order:
//
//  1. my_subs: the caller's own subscription rows (small: a user's
//     subscription count, not the whole table) — every CTE below is scoped
//     off this, using the UNIQUE(user_id, target_type, target_id) index's
//     leading user_id column.
//  2. muted_posts / muted_comment_subtrees: the caller's mute pins — a
//     muted post-subscription carves that post out of a broader BOARD
//     subscription's coverage; a muted comment-subscription carves that
//     comment's whole ltree subtree out of coverage regardless of how the
//     containing post entered scope (board, post, or an unrelated comment
//     subscription) — "mute carves a hole out of a broader subscription"
//     (PLANDOC.md §4).
//  3. scoped_posts: every post the caller's subscriptions put in play at
//     all (union of direct post subs, posts reached via a comment sub, and
//     posts reached via a board sub minus muted_posts).
//  4. unread_comments: comments in scoped_posts, not authored by the
//     caller, created after the post's read_marks watermark (or any
//     comment at all if there's no watermark — "never viewed" means the
//     whole post is unread), and not inside a muted comment subtree.
//  5. unread_roots: a post's own root content counts as exactly one unread
//     item, but only if the caller has never viewed it at all (no
//     read_marks row) — the first view is what creates the watermark
//     (ThreadService.get-thread, task B4) or an explicit mark-read
//     (ReadService.mark-read) does.
//  6. override_posts / override_comments: unread_overrides pins inside
//     scoped_posts count regardless of authorship or watermark — an
//     explicit "keep unread" pin is the caller's own action, not a
//     new-activity notification, so the own-content exclusion doesn't apply
//     to it.
//  7. all_unread: a plain UNION (not UNION ALL) of 4-6 by (item_id,
//     post_id, board_id) — the same comment can appear in both
//     unread_comments and override_comments (e.g. a new reply someone also
//     explicitly pinned); UNION's implicit dedup collapses that to one row
//     rather than double-counting it.
//  8. Final GROUP BY board_id: unread_count = count of distinct unread
//     items in that board; post_ids = the distinct posts carrying at least
//     one of them.
const unreadSummaryQuery = `
WITH my_subs AS (
    SELECT target_type, target_id, muted
    FROM subscriptions
    WHERE user_id = @user_id
),
muted_posts AS (
    SELECT target_id AS post_id
    FROM my_subs
    WHERE target_type = 'post' AND muted
),
muted_comment_subtrees AS (
    SELECT c.path AS path
    FROM my_subs s
    JOIN comments c ON c.id = s.target_id
    WHERE s.target_type = 'comment' AND s.muted
),
scoped_posts AS (
    SELECT target_id AS post_id
    FROM my_subs
    WHERE target_type = 'post' AND NOT muted

    UNION

    SELECT c.post_id
    FROM my_subs s
    JOIN comments c ON c.id = s.target_id
    WHERE s.target_type = 'comment' AND NOT s.muted

    UNION

    SELECT p.id AS post_id
    FROM my_subs s
    JOIN posts p ON p.board_id = s.target_id
    WHERE s.target_type = 'board' AND NOT s.muted
      AND p.id NOT IN (SELECT post_id FROM muted_posts)
),
unread_comments AS (
    SELECT c.id AS item_id, c.post_id, p.board_id
    FROM comments c
    JOIN posts p ON p.id = c.post_id
    JOIN scoped_posts sp ON sp.post_id = c.post_id
    LEFT JOIN read_marks rm ON rm.user_id = @user_id AND rm.post_id = c.post_id
    WHERE c.deleted_at IS NULL
      AND c.author_id <> @user_id
      AND (rm.last_read_at IS NULL OR c.created_at > rm.last_read_at)
      AND NOT EXISTS (
          SELECT 1 FROM muted_comment_subtrees mc WHERE c.path <@ mc.path
      )
),
unread_roots AS (
    SELECT p.id AS item_id, p.id AS post_id, p.board_id
    FROM posts p
    JOIN scoped_posts sp ON sp.post_id = p.id
    LEFT JOIN read_marks rm ON rm.user_id = @user_id AND rm.post_id = p.id
    WHERE p.deleted_at IS NULL
      AND p.author_id <> @user_id
      AND rm.user_id IS NULL
),
override_posts AS (
    SELECT p.id AS item_id, p.id AS post_id, p.board_id
    FROM unread_overrides o
    JOIN posts p ON p.id = o.target_id
    JOIN scoped_posts sp ON sp.post_id = p.id
    WHERE o.user_id = @user_id AND o.target_type = 'post' AND p.deleted_at IS NULL
),
override_comments AS (
    SELECT c.id AS item_id, c.post_id, p.board_id
    FROM unread_overrides o
    JOIN comments c ON c.id = o.target_id
    JOIN posts p ON p.id = c.post_id
    JOIN scoped_posts sp ON sp.post_id = c.post_id
    WHERE o.user_id = @user_id AND o.target_type = 'comment' AND c.deleted_at IS NULL
),
all_unread AS (
    SELECT item_id, post_id, board_id FROM unread_comments
    UNION
    SELECT item_id, post_id, board_id FROM unread_roots
    UNION
    SELECT item_id, post_id, board_id FROM override_posts
    UNION
    SELECT item_id, post_id, board_id FROM override_comments
)
SELECT board_id,
       count(*)                          AS unread_count,
       array_agg(DISTINCT post_id::text) AS post_ids
FROM all_unread
GROUP BY board_id
`

// UnreadSummary runs the query documented above for userID and returns one
// row per board with at least one unread item. See PLANDOC.md §7 B6: this
// is the single aggregation query behind the unread-summary poll endpoint.
func (s *Store) UnreadSummary(ctx context.Context, userID string) ([]BoardUnread, error) {
	var rows []BoardUnread
	err := s.DB.WithContext(ctx).
		Raw(unreadSummaryQuery, sql.Named("user_id", userID)).
		Scan(&rows).Error
	return rows, err
}
