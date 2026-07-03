// Package csilservices holds the hand-written implementations of every
// generated csil.<X>Service interface (api/internal/csil/services.gen.go —
// AuthService, BoardService, ThreadService, EndorsementService,
// SettingsService, SocialService, SubscriptionService, ReadService,
// NotificationService, IntegrationService).
//
// This file is the pattern every implementation across B2-B9 copies. Read it
// before writing a real op body.
//
// # Constructing a service
//
// Each service is a small struct constructed from *store.Store and nothing
// else at this layer (cross-cutting concerns like notification fan-out are
// injected separately once B7 exists, most likely as extra constructor
// arguments at that point — that's additive, not a reason to change the
// shape now):
//
//	type authService struct{ store *store.Store }
//
//	func NewAuthService(st *store.Store) csil.AuthService {
//		return &authService{store: st}
//	}
//
// api/cmd/firepit-api/main.go wires one constructor call per service into
// the server's Services struct (api/internal/server/dispatch.go). Swapping a
// stub for a real implementation is exactly: change what NewXService
// returns; nothing else in the wiring changes.
//
// # Application errors vs. transport errors
//
// Every generated service method has the shape:
//
//	Method(ctx context.Context, req Req) (Resp, error)
//
// A non-nil error is handled in exactly one of two ways, and which way
// applies is decided ENTIRELY by whether that op's CSIL declaration carries
// a `/ ServiceError` arm (csil/firepit.csil) — which in turn decides whether
// api/internal/server/dispatch.go wired that op through routeFallible or
// routeInfallible:
//
//  1. Ops WITH a declared `/ ServiceError` arm (most mutating ops, plus a
//     few fallible reads like get-board): return an *AppError, built with
//     this package's constructors (NotFound, Forbidden, Unauthenticated,
//     Validation, Conflict, Internal, Unimplemented). The dispatcher
//     recognizes *AppError via errors.As, encodes it as the declared
//     ServiceError success-arm (transport status 0 "ok", variant
//     "ServiceError"), and the caller sees a typed, catchable error — never
//     an HTTP-level or transport-level failure. Return an *AppError for
//     every EXPECTED failure mode: not-found, authz denial, validation,
//     conflict, unimplemented.
//
//  2. Ops with NO declared error arm (reads the schema itself declares can't
//     fail — e.g. list-boards, mark-read, unread-summary): there is no typed
//     channel to carry a failure. ANY non-nil error returned from these —
//     *AppError or a plain error — is mapped by the dispatcher to a
//     transport-level failure (status 6, "internal"); the real error is
//     logged server-side only, never assumed safe to hand to the caller
//     verbatim. Real implementations of these ops should reserve returning
//     an error at all for genuinely unexpected failures (a DB outage) — an
//     expected "nothing to show" case is a normal empty-page reply, not an
//     error.
//
// A plain (non-*AppError) Go error returned from a FALLIBLE op (case 1
// above) is ALSO treated as an internal transport-level failure rather than
// assumed safe to expose — *AppError is the only channel that reaches the
// caller as structured, actionable detail. This mirrors longhouse's
// csilrpc.Error convention (longhouse/api/internal/csilrpc/errors.go):
// typed failures are opt-in and explicit, never "whatever error came back."
//
// # The stub implementations in this package
//
// Every method in every file here (task B1) returns Unimplemented(<op>)
// unconditionally. B2 through B9 replace method bodies one service at a
// time as they land; the struct, constructor, and import shape shouldn't
// need to change — only method bodies.
package csilservices
