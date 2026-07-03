package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// endorsementService is the stub EndorsementService implementation (task
// B1). B5 replaces every method body below; see the package doc comment
// (doc.go) for the error-handling contract every replacement must follow.
type endorsementService struct {
	store *store.Store
}

// NewEndorsementService constructs the EndorsementService implementation.
func NewEndorsementService(st *store.Store) csil.EndorsementService {
	return &endorsementService{store: st}
}

func (s *endorsementService) Endorse(ctx context.Context, req csil.EndorseRequest) (csil.Endorsement, error) {
	return csil.Endorsement{}, Unimplemented("EndorsementService.endorse")
}

func (s *endorsementService) Retract(ctx context.Context, req csil.EndorseRequest) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("EndorsementService.retract")
}

func (s *endorsementService) ListEndorsements(ctx context.Context, req csil.TargetRef) (csil.EndorsementList, error) {
	return csil.EndorsementList{}, Unimplemented("EndorsementService.list-endorsements")
}
