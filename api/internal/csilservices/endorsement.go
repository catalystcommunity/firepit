package csilservices

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// endorsementService is the real EndorsementService implementation
// (task B5). See the package doc comment (doc.go) for the *AppError
// contract every method here follows, and PLANDOC.md §4's "Endorser
// ordering" design note for the per-viewer ordering list-endorsements
// implements (via store.ListEndorsementsForViewer).
type endorsementService struct {
	store     *store.Store
	publisher notify.Publisher
}

// NewEndorsementService constructs the EndorsementService implementation.
// publisher is called inside the same transaction as the endorsement
// write (notify.KindEndorsed), same contract as every other write path
// that emits an event — see notify's package doc comment. Pass
// notify.Noop{} until B7's fan-out lands.
func NewEndorsementService(st *store.Store, publisher notify.Publisher) csil.EndorsementService {
	return &endorsementService{store: st, publisher: publisher}
}

// validTargetType reports whether t is one of the two kinds of content
// that can be endorsed. Endorsing a board is meaningless
// (csil/types/endorsements.csil) and is rejected as a validation error.
func validTargetType(t csil.TargetType) bool {
	return string(t) == "post" || string(t) == "comment"
}

func (s *endorsementService) Endorse(ctx context.Context, req csil.EndorseRequest) (csil.Endorsement, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Endorsement{}, Unauthenticated("login required to endorse")
	}
	if !validTargetType(req.TargetType) {
		return csil.Endorsement{}, Validation("target_type", `target_type must be "post" or "comment"`)
	}
	targetType := string(req.TargetType)

	var result store.Endorsement
	var roleBadge string
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		target, err := s.store.ResolveEndorsementTarget(ctx, tx, targetType, req.TargetId)
		if err != nil {
			if store.IsNotFound(err) {
				return NotFound(targetType, "target not found")
			}
			return err
		}
		if target.DeletedAt != nil {
			return Conflict("cannot endorse deleted content")
		}
		if target.AuthorID == user.ID {
			return Forbidden("cannot endorse your own content")
		}

		endorsement, created, err := s.store.InsertEndorsement(ctx, tx, user.ID, targetType, req.TargetId)
		if err != nil {
			return err
		}
		result = *endorsement

		// Idempotent re-endorse: the row already existed, so skip the
		// reputation bump and the notification — both already happened
		// the first time this user endorsed this target.
		if created {
			trusted, err := s.store.IsTrustedDomain(ctx, tx, user.LinkkeysDomain)
			if err != nil {
				return err
			}
			if trusted {
				if err := s.store.BumpReputation(ctx, tx, target.AuthorID, 1); err != nil {
					return err
				}
			}

			if s.publisher != nil {
				if err := s.publisher.Publish(ctx, tx, notify.Event{
					Kind:       notify.KindEndorsed,
					ActorID:    user.ID,
					BoardID:    target.BoardID,
					PostID:     target.PostID,
					TargetType: targetType,
					TargetID:   req.TargetId,
				}); err != nil {
					return err
				}
			}
		}

		roleBadge, err = s.store.BoardRoleForUser(ctx, tx, target.BoardID, user.ID)
		return err
	})
	if txErr != nil {
		var appErr *AppError
		if errors.As(txErr, &appErr) {
			return csil.Endorsement{}, appErr
		}
		return csil.Endorsement{}, Internal("endorse: " + txErr.Error())
	}

	resp := csil.Endorsement{
		Id:         csil.EndorsementID(result.ID),
		UserId:     csil.UserID(result.UserID),
		TargetType: csil.TargetType(result.TargetType),
		TargetId:   result.TargetID,
		CreatedAt:  result.CreatedAt,
	}
	if roleBadge != "" {
		resp.RoleBadge = &roleBadge
	}
	// The endorser is always the caller here — no extra lookup needed, we
	// already hold their handle from reqctx.
	if user.Handle != "" {
		handle := user.Handle
		resp.AuthorHandle = &handle
	}
	return resp, nil
}

func (s *endorsementService) Retract(ctx context.Context, req csil.EndorseRequest) (csil.Empty, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.Empty{}, Unauthenticated("login required to retract an endorsement")
	}
	if !validTargetType(req.TargetType) {
		return csil.Empty{}, Validation("target_type", `target_type must be "post" or "comment"`)
	}
	targetType := string(req.TargetType)

	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := s.store.FindEndorsement(ctx, tx, user.ID, targetType, req.TargetId)
		if err != nil {
			if store.IsNotFound(err) {
				// Idempotent: nothing to retract is a no-op success, not
				// a not-found error.
				return nil
			}
			return err
		}

		if err := s.store.DeleteEndorsement(ctx, tx, existing.ID); err != nil {
			return err
		}

		trusted, err := s.store.IsTrustedDomain(ctx, tx, user.LinkkeysDomain)
		if err != nil {
			return err
		}
		if trusted {
			// The target may itself be gone by now in principle (it
			// isn't, in practice — content is only ever soft-deleted),
			// so a not-found here is tolerated rather than blocking the
			// retraction: the endorsement row still comes off either way.
			target, err := s.store.ResolveEndorsementTarget(ctx, tx, targetType, req.TargetId)
			if err != nil && !store.IsNotFound(err) {
				return err
			}
			if err == nil {
				if err := s.store.BumpReputation(ctx, tx, target.AuthorID, -1); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if txErr != nil {
		var appErr *AppError
		if errors.As(txErr, &appErr) {
			return csil.Empty{}, appErr
		}
		return csil.Empty{}, Internal("retract: " + txErr.Error())
	}
	return csil.Empty{}, nil
}

func (s *endorsementService) ListEndorsements(ctx context.Context, req csil.TargetRef) (csil.EndorsementList, error) {
	// list-endorsements has no declared ServiceError arm (csil/firepit.csil):
	// an invalid or unresolvable target reads as an empty list, never an
	// error — see the package doc comment (doc.go) on infallible ops.
	if !validTargetType(req.TargetType) {
		return csil.EndorsementList{Endorsements: []csil.Endorsement{}}, nil
	}

	var viewerID *string
	if user, ok := reqctx.User(ctx); ok {
		viewerID = &user.ID
	}

	views, err := s.store.ListEndorsementsForViewer(ctx, viewerID, string(req.TargetType), req.TargetId)
	if err != nil {
		return csil.EndorsementList{}, err
	}

	// One batched endorser lookup for the whole list (never a per-row
	// query).
	userIDs := make([]string, len(views))
	for i, v := range views {
		userIDs[i] = v.Endorsement.UserID
	}
	endorsers, err := s.store.GetUsersByIDs(ctx, userIDs)
	if err != nil {
		return csil.EndorsementList{}, err
	}

	out := make([]csil.Endorsement, len(views))
	for i, v := range views {
		e := csil.Endorsement{
			Id:         csil.EndorsementID(v.Endorsement.ID),
			UserId:     csil.UserID(v.Endorsement.UserID),
			TargetType: csil.TargetType(v.Endorsement.TargetType),
			TargetId:   v.Endorsement.TargetID,
			CreatedAt:  v.Endorsement.CreatedAt,
		}
		if v.RoleBadge != "" {
			roleBadge := v.RoleBadge
			e.RoleBadge = &roleBadge
		}
		if handle := endorsers[v.Endorsement.UserID].Handle; handle != "" {
			e.AuthorHandle = &handle
		}
		out[i] = e
	}
	return csil.EndorsementList{Endorsements: out}, nil
}
