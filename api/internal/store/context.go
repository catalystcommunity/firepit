package store

import "context"

// currentUserContextKey is the context key the session middleware
// (api/internal/server/session.go) attaches the authenticated caller's
// *User under, and CurrentUser reads back.
//
// This lives in package store — rather than in api/internal/server, which
// is where "the session middleware's context helper" conceptually belongs —
// because api/internal/server already imports api/internal/csilservices
// (dispatch.go needs *csilservices.AppError to recognize a typed service
// error). csilservices in turn needs to read the same "who is the caller"
// value out of ctx to implement any authz-gated op (every mutating
// SettingsService/SocialService method, and every sibling B3-B8 service
// that isn't purely public-read). If that accessor lived in server,
// csilservices -> server would close an import cycle with server ->
// csilservices. store has no dependents of its own, so it's the one
// package both sides can already reach: the session middleware calls
// WithCurrentUser after a successful cookie lookup, and every service in
// csilservices calls CurrentUser to find out who's asking.
//
// api/internal/server/session.go's exported CurrentUser (and its
// unexported withCurrentUser) are thin forwarding wrappers around the two
// functions below — same names, same signatures, so nothing that already
// depends on server.CurrentUser needs to change.
type currentUserContextKey struct{}

// CurrentUser returns the user attached to ctx by the session middleware,
// or ok=false if the caller has no valid session — anonymous access is a
// normal, expected state (PLANDOC.md §1: anonymous read on public boards),
// never an error on its own. Individual service methods decide whether
// anonymous access is allowed for a given op.
func CurrentUser(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(currentUserContextKey{}).(*User)
	return u, ok
}

// WithCurrentUser attaches u to ctx for CurrentUser to retrieve later.
func WithCurrentUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, currentUserContextKey{}, u)
}
