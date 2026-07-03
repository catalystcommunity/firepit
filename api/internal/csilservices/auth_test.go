package csilservices

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/catalystcommunity/firepit/api/internal/config"
	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/linkkeys"
)

// --- handle/display-name derivation (pure, no I/O) ---

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Ada Lovelace":    "ada-lovelace",
		"  spaced  out  ": "spaced-out",
		"already-slug":    "already-slug",
		"Emoji🎉Party":     "emoji-party",
		"---":             "",
		"":                "",
		"MiXeD_Case.99":   "mixed-case-99",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveHandleCandidate(t *testing.T) {
	t.Run("prefers a released handle claim", func(t *testing.T) {
		claims := map[string]string{"handle": "Ada H.", "display_name": "someone else"}
		a := &linkkeys.Assertion{UserID: "u1", DisplayName: "assertion name"}
		require.Equal(t, "ada-h", deriveHandleCandidate(claims, a))
	})

	t.Run("falls back to a released display_name claim", func(t *testing.T) {
		claims := map[string]string{"display_name": "Grace Hopper"}
		a := &linkkeys.Assertion{UserID: "u1", DisplayName: "assertion name"}
		require.Equal(t, "grace-hopper", deriveHandleCandidate(claims, a))
	})

	t.Run("falls back to the assertion's own display name", func(t *testing.T) {
		a := &linkkeys.Assertion{UserID: "u1", DisplayName: "Margaret Hamilton"}
		require.Equal(t, "margaret-hamilton", deriveHandleCandidate(nil, a))
	})

	t.Run("falls back to the raw linkkeys user id", func(t *testing.T) {
		a := &linkkeys.Assertion{UserID: "user-42"}
		require.Equal(t, "user-42", deriveHandleCandidate(nil, a))
	})

	t.Run("falls back to the literal 'user' when everything strips to nothing", func(t *testing.T) {
		a := &linkkeys.Assertion{UserID: "🎉🎉🎉"}
		require.Equal(t, "user", deriveHandleCandidate(nil, a))
	})

	t.Run("empty claim values are skipped in favor of the next signal", func(t *testing.T) {
		claims := map[string]string{"handle": "   ", "display_name": "Katherine Johnson"}
		a := &linkkeys.Assertion{UserID: "u1"}
		require.Equal(t, "katherine-johnson", deriveHandleCandidate(claims, a))
	})
}

func TestResolveDisplayName(t *testing.T) {
	t.Run("prefers a released display_name claim", func(t *testing.T) {
		claims := map[string]string{"display_name": "Claimed Name"}
		a := &linkkeys.Assertion{DisplayName: "Assertion Name"}
		require.Equal(t, "Claimed Name", resolveDisplayName(claims, a))
	})
	t.Run("falls back to the assertion's display name absent a claim", func(t *testing.T) {
		a := &linkkeys.Assertion{DisplayName: "Assertion Name"}
		require.Equal(t, "Assertion Name", resolveDisplayName(nil, a))
	})
	t.Run("an empty claim value doesn't override the assertion", func(t *testing.T) {
		claims := map[string]string{"display_name": ""}
		a := &linkkeys.Assertion{DisplayName: "Assertion Name"}
		require.Equal(t, "Assertion Name", resolveDisplayName(claims, a))
	})
}

// --- assertion expiry ---

func TestAssertionExpired(t *testing.T) {
	t.Run("empty expires_at is treated as not expired", func(t *testing.T) {
		expired, err := assertionExpired(&linkkeys.Assertion{})
		require.NoError(t, err)
		require.False(t, expired)
	})
	t.Run("future expiry is not expired", func(t *testing.T) {
		expired, err := assertionExpired(&linkkeys.Assertion{ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		require.NoError(t, err)
		require.False(t, expired)
	})
	t.Run("past expiry is expired", func(t *testing.T) {
		expired, err := assertionExpired(&linkkeys.Assertion{ExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)})
		require.NoError(t, err)
		require.True(t, expired)
	})
	t.Run("malformed expiry is an error", func(t *testing.T) {
		_, err := assertionExpired(&linkkeys.Assertion{ExpiresAt: "not-a-timestamp"})
		require.Error(t, err)
	})
}

// --- BuildPKIClient ---

func TestBuildPKIClient(t *testing.T) {
	t.Run("http transport with no PKI URL is unconfigured", func(t *testing.T) {
		require.Nil(t, BuildPKIClient(config.Config{LinkkeysTransport: "http"}))
	})
	t.Run("http transport with a PKI URL builds a client", func(t *testing.T) {
		got := BuildPKIClient(config.Config{LinkkeysTransport: "http", LinkkeysPKIURL: "https://rp.example"})
		require.NotNil(t, got)
		require.IsType(t, &linkkeys.Client{}, got)
	})
	t.Run("tcp transport with no domain or addr is unconfigured", func(t *testing.T) {
		require.Nil(t, BuildPKIClient(config.Config{LinkkeysTransport: "tcp"}))
	})
	t.Run("default (empty) transport behaves like http", func(t *testing.T) {
		require.Nil(t, BuildPKIClient(config.Config{}))
	})
}

// --- BeginLogin / CompleteLogin flow (via a fake PKIClient; no store/DB) ---

type fakePKI struct {
	signRequestFn     func(callbackURL, nonce string) (string, error)
	decryptTokenFn    func(string) (string, error)
	verifyAssertionFn func(string, string) (*linkkeys.Assertion, error)
}

func (f *fakePKI) SignRequest(callbackURL, nonce string) (string, error) {
	return f.signRequestFn(callbackURL, nonce)
}
func (f *fakePKI) DecryptToken(tok string) (string, error) { return f.decryptTokenFn(tok) }
func (f *fakePKI) VerifyAssertion(signed, domain string) (*linkkeys.Assertion, error) {
	return f.verifyAssertionFn(signed, domain)
}

func TestBeginLogin(t *testing.T) {
	baseOpts := LoginOptions{IDPDomain: "idp.example", NonceSecret: []byte("secret")}

	t.Run("rejects an empty domain", func(t *testing.T) {
		svc := &authService{pki: &fakePKI{}, opts: baseOpts, idpURL: "https://idp.example", callbackURL: "https://app/callback"}
		_, err := svc.BeginLogin(context.Background(), csil.BeginLoginRequest{Domain: ""})
		requireAppErrCode(t, err, CodeValidation)
	})

	t.Run("rejects a domain other than the configured IDP domain", func(t *testing.T) {
		svc := &authService{pki: &fakePKI{}, opts: baseOpts, idpURL: "https://idp.example", callbackURL: "https://app/callback"}
		_, err := svc.BeginLogin(context.Background(), csil.BeginLoginRequest{Domain: "someone-elses-idp.example"})
		requireAppErrCode(t, err, CodeValidation)
	})

	t.Run("returns Internal when the PKI client is unconfigured", func(t *testing.T) {
		svc := &authService{pki: nil, opts: baseOpts}
		_, err := svc.BeginLogin(context.Background(), csil.BeginLoginRequest{Domain: "idp.example"})
		requireAppErrCode(t, err, CodeInternal)
	})

	t.Run("happy path mints a nonce and returns the IDP redirect URL", func(t *testing.T) {
		var gotNonce string
		pki := &fakePKI{signRequestFn: func(callbackURL, nonce string) (string, error) {
			require.Equal(t, "https://app/callback", callbackURL)
			gotNonce = nonce
			return "signed-request-blob", nil
		}}
		svc := &authService{pki: pki, opts: baseOpts, idpURL: "https://idp.example", callbackURL: "https://app/callback"}
		resp, err := svc.BeginLogin(context.Background(), csil.BeginLoginRequest{Domain: "idp.example"})
		require.NoError(t, err)
		require.Contains(t, resp.RedirectUrl, "https://idp.example/auth/authorize?")
		require.Contains(t, resp.RedirectUrl, "signed_request=signed-request-blob")
		require.NotEmpty(t, gotNonce)
		require.NoError(t, linkkeys.VerifyNonce(baseOpts.NonceSecret, gotNonce))
	})

	t.Run("wraps an RP sign-request failure as Internal", func(t *testing.T) {
		pki := &fakePKI{signRequestFn: func(string, string) (string, error) { return "", errors.New("rp unreachable") }}
		svc := &authService{pki: pki, opts: baseOpts, idpURL: "https://idp.example", callbackURL: "https://app/callback"}
		_, err := svc.BeginLogin(context.Background(), csil.BeginLoginRequest{Domain: "idp.example"})
		requireAppErrCode(t, err, CodeInternal)
	})
}

func requireAppErrCode(t *testing.T, err error, want uint64) {
	t.Helper()
	require.Error(t, err)
	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, want, appErr.Code)
}
