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
// cookie on successful login) must reuse this exact constant rather than a
// hardcoded string.
const SessionCookieName = "firepit_session"

// CurrentUser returns the user attached to ctx by the session middleware, or
// ok=false if the caller has no valid session — which is a normal,
// expected state (anonymous read), never an error on its own. Individual
// service methods decide whether anonymous access is allowed for a given
// op; this helper just answers "who, if anyone."
// It's a thin forward to api/internal/reqctx (see that package's doc
// comment for why the context-key plumbing itself lives there rather than
// here: csilservices needs the same accessor and can't import this package
// without an import cycle through dispatch.go, which already imports
// csilservices for *csilservices.AppError).
func CurrentUser(ctx context.Context) (*store.User, bool) {
	return reqctx.User(ctx)
}

// sessionMiddleware reads the session cookie (if any), looks it up via st,
// and attaches the resulting user to the request context. A missing cookie,
// an unrecognized token, or an expired session all resolve to "no user
// attached" — anonymous read is a supported feature (PLANDOC.md §1), not an
// error, so this middleware never rejects a request; it only enriches the
// context for handlers (and later, authz checks) that care.
func sessionMiddleware(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
				if user := lookupSessionUser(ctx, st, cookie.Value); user != nil {
					ctx = reqctx.WithUser(ctx, user)
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// lookupSessionUser resolves a raw session token to its owning user, or nil
// if there's no valid session for it (missing, unknown, or expired — see
// store.Store.LookupSession) or the session's user row is itself gone.
// Every failure path here is logged at debug level and returns nil rather
// than propagating an error: by design, callers treat "no user" as
// anonymous, not as a fault.
func lookupSessionUser(ctx context.Context, st *store.Store, rawToken string) *store.User {
	sess, err := st.LookupSession(ctx, store.HashSessionToken(rawToken))
	if err != nil {
		if !store.IsNotFound(err) {
			log.WithError(err).Debug("session lookup failed")
		}
		return nil
	}
	user, err := st.GetUser(ctx, sess.UserID)
	if err != nil {
		log.WithError(err).WithField("user_id", sess.UserID).Debug("session referenced a missing user")
		return nil
	}
	return user
}
