package server

import (
	"context"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// SessionCookieName is the cookie firepit's own session lives in (PLANDOC.md
// §3: "Sessions are firepit's; linkkeys only verifies identity"). There is
// no longhouse precedent to mirror here — longhouse is bearer-JWT-only, no
// cookies at all — so this name is new to firepit; B2 (which mints the
// cookie on successful login, server/authcallback.go) reuses this exact
// constant rather than a hardcoded string.
const SessionCookieName = "firepit_session"

// CurrentUser returns the user attached to ctx by the session middleware, or
// ok=false if the caller has no valid session — which is a normal,
// expected state (anonymous read), never an error on its own. Individual
// service methods decide whether anonymous access is allowed for a given
// op; this helper just answers "who, if anyone."
//
// Delegates to reqctx.User (api/internal/reqctx): the context key/storage
// lives there because csilservices (every service implementation, this
// task's AuthService.Whoami included) cannot import this package without a
// cycle (dispatch.go already imports csilservices the other way), and
// reqctx is the shared, dependency-free package both sides depend on
// instead. This wrapper keeps server.CurrentUser's name, signature, and doc
// contract unchanged for any caller that already used it.
func CurrentUser(ctx context.Context) (*store.User, bool) {
	return reqctx.User(ctx)
}

// sessionMiddleware reads the session cookie (if any), looks it up via st,
// and attaches the resulting user AND the session row itself to the
// request context (see reqctx.WithUser/WithSession — AuthService.Logout
// needs the session row to delete exactly the one the cookie names, not
// every session belonging to that user). A missing cookie, an unrecognized
// token, or an expired session all resolve to "no user attached" —
// anonymous read is a supported feature (PLANDOC.md §1), not an error, so
// this middleware never rejects a request; it only enriches the context for
// handlers (and authz checks) that care.
func sessionMiddleware(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
				if user, sess := lookupSessionUser(ctx, st, cookie.Value); user != nil {
					ctx = reqctx.WithUser(ctx, user)
					ctx = reqctx.WithSession(ctx, sess)
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// lookupSessionUser resolves a raw session token to its owning user and
// session row, or (nil, nil) if there's no valid session for it (missing,
// unknown, or expired — see store.Store.LookupSession) or the session's
// user row is itself gone. Every failure path here is logged at debug level
// and returns nil rather than propagating an error: by design, callers
// treat "no user" as anonymous, not as a fault.
func lookupSessionUser(ctx context.Context, st *store.Store, rawToken string) (*store.User, *store.Session) {
	sess, err := st.LookupSession(ctx, store.HashSessionToken(rawToken))
	if err != nil {
		if !store.IsNotFound(err) {
			log.WithError(err).Debug("session lookup failed")
		}
		return nil, nil
	}
	user, err := st.GetUser(ctx, sess.UserID)
	if err != nil {
		log.WithError(err).WithField("user_id", sess.UserID).Debug("session referenced a missing user")
		return nil, nil
	}
	return user, sess
}
