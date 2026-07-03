// BoardService (task B3, PLANDOC.md §5 §7): per-project/topic discussion
// boards, membership roles, and admin-only creation/archival.
//
// # Authz matrix
//
//	Op                   Anonymous  Member  Moderator  Maintainer  Admin
//	list-boards               yes     yes      yes         yes      yes   (excludes archived)
//	get-board                 yes     yes      yes         yes      yes   (archived readable too)
//	create-board                -       -        -           -      yes
//	update-board                 -       -        -         own      yes
//	archive-board                -       -        -           -      yes
//	set-board-member              -       -        -           -      yes
//	remove-board-member           -       -        -         own      yes
//
// "own" = the maintainer's role must be on the specific board_id the
// request targets, not maintainer-of-any-board.
//
// A few deliberate calls, since PLANDOC.md §5's inline comments
// ("update-board ;; admin", "remove-board-member ;; maintainer/mod") and
// csil/types/boards.csil's doc comment ("both may edit board metadata /
// manage the other role") read slightly more permissively than this task's
// own SCOPE description ("update-board (admin or board maintainer)",
// "maintainers may manage moderators on their own board"). This
// implementation follows the narrower, more conservative reading
// throughout:
//
//   - update-board: admin or the board's maintainer. NOT moderators, even
//     though boards.csil's doc comment says "both may edit board
//     metadata" — moderator is a delegated *content*-moderation role here,
//     not a co-owner of board metadata.
//   - set-board-member: admin only, full stop. Maintainers cannot
//     self-service promote/demote other users via this op — every role
//     grant is admin-reviewed.
//   - remove-board-member: admin, or the board's maintainer removing
//     *any* board_members row on their own board (including another
//     maintainer — this matches boards.csil's "only maintainer may remove
//     another maintainer"). Moderators have no remove authority at all,
//     matching this task's "maintainers may manage moderators" wording
//     literally (not "maintainers and moderators").
//
// If a later task needs the more permissive moderator-manages-moderator
// reading, that's a one-line change to canManageBoardMetadata/
// RemoveBoardMember below — flagged as a deviation in this task's report
// rather than silently guessed at.
package csilservices

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

const (
	boardListDefaultLimit = 50
	boardListMaxLimit     = 200

	boardSlugMinLen = 3
	boardSlugMaxLen = 50

	boardTitleMaxLen       = 256
	boardDescriptionMaxLen = 2000
)

// slugPattern matches the same shape csil/types/boards.csil's wire-level
// validation enforces (lowercase alphanumeric segments joined by single
// hyphens — no leading/trailing/doubled hyphens), just with this task's
// tighter length bound (3-50, not the wire type's 1-64) layered on top in
// validateSlug.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// boardService is the BoardService implementation (task B3).
type boardService struct {
	store *store.Store
}

// NewBoardService constructs the BoardService implementation.
func NewBoardService(st *store.Store) csil.BoardService {
	return &boardService{store: st}
}

func (s *boardService) ListBoards(ctx context.Context, req csil.ListBoardsRequest) (csil.BoardPage, error) {
	limit := uint64(boardListDefaultLimit)
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}
	if limit > boardListMaxLimit {
		limit = boardListMaxLimit
	}

	afterTitle, afterID, err := decodeBoardCursor(req.Cursor)
	if err != nil {
		// A malformed cursor isn't "genuinely unexpected" in the sense
		// api/internal/csilservices/doc.go reserves real errors for on an
		// op with no declared ServiceError arm (list-boards has none) —
		// there's no typed channel to explain the problem to the caller
		// anyway. Degrade to "start of list" rather than failing the
		// request over a stale/corrupt cursor.
		afterTitle, afterID = "", ""
	}

	// Fetch one extra row so "is there a next page" doesn't need a
	// separate count query.
	boards, err := s.store.ListActiveBoards(ctx, afterTitle, afterID, int(limit)+1)
	if err != nil {
		return csil.BoardPage{}, err
	}

	hasMore := uint64(len(boards)) > limit
	if hasMore {
		boards = boards[:limit]
	}

	page := csil.BoardPage{Boards: make([]csil.Board, 0, len(boards))}
	for _, b := range boards {
		page.Boards = append(page.Boards, boardToWire(b))
	}
	if hasMore && len(boards) > 0 {
		last := boards[len(boards)-1]
		cursor := encodeBoardCursor(last.Title, last.ID)
		page.NextCursor = &cursor
	}
	return page, nil
}

func (s *boardService) GetBoard(ctx context.Context, req csil.BoardSlug) (csil.Board, error) {
	slug := strings.TrimSpace(string(req))
	if slug == "" {
		return csil.Board{}, Validation("slug", "slug is required")
	}
	// Anyone (including anonymous) may look up a board by slug, and
	// archived boards are still browsable (csil/types/boards.csil), so
	// there's no authz check here beyond "does it exist."
	b, err := s.store.GetBoardBySlug(ctx, slug)
	if err != nil {
		if store.IsNotFound(err) {
			return csil.Board{}, NotFound("board", "board not found")
		}
		return csil.Board{}, Internal("looking up board: " + err.Error())
	}
	return boardToWire(*b), nil
}

func (s *boardService) CreateBoard(ctx context.Context, req csil.CreateBoardRequest) (csil.Board, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Board{}, Unauthenticated("login required")
	}
	if !IsInstanceAdmin(user) {
		return csil.Board{}, Forbidden("only an instance admin may create a board")
	}

	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if appErr := validateSlug(slug); appErr != nil {
		return csil.Board{}, appErr
	}
	title := strings.TrimSpace(req.Title)
	if appErr := validateTitle(title); appErr != nil {
		return csil.Board{}, appErr
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
		if appErr := validateDescription(description); appErr != nil {
			return csil.Board{}, appErr
		}
	}
	if appErr := validateBoardKind(req.Kind); appErr != nil {
		return csil.Board{}, appErr
	}

	exists, err := s.store.BoardSlugExists(ctx, slug)
	if err != nil {
		return csil.Board{}, Internal("checking slug availability: " + err.Error())
	}
	if exists {
		return csil.Board{}, Conflict("a board with this slug already exists")
	}

	b := &store.Board{
		Slug:        slug,
		Title:       title,
		Description: description,
		Kind:        string(req.Kind),
		CreatedBy:   user.ID,
	}
	if err := s.store.CreateBoard(ctx, b); err != nil {
		if store.IsUniqueViolation(err) {
			// The BoardSlugExists pre-check above raced with a concurrent
			// create-board for the same slug.
			return csil.Board{}, Conflict("a board with this slug already exists")
		}
		return csil.Board{}, Internal("creating board: " + err.Error())
	}
	return boardToWire(*b), nil
}

func (s *boardService) UpdateBoard(ctx context.Context, req csil.UpdateBoardRequest) (csil.Board, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Board{}, Unauthenticated("login required")
	}

	board, err := s.store.GetBoardByID(ctx, string(req.Id))
	if err != nil {
		if store.IsNotFound(err) {
			return csil.Board{}, NotFound("board", "board not found")
		}
		return csil.Board{}, Internal("looking up board: " + err.Error())
	}

	allowed, err := canManageBoardMetadata(ctx, s.store, user, board.ID)
	if err != nil {
		return csil.Board{}, Internal("checking board authorization: " + err.Error())
	}
	if !allowed {
		return csil.Board{}, Forbidden("only an instance admin or this board's maintainer may update it")
	}

	updates := map[string]any{}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if appErr := validateTitle(title); appErr != nil {
			return csil.Board{}, appErr
		}
		updates["title"] = title
	}
	if req.Description != nil {
		if appErr := validateDescription(*req.Description); appErr != nil {
			return csil.Board{}, appErr
		}
		updates["description"] = *req.Description
	}
	if len(updates) == 0 {
		return boardToWire(*board), nil
	}

	updated, err := s.store.UpdateBoardFields(ctx, board.ID, updates)
	if err != nil {
		return csil.Board{}, Internal("updating board: " + err.Error())
	}
	return boardToWire(*updated), nil
}

func (s *boardService) ArchiveBoard(ctx context.Context, req csil.BoardID) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("login required")
	}
	if !IsInstanceAdmin(user) {
		return csil.Empty{}, Forbidden("only an instance admin may archive a board")
	}

	board, err := s.store.GetBoardByID(ctx, string(req))
	if err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("board", "board not found")
		}
		return csil.Empty{}, Internal("looking up board: " + err.Error())
	}

	rows, err := s.store.ArchiveBoard(ctx, board.ID)
	if err != nil {
		return csil.Empty{}, Internal("archiving board: " + err.Error())
	}
	if rows == 0 {
		return csil.Empty{}, Conflict("board is already archived")
	}
	return csil.Empty{}, nil
}

func (s *boardService) SetBoardMember(ctx context.Context, req csil.SetBoardMemberRequest) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("login required")
	}
	if !IsInstanceAdmin(user) {
		return csil.Empty{}, Forbidden("only an instance admin may assign board roles")
	}

	board, err := s.store.GetBoardByID(ctx, string(req.BoardId))
	if err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("board", "board not found")
		}
		return csil.Empty{}, Internal("looking up board: " + err.Error())
	}

	role, appErr := normalizeBoardRole(req.Role)
	if appErr != nil {
		return csil.Empty{}, appErr
	}

	targetUserID := strings.TrimSpace(string(req.UserId))
	if targetUserID == "" {
		return csil.Empty{}, Validation("user_id", "user_id is required")
	}
	if _, err := s.store.GetUser(ctx, targetUserID); err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("user", "user not found")
		}
		return csil.Empty{}, Internal("looking up user: " + err.Error())
	}

	if err := s.store.SetBoardMember(ctx, board.ID, targetUserID, role); err != nil {
		return csil.Empty{}, Internal("assigning board role: " + err.Error())
	}
	return csil.Empty{}, nil
}

func (s *boardService) RemoveBoardMember(ctx context.Context, req csil.RemoveBoardMemberRequest) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("login required")
	}

	board, err := s.store.GetBoardByID(ctx, string(req.BoardId))
	if err != nil {
		if store.IsNotFound(err) {
			return csil.Empty{}, NotFound("board", "board not found")
		}
		return csil.Empty{}, Internal("looking up board: " + err.Error())
	}

	if !IsInstanceAdmin(user) {
		isMaintainer, err := IsBoardMaintainer(ctx, s.store, user.ID, board.ID)
		if err != nil {
			return csil.Empty{}, Internal("checking board authorization: " + err.Error())
		}
		if !isMaintainer {
			return csil.Empty{}, Forbidden("only an instance admin or this board's maintainer may remove a board member")
		}
	}

	targetUserID := strings.TrimSpace(string(req.UserId))
	if targetUserID == "" {
		return csil.Empty{}, Validation("user_id", "user_id is required")
	}

	rows, err := s.store.RemoveBoardMember(ctx, board.ID, targetUserID)
	if err != nil {
		return csil.Empty{}, Internal("removing board member: " + err.Error())
	}
	if rows == 0 {
		return csil.Empty{}, NotFound("board_member", "that user has no role on this board")
	}
	return csil.Empty{}, nil
}

// canManageBoardMetadata implements update-board's authz rule: an instance
// admin, or the specific board's maintainer (not moderator — see this
// file's package doc comment for why).
func canManageBoardMetadata(ctx context.Context, st *store.Store, user *store.User, boardID string) (bool, error) {
	if IsInstanceAdmin(user) {
		return true, nil
	}
	return IsBoardMaintainer(ctx, st, user.ID, boardID)
}

// boardToWire maps a store.Board row to its csil.Board wire representation.
func boardToWire(b store.Board) csil.Board {
	wire := csil.Board{
		Id:        csil.BoardID(b.ID),
		Slug:      b.Slug,
		Title:     b.Title,
		Kind:      csil.BoardKind(b.Kind),
		CreatedBy: csil.UserID(b.CreatedBy),
		CreatedAt: b.CreatedAt,
	}
	if b.Description != "" {
		d := b.Description
		wire.Description = &d
	}
	if b.ArchivedAt != nil {
		archivedAt := *b.ArchivedAt
		wire.ArchivedAt = &archivedAt
	}
	return wire
}

// validateSlug enforces this task's slug rule: lowercase, [a-z0-9-],
// 3-50 characters, no leading/trailing/doubled hyphens. This is
// deliberately tighter than csil/types/boards.csil's wire-level validation
// (1-64 characters, same character-class regex) — 3-50 is a strict subset
// of 1-64, so nothing here can accept something the wire type would
// reject.
func validateSlug(slug string) *AppError {
	if len(slug) < boardSlugMinLen || len(slug) > boardSlugMaxLen {
		return Validation("slug", "slug must be 3-50 characters")
	}
	if !slugPattern.MatchString(slug) {
		return Validation("slug", `slug must be lowercase alphanumeric segments separated by single hyphens (e.g. "my-board")`)
	}
	return nil
}

func validateTitle(title string) *AppError {
	if title == "" {
		return Validation("title", "title is required")
	}
	if len(title) > boardTitleMaxLen {
		return Validation("title", "title must be at most 256 characters")
	}
	return nil
}

func validateDescription(description string) *AppError {
	if len(description) > boardDescriptionMaxLen {
		return Validation("description", "description must be at most 2000 characters")
	}
	return nil
}

func validateBoardKind(kind csil.BoardKind) *AppError {
	switch string(kind) {
	case "discussion", "announce":
		return nil
	default:
		return Validation("kind", `kind must be "discussion" or "announce"`)
	}
}

// normalizeBoardRole validates req.Role against the two roles
// board_member_role allows, returning the plain string store.SetBoardMember
// expects.
func normalizeBoardRole(role csil.BoardRole) (string, *AppError) {
	switch string(role) {
	case store.BoardRoleMaintainer, store.BoardRoleModerator:
		return string(role), nil
	default:
		return "", Validation("role", `role must be "maintainer" or "moderator"`)
	}
}

// --- list-boards cursor ---
//
// PageCursor is documented (csil/types/common.csil) as opaque text the
// server mints and the client must not parse. We encode a keyset pagination
// position — (title, id) of the last row on the current page, matching
// ListActiveBoards' ORDER BY — as base64 so the wire value carries no
// structure a client could accidentally depend on.

const boardCursorSep = "\x1f" // ASCII unit separator: never legal in a title/id.

func encodeBoardCursor(title, id string) csil.PageCursor {
	raw := title + boardCursorSep + id
	return csil.PageCursor(base64.StdEncoding.EncodeToString([]byte(raw)))
}

// decodeBoardCursor returns ("", "", nil) for a nil/empty cursor (first
// page) and a non-nil error only if c is non-empty but malformed — callers
// on an infallible op (list-boards has no declared ServiceError arm) should
// treat that as "start of list" rather than propagating the error; see
// ListBoards.
func decodeBoardCursor(c *csil.PageCursor) (title, id string, err error) {
	if c == nil || *c == "" {
		return "", "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(string(*c))
	if err != nil {
		return "", "", err
	}
	title, id, found := strings.Cut(string(raw), boardCursorSep)
	if !found {
		return "", "", errMalformedCursor
	}
	return title, id, nil
}

var errMalformedCursor = errors.New("malformed board cursor")
