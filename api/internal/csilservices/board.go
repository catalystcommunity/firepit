package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// boardService is the stub BoardService implementation (task B1). B3
// replaces every method body below; see the package doc comment (doc.go)
// for the error-handling contract every replacement must follow.
type boardService struct {
	store *store.Store
}

// NewBoardService constructs the BoardService implementation.
func NewBoardService(st *store.Store) csil.BoardService {
	return &boardService{store: st}
}

func (s *boardService) ListBoards(ctx context.Context, req csil.ListBoardsRequest) (csil.BoardPage, error) {
	// list-boards has no declared ServiceError arm; see AuthService.Logout
	// for why this stub error still becomes a (temporary) transport-level
	// failure.
	return csil.BoardPage{}, Unimplemented("BoardService.list-boards")
}

func (s *boardService) GetBoard(ctx context.Context, req csil.BoardSlug) (csil.Board, error) {
	return csil.Board{}, Unimplemented("BoardService.get-board")
}

func (s *boardService) CreateBoard(ctx context.Context, req csil.CreateBoardRequest) (csil.Board, error) {
	return csil.Board{}, Unimplemented("BoardService.create-board")
}

func (s *boardService) UpdateBoard(ctx context.Context, req csil.UpdateBoardRequest) (csil.Board, error) {
	return csil.Board{}, Unimplemented("BoardService.update-board")
}

func (s *boardService) ArchiveBoard(ctx context.Context, req csil.BoardID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("BoardService.archive-board")
}

func (s *boardService) SetBoardMember(ctx context.Context, req csil.SetBoardMemberRequest) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("BoardService.set-board-member")
}

func (s *boardService) RemoveBoardMember(ctx context.Context, req csil.RemoveBoardMemberRequest) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("BoardService.remove-board-member")
}
