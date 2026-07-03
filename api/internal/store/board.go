package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm/clause"
)

// Board mirrors the `boards` table.
type Board struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	Slug        string     `gorm:"column:slug;not null"`
	Title       string     `gorm:"column:title;not null"`
	Description string     `gorm:"column:description;not null;default:''"`
	Kind        string     `gorm:"column:kind;type:board_kind;not null;default:'discussion'"`
	CreatedBy   string     `gorm:"column:created_by;type:uuid;not null"`
	ArchivedAt  *time.Time `gorm:"column:archived_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
}

func (Board) TableName() string { return "boards" }

// Board role values stored in board_members.role (the board_member_role
// Postgres enum). csilservices/authz.go's BoardRole helper returns these
// same strings so callers never have to duplicate the literals.
const (
	BoardRoleMaintainer = "maintainer"
	BoardRoleModerator  = "moderator"
)

// BoardMember mirrors the `board_members` table: per-board maintainer /
// moderator role assignment.
type BoardMember struct {
	BoardID   string    `gorm:"column:board_id;type:uuid;primaryKey"`
	UserID    string    `gorm:"column:user_id;type:uuid;primaryKey"`
	Role      string    `gorm:"column:role;type:board_member_role;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (BoardMember) TableName() string { return "board_members" }

// ListActiveBoards returns up to limit non-archived boards ordered by
// (title, id) ascending — the ordering BoardPage's doc comment
// (csil/types/boards.csil) promises ("boards are ordered by title", id as
// tie-breaker for a stable keyset cursor when titles collide). afterTitle/
// afterID implement keyset pagination: pass both empty for the first page,
// otherwise the (title, id) of the last row on the previous page. Callers
// typically request limit+1 rows to detect "is there another page" without
// a separate count query (see csilservices.boardService.ListBoards).
func (s *Store) ListActiveBoards(ctx context.Context, afterTitle, afterID string, limit int) ([]Board, error) {
	q := s.DB.WithContext(ctx).
		Where("archived_at IS NULL").
		Order("title ASC, id ASC").
		Limit(limit)
	if afterTitle != "" || afterID != "" {
		// Postgres row-value comparison: strictly-greater-than on the
		// (title, id) tuple, matching the ORDER BY above.
		q = q.Where("(title, id) > (?, ?)", afterTitle, afterID)
	}
	var boards []Board
	if err := q.Find(&boards).Error; err != nil {
		return nil, err
	}
	return boards, nil
}

// GetBoardBySlug returns the board with the given slug, or
// gorm.ErrRecordNotFound (see IsNotFound) if there is none. Archived boards
// are still returned — per boards.csil, an archived board stays browsable,
// just read-only.
func (s *Store) GetBoardBySlug(ctx context.Context, slug string) (*Board, error) {
	var b Board
	if err := s.DB.WithContext(ctx).First(&b, "slug = ?", slug).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// GetBoardByID returns the board with the given id, or
// gorm.ErrRecordNotFound (see IsNotFound) if there is none.
func (s *Store) GetBoardByID(ctx context.Context, id string) (*Board, error) {
	var b Board
	if err := s.DB.WithContext(ctx).First(&b, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// BoardSlugExists reports whether a board with the given slug already
// exists (used by create-board for a friendly pre-check before hitting the
// database's own UNIQUE constraint — see IsUniqueViolation for the backstop
// against the create/check race).
func (s *Store) BoardSlugExists(ctx context.Context, slug string) (bool, error) {
	var count int64
	if err := s.DB.WithContext(ctx).Model(&Board{}).Where("slug = ?", slug).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateBoard inserts b, populating its generated fields (ID, CreatedAt) on
// success. Returns a Postgres unique-violation error (see IsUniqueViolation)
// if b.Slug collides with an existing board — the caller is expected to
// have already done a BoardSlugExists pre-check, so this only fires on a
// genuine create/create race.
func (s *Store) CreateBoard(ctx context.Context, b *Board) error {
	return s.DB.WithContext(ctx).Create(b).Error
}

// UpdateBoardFields applies updates (column name -> new value) to the board
// identified by id and returns the row as it reads after the update.
// Returns gorm.ErrRecordNotFound if id doesn't match a board.
func (s *Store) UpdateBoardFields(ctx context.Context, id string, updates map[string]any) (*Board, error) {
	if err := s.DB.WithContext(ctx).Model(&Board{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetBoardByID(ctx, id)
}

// ArchiveBoard sets archived_at = now() on the board identified by id, but
// only if it isn't already archived. Returns the number of rows affected
// (0 or 1) so the caller can distinguish "already archived" (0 rows, board
// exists per an earlier lookup) from a genuine change — see
// csilservices.boardService.ArchiveBoard, which reports the former as a
// Conflict rather than silently succeeding.
func (s *Store) ArchiveBoard(ctx context.Context, id string) (int64, error) {
	tx := s.DB.WithContext(ctx).Model(&Board{}).
		Where("id = ? AND archived_at IS NULL", id).
		Update("archived_at", time.Now().UTC())
	return tx.RowsAffected, tx.Error
}

// SetBoardMember assigns (or, if a row already exists for board_id+user_id,
// replaces) role for userID on boardID — an upsert on the
// board_members(board_id, user_id) primary key, matching
// SetBoardMemberRequest's documented idempotent-assignment semantics
// (csil/types/boards.csil).
func (s *Store) SetBoardMember(ctx context.Context, boardID, userID, role string) error {
	bm := &BoardMember{BoardID: boardID, UserID: userID, Role: role}
	return s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "board_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"role"}),
		}).
		Create(bm).Error
}

// GetBoardMemberRole returns the role userID holds on boardID, or
// gorm.ErrRecordNotFound (see IsNotFound) if the user has no board_members
// row there at all — a normal "not a maintainer/moderator" state, not a
// fault. csilservices.BoardRole is the authz-layer wrapper callers outside
// this package should use, which turns that not-found into ("", nil).
func (s *Store) GetBoardMemberRole(ctx context.Context, boardID, userID string) (string, error) {
	var bm BoardMember
	if err := s.DB.WithContext(ctx).First(&bm, "board_id = ? AND user_id = ?", boardID, userID).Error; err != nil {
		return "", err
	}
	return bm.Role, nil
}

// RemoveBoardMember deletes userID's board_members row on boardID entirely
// (they remain a regular member with no special privileges — boards.csil).
// Returns the number of rows deleted (0 or 1) so the caller can tell
// "nothing to remove" apart from a genuine removal.
func (s *Store) RemoveBoardMember(ctx context.Context, boardID, userID string) (int64, error) {
	tx := s.DB.WithContext(ctx).
		Where("board_id = ? AND user_id = ?", boardID, userID).
		Delete(&BoardMember{})
	return tx.RowsAffected, tx.Error
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) — e.g. a racing create-board hitting boards'
// slug UNIQUE index after CreateBoard's caller already did a
// BoardSlugExists pre-check that passed. General enough that other stores
// will likely want it too; it stays here for now so this task only touches
// board.go, per this repo's Wave-B file-ownership split.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// TrustedDomain mirrors the `trusted_domains` table: instance-level,
// admin-managed linkkeys domains whose endorsements earn reputation.
type TrustedDomain struct {
	Domain    string    `gorm:"column:domain;primaryKey"`
	AddedBy   string    `gorm:"column:added_by;type:uuid;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (TrustedDomain) TableName() string { return "trusted_domains" }
