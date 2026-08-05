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

// testServices constructs real services with dependencies that are safe for
// dispatcher tests. These tests do not execute database operations.
func testServices() Services {
	var st *store.Store
	return Services{
		Auth:         csilservices.NewAuthService(st, config.Config{}),
		Board:        csilservices.NewBoardService(st),
		Category:     csilservices.NewCategoryService(st),
		Thread:       csilservices.NewThreadService(st, notify.Noop{}),
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
// *AppError should return a typed ServiceError reply, not a transport error.
// A zero-value config leaves auth/begin-login without a configured RP, which
// provides a stable CodeInternal result.
func TestDispatchFallibleOpReturnsServiceError(t *testing.T) {
	routes := buildRoutes(testServices())
	req := &transport.RpcRequest{
		Service: "auth",
		Op:      "begin-login",
		Payload: csil.EncodeBeginLoginRequest(csil.BeginLoginRequest{Domain: "example.com"}),
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

// failingNotificationService embeds the real NotificationService interface
// but forces MarkAllRead (an op with no declared error arm) to return a
// plain error, giving TestDispatchInfallibleOpReturnsTransportError a
// stable test subject.
type failingNotificationService struct {
	csil.NotificationService
}

func (failingNotificationService) MarkAllRead(context.Context, csil.Empty) (csil.Empty, error) {
	return csil.Empty{}, context.DeadlineExceeded // any non-*AppError error
}

// TestDispatchInfallibleOpReturnsTransportError exercises the
// routeInfallible path: an op with NO declared error arm whose
// implementation returns an error has no typed channel to carry it, so it
// must surface as a transport-level internal failure.
func TestDispatchInfallibleOpReturnsTransportError(t *testing.T) {
	svcs := testServices()
	svcs.Notification = failingNotificationService{svcs.Notification}
	routes := buildRoutes(svcs)
	req := &transport.RpcRequest{
		Service: "notification",
		Op:      "mark-all-read",
		Payload: csil.EncodeEmpty(csil.Empty{}),
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
	routes := buildRoutes(testServices())

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
	routes := buildRoutes(testServices())
	req := &transport.RpcRequest{Service: "auth", Op: "begin-login", Payload: []byte{0xff, 0xff, 0xff}}

	outcome := dispatch(context.Background(), routes, req)
	if outcome.IsReply || outcome.Status != transport.StatusMalformedEnvelope {
		t.Fatalf("got %+v, want transport status %v", outcome, transport.StatusMalformedEnvelope)
	}
}
