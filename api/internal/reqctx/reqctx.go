// Package reqctx carries the current authenticated user (if any) on a
// request's context.Context, shared between api/internal/server (which
// attaches it) and api/internal/csilservices (which reads it back for
// authorization checks).
//
// This lives in its own tiny package, rather than the user simply being a
// server.CurrentUser accessor as originally sketched, because of an import
// direction constraint: api/internal/server/dispatch.go already imports
// csilservices (for *csilservices.AppError, to encode a service method's
// returned error as the declared ServiceError success-arm). If
// csilservices needed to import server back for CurrentUser, that would be
// an import cycle (server -> csilservices -> server). Both sides depend on
// this package instead, and neither depends on the other for this.
package reqctx

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

type userKey struct{}

// WithUser attaches u to ctx for User to retrieve later. Called by
// api/internal/server's session middleware once it has resolved a session
// cookie to a user row.
func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// User returns the user attached to ctx, or ok=false if the caller has no
// valid session — which is a normal, expected state (anonymous read), never
// an error on its own. Individual service methods decide whether anonymous
// access is allowed for a given op; this helper just answers "who, if
// anyone."
func User(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userKey{}).(*store.User)
	return u, ok
}
