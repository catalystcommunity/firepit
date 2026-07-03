package csilservices

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/config"
	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/linkkeys"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// AuthService implements the linkkeys-backed login flow (task B2,
// PLANDOC.md §3, §5) across two entry points that must agree on how they
// talk to the RP and mint/verify nonces without duplicating that
// construction:
//
//   - authService.BeginLogin below (the CSIL op: sign-request, mint the
//     self-verifying login nonce, return the IDP redirect URL).
//   - CompleteLogin (called from the one HTTP-native, non-CSIL route this
//     flow needs: server/authcallback.go's GET /auth/callback — the IDP can
//     only redirect a plain browser GET, which can't be a CSIL-RPC POST).
//
// Both call BuildPKIClient(cfg) and NewLoginOptions(cfg) to build identical
// dependencies from the same config.Config, so this file is the one place
// that decides how firepit talks to the linkkeys RP sidecar.
//
// # Why no cookie carries the login nonce
//
// PLANDOC.md §3 describes begin-login as setting "a nonce cookie" before the
// 302 to the IDP. In practice begin-login is a CSIL-RPC op
// (api/internal/server/dispatch.go's routeFallible/typedHandler), and the
// HTTP carrier for CSIL-RPC (server.go's handleRPC) has no channel for a
// service method to set a response header — every op returns (Resp, error),
// nothing else. Retrofitting one would mean widening HandlerOutcome/
// typedHandler for every op across every Wave B service, a much bigger and
// riskier change than this task's scope. A self-verifying nonce (HMAC +
// embedded expiry, api/internal/linkkeys/nonce.go, ported from longhouse's
// api/internal/auth/nonce.go) achieves the same "prove this login round-trip
// is ours" property with NO server-side state at all — not a cookie, not a
// database row (this task's store ownership is users/sessions only, no new
// nonce table) — so it both sidesteps the transport limitation and fits the
// ownership scope. It round-trips through the RP+IDP inside the assertion's
// own `nonce` field, which GET /auth/callback re-verifies.
type authService struct {
	store       *store.Store
	pki         PKIClient
	opts        LoginOptions
	idpURL      string
	callbackURL string
}

// PKIClient is the subset of linkkeys.Client/linkkeys.TCPClient the auth flow
// uses. Kept as an interface (rather than depending on a concrete type) so
// tests can inject a fake/mock RP without a live sidecar.
type PKIClient interface {
	SignRequest(callbackURL, nonce string) (string, error)
	DecryptToken(encryptedToken string) (string, error)
	VerifyAssertion(signedAssertion, expectedDomain string) (*linkkeys.Assertion, error)
}

// UserInfoFetcher is an optional capability a PKIClient may implement to
// redeem a verified assertion for the full linkkeys UserInfo (display name +
// released claims) — richer than the assertion itself. linkkeys.TCPClient
// implements it; linkkeys.Client (the legacy HTTP shim) does not.
// Enrichment is best-effort: login never blocks on it (see fetchClaims).
type UserInfoFetcher interface {
	FetchUserInfo(token, domain string) (*linkkeys.UserInfo, error)
}

// LoginOptions is the small bundle of config both halves of the login flow
// need beyond the store/PKI client, so BeginLogin and CompleteLogin build
// identical dependencies from the same config.Config.
type LoginOptions struct {
	// IDPDomain is the single identity domain this deployment's IDP serves
	// (PLANDOC.md §2's "first deployment" IDP, linkkeys.todandlorna.com).
	// v1 is single-IDP: BeginLogin rejects any other requested domain, and
	// CompleteLogin verifies the assertion against this one. Empty disables
	// both checks (useful for a dev/test RP that doesn't care).
	IDPDomain string
	// RPDomain is firepit's own relying-party identity, checked against the
	// assertion's `audience` claim. Empty disables the check.
	RPDomain string
	// NonceSecret HMAC-signs/verifies the self-verifying login nonce (see
	// the package doc comment above and api/internal/linkkeys/nonce.go).
	NonceSecret []byte
	// SessionTTL is how long a freshly minted session cookie is valid.
	SessionTTL time.Duration
}

// NewLoginOptions builds LoginOptions from cfg — the one place both
// NewAuthService and server/authcallback.go's CompleteLogin call read
// config.Config into the values the login flow actually needs.
func NewLoginOptions(cfg config.Config) LoginOptions {
	return LoginOptions{
		IDPDomain:   cfg.LinkkeysIDPDomain,
		RPDomain:    cfg.LinkkeysRPDomain(),
		NonceSecret: []byte(cfg.SessionNonceSecret),
		SessionTTL:  store.DefaultSessionTTL,
	}
}

// BuildPKIClient constructs the linkkeys RP client for cfg's configured
// transport, or nil if that transport is unconfigured. "tcp" speaks
// canonical CSIL-RPC over TCP (linkkeys.TCPClient, additionally satisfying
// UserInfoFetcher); "http" (default) speaks the legacy JSON PKI sidecar
// (linkkeys.Client). Both satisfy PKIClient. Exported so
// server/authcallback.go's GET /auth/callback builds the identical client
// NewAuthService's BeginLogin uses, without duplicating this selection
// logic (mirrors longhouse's cmd/serve.go buildPKIClient, relocated here
// since firepit has no equivalent per-binary wiring file for it to live in
// without duplicating imports across cmd/ and server/).
func BuildPKIClient(cfg config.Config) PKIClient {
	switch strings.ToLower(cfg.LinkkeysTransport) {
	case "tcp":
		if cfg.LinkkeysIDPDomain == "" && cfg.LinkkeysTCPAddr == "" {
			return nil
		}
		var fps []string
		if cfg.LinkkeysTCPFingerprints != "" {
			fps = strings.Split(cfg.LinkkeysTCPFingerprints, ",")
		}
		c, err := linkkeys.NewTCPClient(linkkeys.TCPConfig{
			Domain:       cfg.LinkkeysIDPDomain,
			APIBase:      cfg.LinkkeysIDPURL,
			APIKey:       cfg.LinkkeysPKIAPIKey,
			Addr:         cfg.LinkkeysTCPAddr,
			Fingerprints: fps,
			Insecure:     cfg.LinkkeysTCPAllowInsecure,
		})
		if err != nil {
			log.WithError(err).Error("linkkeys tcp transport init failed; login will fail")
			return nil
		}
		return c
	default: // "http"
		if cfg.LinkkeysPKIURL == "" {
			return nil
		}
		return linkkeys.New(cfg.LinkkeysPKIURL, cfg.LinkkeysPKIAPIKey, cfg.LinkkeysPKIAllowInvalidCerts)
	}
}

// NewAuthService constructs the AuthService implementation.
func NewAuthService(st *store.Store, cfg config.Config) csil.AuthService {
	return &authService{
		store:       st,
		pki:         BuildPKIClient(cfg),
		opts:        NewLoginOptions(cfg),
		idpURL:      cfg.LinkkeysIDPURL,
		callbackURL: cfg.AppCallbackURL,
	}
}

// BeginLogin starts a login: mints a self-verifying nonce, asks the RP to
// sign an auth request bound to it, and returns the IDP URL the SPA should
// navigate the browser to next (PLANDOC.md §3).
func (s *authService) BeginLogin(_ context.Context, req csil.BeginLoginRequest) (csil.BeginLoginResponse, error) {
	if s.pki == nil {
		return csil.BeginLoginResponse{}, Internal("linkkeys RP is not configured")
	}
	if err := req.Validate(); err != nil {
		return csil.BeginLoginResponse{}, Validation("domain", err.Error())
	}
	domain := strings.TrimSpace(req.Domain)
	if s.opts.IDPDomain != "" && domain != s.opts.IDPDomain {
		return csil.BeginLoginResponse{}, Validation("domain", "this instance only supports the "+s.opts.IDPDomain+" identity domain")
	}
	if s.callbackURL == "" {
		return csil.BeginLoginResponse{}, Internal("auth callback URL is not configured")
	}
	if s.idpURL == "" {
		return csil.BeginLoginResponse{}, Internal("linkkeys IDP URL is not configured")
	}

	nonce := linkkeys.MintNonce(s.opts.NonceSecret)
	signedRequest, err := s.pki.SignRequest(s.callbackURL, nonce)
	if err != nil {
		log.WithError(err).Error("begin-login: RP sign-request failed")
		return csil.BeginLoginResponse{}, Internal("could not reach identity service")
	}

	q := url.Values{}
	q.Set("signed_request", signedRequest)
	redirectURL := strings.TrimRight(s.idpURL, "/") + "/auth/authorize?" + q.Encode()
	return csil.BeginLoginResponse{RedirectUrl: redirectURL}, nil
}

// Logout deletes exactly the session the caller's cookie names (never every
// session belonging to that user — logging out one device shouldn't touch
// any other). Per the CSIL schema's declared contract (no ServiceError arm;
// see the stub's original comment this replaces), logout never errors even
// when already logged out or anonymous — a delete failure is logged and
// swallowed rather than surfaced, since there is no typed channel to carry
// it and the dispatcher would otherwise turn it into an opaque transport
// failure for an op that's supposed to always succeed.
func (s *authService) Logout(ctx context.Context, _ csil.Empty) (csil.Empty, error) {
	if sess, ok := reqctx.Session(ctx); ok {
		if err := s.store.DeleteSession(ctx, sess.TokenHash); err != nil {
			log.WithError(err).Warn("logout: session delete failed (still reporting success)")
		}
	}
	return csil.Empty{}, nil
}

// Whoami reflects the caller's own profile from the session middleware's
// resolved user (api/internal/server/session.go, via api/internal/reqctx).
func (s *authService) Whoami(ctx context.Context, _ csil.Empty) (csil.UserProfile, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return csil.UserProfile{}, Unauthenticated("no active session")
	}
	return csil.UserProfile{
		Id:             csil.UserID(user.ID),
		LinkkeysDomain: user.LinkkeysDomain,
		Handle:         user.Handle,
		DisplayName:    user.DisplayName,
		Kind:           csil.UserKind(user.Kind),
		Roles:          []string(user.Roles),
		CreatedAt:      user.CreatedAt,
	}, nil
}

// CompleteLoginResult is what a successful GET /auth/callback round-trip
// produces.
type CompleteLoginResult struct {
	User     *store.User
	Session  *store.Session
	RawToken string
}

// CompleteLogin runs the second half of the linkkeys login flow — the first
// half is authService.BeginLogin above. Call sequence (PLANDOC.md §3, §5):
// decrypt-token -> verify-assertion (nonce, audience, expiry) ->
// userinfo-fetch (best-effort) -> upsert users row -> mint a session.
//
// Returns an *AppError for every EXPECTED failure (bad/expired token, nonce
// rejected, domain/audience mismatch, expired assertion) so
// server/authcallback.go can map it to a meaningful HTTP status; a
// non-*AppError return means an unexpected failure (store/DB error).
func CompleteLogin(ctx context.Context, st *store.Store, pki PKIClient, opts LoginOptions, encryptedToken string) (*CompleteLoginResult, error) {
	if pki == nil {
		return nil, Internal("linkkeys RP is not configured")
	}
	if strings.TrimSpace(encryptedToken) == "" {
		return nil, Validation("encrypted_token", "encrypted_token is required")
	}

	signedAssertion, err := pki.DecryptToken(encryptedToken)
	if err != nil {
		log.WithError(err).Warn("auth callback: RP decrypt-token failed")
		return nil, Internal("could not decrypt token")
	}

	assertion, err := pki.VerifyAssertion(signedAssertion, opts.IDPDomain)
	if err != nil {
		log.WithError(err).Info("auth callback: assertion verification failed")
		return nil, Unauthenticated("assertion verification failed")
	}

	if opts.IDPDomain != "" && assertion.Domain != opts.IDPDomain {
		log.WithFields(log.Fields{"domain": assertion.Domain}).Info("auth callback: assertion from unexpected domain")
		return nil, Unauthenticated("assertion from unexpected domain")
	}
	if assertion.Audience != "" && opts.RPDomain != "" && assertion.Audience != opts.RPDomain {
		log.WithFields(log.Fields{"audience": assertion.Audience}).Info("auth callback: assertion audience mismatch")
		return nil, Unauthenticated("assertion audience mismatch")
	}
	if err := linkkeys.VerifyNonce(opts.NonceSecret, assertion.Nonce); err != nil {
		log.WithError(err).Info("auth callback: nonce rejected")
		return nil, Unauthenticated("login nonce invalid or expired")
	}
	if expired, err := assertionExpired(assertion); err != nil {
		return nil, Unauthenticated("assertion has a malformed expiry")
	} else if expired {
		return nil, Unauthenticated("assertion has expired")
	}

	claims := fetchClaims(pki, signedAssertion, assertion)
	displayName := resolveDisplayName(claims, assertion)
	baseHandle := deriveHandleCandidate(claims, assertion)

	user, err := st.UpsertUser(ctx, assertion.Domain, assertion.UserID, baseHandle, displayName)
	if err != nil {
		log.WithError(err).Error("auth callback: user upsert failed")
		return nil, err
	}

	sess, rawToken, err := st.CreateSession(ctx, user.ID, opts.SessionTTL)
	if err != nil {
		log.WithError(err).Error("auth callback: session mint failed")
		return nil, err
	}

	return &CompleteLoginResult{User: user, Session: sess, RawToken: rawToken}, nil
}

// assertionExpired parses the assertion's own expires_at (RFC3339, set by
// the IDP — distinct from the login nonce's independent, shorter-lived
// embedded expiry checked just before this) and reports whether it's in the
// past. An empty ExpiresAt (only ever seen from a misbehaving or
// intentionally-lenient test RP) is treated as "not expired" — the field is
// optional on the wire (linkkeys.Assertion has no `omitempty`-defeating
// requirement here), so its absence isn't itself a failure.
func assertionExpired(a *linkkeys.Assertion) (bool, error) {
	if a.ExpiresAt == "" {
		return false, nil
	}
	expiresAt, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return false, err
	}
	return time.Now().UTC().After(expiresAt), nil
}

// fetchClaims redeems a verified assertion for the claim set the user's
// consent + the IDP's policy released to us (claim_type -> value).
// Best-effort and strictly optional: returns nil when the PKI transport
// can't fetch UserInfo (the HTTP shim), the fetch fails, or nothing was
// released — callers degrade to the assertion's own fields. Ported from
// longhouse's csilservices.AuthService.fetchClaims.
func fetchClaims(pki PKIClient, token string, a *linkkeys.Assertion) map[string]string {
	fetcher, ok := pki.(UserInfoFetcher)
	if !ok {
		return nil
	}
	info, err := fetcher.FetchUserInfo(token, a.Domain)
	if err != nil {
		log.WithError(err).Debug("auth: userinfo-fetch failed; proceeding without claims")
		return nil
	}
	if info == nil {
		return nil
	}
	claims := info.ClaimValues()
	if info.DisplayName != "" {
		if _, ok := claims["display_name"]; !ok {
			if claims == nil {
				claims = make(map[string]string, 1)
			}
			claims["display_name"] = info.DisplayName
		}
	}
	return claims
}

// resolveDisplayName prefers a released display_name claim, falling back to
// the assertion's own display name when none was granted.
func resolveDisplayName(claims map[string]string, a *linkkeys.Assertion) string {
	if v, ok := claims["display_name"]; ok && v != "" {
		return v
	}
	return a.DisplayName
}

// handleSlugPattern matches the characters a derived handle is allowed to
// keep; everything else is dropped before collapsing/trimming. Mirrors the
// board slug convention (csil/types/boards.csil) but without the length cap
// applied here — UpsertUser's collision suffixing may extend it a little,
// and a hard cap on top of that is left to a future validation pass if it
// turns out to matter.
var handleSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// deriveHandleCandidate builds the base (pre-collision-resolution) handle
// for a brand new user from the strongest available identity signal, in
// order: a released `handle` claim, then a released or assertion-carried
// display name (slugified), then the raw linkkeys user id (slugified). Falls
// back to the literal "user" if every signal strips to nothing (e.g. a
// display name that's 100% emoji) so UpsertUser never has to insert an empty
// handle. See store.UpsertUser's doc comment for what happens next
// (collision resolution) — this function only picks the starting point.
func deriveHandleCandidate(claims map[string]string, a *linkkeys.Assertion) string {
	if v, ok := claims["handle"]; ok {
		if slug := slugify(v); slug != "" {
			return slug
		}
	}
	if v, ok := claims["display_name"]; ok {
		if slug := slugify(v); slug != "" {
			return slug
		}
	}
	if a.DisplayName != "" {
		if slug := slugify(a.DisplayName); slug != "" {
			return slug
		}
	}
	if slug := slugify(a.UserID); slug != "" {
		return slug
	}
	return "user"
}

// slugify lowercases s, replaces every run of non-[a-z0-9] characters with a
// single hyphen, and trims leading/trailing hyphens.
func slugify(s string) string {
	lowered := strings.ToLower(s)
	slug := handleSlugPattern.ReplaceAllString(lowered, "-")
	return strings.Trim(slug, "-")
}

// "Who is the caller" during Whoami/Logout above comes from
// api/internal/reqctx (reqctx.User / reqctx.Session), not a context key
// owned by this file: api/internal/server/dispatch.go already imports
// csilservices (for *AppError), so csilservices importing server back for a
// CurrentUser-style helper would be a compile-time cycle. reqctx is the
// shared, dependency-free package both sides depend on instead — see its
// package doc comment for the full rationale. server/session.go's
// sessionMiddleware calls reqctx.WithUser/WithSession after resolving the
// cookie; server.CurrentUser forwards to reqctx.User for any caller that
// already used that name.
