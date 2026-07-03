package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

// VerifySignature reports whether header (the request's
// X-Hub-Signature-256 value) is a valid HMAC-SHA256 signature of body under
// secret, per GitHub's documented scheme
// (https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries):
// header is "sha256=" followed by the hex-encoded HMAC. The comparison
// itself is constant-time (hmac.Equal on the decoded MAC bytes) so a
// timing side-channel can't be used to guess the signature byte by byte.
//
// An empty, malformed, or wrong-prefix header is treated as "not valid"
// (returns false) rather than panicking or erroring — every caller path
// through Handler.ServeHTTP maps any false here to the same HTTP 401.
func VerifySignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if secret == "" || !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// ResolveSecret resolves a github_mappings.secret_ref to the actual HMAC
// secret value. secret_ref is the NAME of an environment variable (see the
// package doc comment's "Secret resolution" section for the
// FIREPIT_GH_SECRET_<...> naming convention), never the secret itself — so
// resolution is just an env lookup. ok is false if the env var is unset or
// empty, which Handler.ServeHTTP treats as a hard verification failure
// (HTTP 401), never as "skip verification."
func ResolveSecret(secretRef string) (secret string, ok bool) {
	v := os.Getenv(secretRef)
	return v, v != ""
}
