package reqctx

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// This file extends reqctx (see reqctx.go's package doc for why "who is the
// caller" plumbing lives in this shared, dependency-free package rather than
// in api/internal/server or api/internal/csilservices) with the underlying
// *store.Session row itself, not just the user it belongs to. Task B2's
// AuthService.Logout (api/internal/csilservices/auth.go) needs this: to
// delete exactly the ONE session the presented cookie names — never every
// session belonging to that user, which would log out every device — it
// needs the session's token hash, which User/WithUser alone don't carry.
// Kept in its own file so reqctx.go itself (shared verbatim with sibling
// tasks that only need User) never needs to change for this.

type sessionKey struct{}

// WithSession attaches sess to ctx for Session to retrieve later. Called by
// api/internal/server's session middleware alongside WithUser, once it has
// resolved a session cookie to both the owning user and the session row.
func WithSession(ctx context.Context, sess *store.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, sess)
}

// Session returns the store.Session behind the caller's session cookie, or
// ok=false if there is none (no cookie, or an invalid/expired one).
func Session(ctx context.Context) (*store.Session, bool) {
	sess, ok := ctx.Value(sessionKey{}).(*store.Session)
	return sess, ok
}
