package server

import (
	"context"
	"testing"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/api/internal/transport"
)

// stubServices builds a Services value backed entirely by the
// csilservices.NewXService stubs, exactly as main.go does today. The stub
// constructors never touch *store.Store, so a nil store is safe here.
func stubServices() Services {
	var st *store.Store
	return Services{
		Auth:         csilservices.NewAuthService(st),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st),
		Endorsement:  csilservices.NewEndorsementService(st),
		Settings:     csilservices.NewSettingsService(st),
		Social:       csilservices.NewSocialService(st),
		Subscription: csilservices.NewSubscriptionService(st),
		Read:         csilservices.NewReadService(st),
		Notification: csilservices.NewNotificationService(st),
		Integration:  csilservices.NewIntegrationService(st),
	}
}

// TestDispatchFallibleOpReturnsServiceError exercises the routeFallible path:
// an op with a declared `/ ServiceError` arm whose stub returns an
// *AppError should come back as a typed ServiceError reply (transport
// status ok), not a transport-level failure.
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
	if svcErr.Code != csilservices.CodeUnimplemented {
		t.Errorf("code = %d, want %d", svcErr.Code, csilservices.CodeUnimplemented)
	}
	if svcErr.Message == "" {
		t.Error("expected a non-empty message")
	}
}

// TestDispatchInfallibleOpReturnsTransportError exercises the
// routeInfallible path: an op with NO declared error arm whose stub still
// returns an error has no typed channel to carry it, so it must surface as
// a transport-level internal failure.
func TestDispatchInfallibleOpReturnsTransportError(t *testing.T) {
	routes := buildRoutes(stubServices())
	req := &transport.RpcRequest{
		Service: "board",
		Op:      "list-boards",
		Payload: csil.EncodeBoardListBoardsRequest(csil.ListBoardsRequest{}),
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
