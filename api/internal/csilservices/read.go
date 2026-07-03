package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// readService is the stub ReadService implementation (task B1). B6 replaces
// every method body below; see the package doc comment (doc.go) for the
// error-handling contract every replacement must follow.
type readService struct {
	store *store.Store
}

// NewReadService constructs the ReadService implementation.
func NewReadService(st *store.Store) csil.ReadService {
	return &readService{store: st}
}

func (s *readService) MarkRead(ctx context.Context, req csil.TargetRef) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("ReadService.mark-read")
}

func (s *readService) MarkUnread(ctx context.Context, req csil.TargetRef) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("ReadService.mark-unread")
}

func (s *readService) UnreadSummary(ctx context.Context, req csil.Empty) (csil.UnreadSummary, error) {
	return csil.UnreadSummary{}, Unimplemented("ReadService.unread-summary")
}
