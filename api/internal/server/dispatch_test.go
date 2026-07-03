package server

import (
	"context"
	"testing"

	"github.com/catalystcommunity/firepit/api/internal/config"
	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/api/internal/transport"
)

// stubServices builds a Services value backed entirely by the
// csilservices.NewXService stubs, exactly as main.go does today. The stub
// constructors never touch *store.Store, so a nil store is safe here.
// AuthService (B2) and EndorsementService (B5) are real rather than stubs,
// but both are likewise safe to construct here: these tests exercise
// dispatch mechanics only and never invoke a method that dereferences the
// nil store — a zero-value config.Config leaves AuthService unable to
// reach any RP (see TestDispatchFallibleOpReturnsServiceError), and
// notify.Noop{} mirrors main.go's own wiring.
func stubServices() Services {
	var st *store.Store
	return Services{
		Auth:         csilservices.NewAuthService(st, config.Config{}),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st),
		Endorsement:  csilservices.NewEndorsementService(st, notify.Noop{}),
		Settings:     csilservices.NewSettingsService(st),
		Social:       csilservices.NewSocialService(st),
		Subscription: csilservices.NewSubscriptionService(st),
		Read:         csilservices.NewReadService(st),
		Notification: csilservices.NewNotificationService(st),
		Integration:  csilservices.NewIntegrationService(st),
	}
}

// TestDispatchFallibleOpReturnsServiceError exercises the routeFallible path:
// an op with a declared `/ ServiceError` arm whose implementation returns an
// *AppError should come back as a typed ServiceError reply (transport
// status ok), not a transport-level failure. auth/begin-login is real (task
// B2), not a stub; stubServices()'s zero-value config.Config leaves it with
// no configured linkkeys RP, so it still returns a (different) *AppError —
// CodeInternal rather than CodeUnimplemented — which is exactly the
// dispatch-mechanics behavior this test cares about.
func TestDispatchFallibleOpReturnsServiceError(t *testing.T) {
	routes := buildRoutes(stubServices())
	req := &transport.RpcRequest{
		Service: "auth",
		Op:      "begin-login",
		Payload: csil.EncodeAuthBeginLoginRequest(csil.BeginLoginRequest{Domain: "example.com"}),
	}

	outcome := dispatch(context.Background(), routes, req)
	if !outcome.IsReply {
		t.Fatalf("expected a typed reply, got transport status %v (%s)", outcome.Status, outcome.Message)
	}
	if outcome.Variant != "ServiceError" {
		t.Fatalf("expected variant ServiceError, got %q", outcome.Variant)
	}
	svcErr, err := csil.DecodeServiceError(outcome.Payload)
	if err != nil {
		t.Fatalf("decode ServiceError: %v", err)
	}
	if svcErr.Code != csilservices.CodeInternal {
		t.Errorf("code = %d, want %d", svcErr.Code, csilservices.CodeInternal)
	}
	if svcErr.Message == "" {
		t.Error("expected a non-empty message")
	}
}

// TestDispatchInfallibleOpReturnsTransportError exercises the
// routeInfallible path: an op with NO declared error arm whose stub still
// returns an error has no typed channel to carry it, so it must surface as
// a transport-level internal failure.
//
// Uses thread/list-posts (still a B1 stub as of task B3) rather than
// board/list-boards: BoardService is a real implementation now (task B3)
// and its ListBoards touches *store.Store, which stubServices() sets up as
// nil — exercising a still-unimplemented op is what this test actually
// wants to cover, so it moved to one that stays a stub until B4 lands
// rather than asserting on BoardService's stub behavior specifically.
func TestDispatchInfallibleOpReturnsTransportError(t *testing.T) {
	routes := buildRoutes(stubServices())
	req := &transport.RpcRequest{
		Service: "thread",
		Op:      "list-posts",
		Payload: csil.EncodeThreadListPostsRequest(csil.ListPostsRequest{BoardId: "board-1"}),
	}

	outcome := dispatch(context.Background(), routes, req)
	if outcome.IsReply {
		t.Fatalf("expected a transport-level failure, got a typed reply (variant %q)", outcome.Variant)
	}
	if outcome.Status != transport.StatusInternal {
		t.Errorf("status = %v, want %v", outcome.Status, transport.StatusInternal)
	}
}

// TestDispatchUnknownServiceOrOp checks the two "no route" cases.
func TestDispatchUnknownServiceOrOp(t *testing.T) {
	routes := buildRoutes(stubServices())

	t.Run("unknown service", func(t *testing.T) {
		outcome := dispatch(context.Background(), routes, &transport.RpcRequest{Service: "nope", Op: "whatever"})
		if outcome.IsReply || outcome.Status != transport.StatusUnknownServiceOrOp {
			t.Fatalf("got %+v, want transport status %v", outcome, transport.StatusUnknownServiceOrOp)
		}
	})

	t.Run("unknown op", func(t *testing.T) {
		outcome := dispatch(context.Background(), routes, &transport.RpcRequest{Service: "auth", Op: "nope"})
		if outcome.IsReply || outcome.Status != transport.StatusUnknownServiceOrOp {
			t.Fatalf("got %+v, want transport status %v", outcome, transport.StatusUnknownServiceOrOp)
		}
	})
}

// TestDispatchMalformedPayload checks that a payload the decoder can't parse
// produces StatusMalformedEnvelope rather than a panic or a typed reply.
func TestDispatchMalformedPayload(t *testing.T) {
	routes := buildRoutes(stubServices())
	req := &transport.RpcRequest{Service: "auth", Op: "begin-login", Payload: []byte{0xff, 0xff, 0xff}}

	outcome := dispatch(context.Background(), routes, req)
	if outcome.IsReply || outcome.Status != transport.StatusMalformedEnvelope {
		t.Fatalf("got %+v, want transport status %v", outcome, transport.StatusMalformedEnvelope)
	}
}
