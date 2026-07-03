package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignatureValid(t *testing.T) {
	body := FixtureIssuesOpened
	secret := "s3kr1t"
	header := sign(secret, body)

	if !VerifySignature(secret, body, header) {
		t.Fatal("expected a valid signature to verify")
	}
}

func TestVerifySignatureInvalid(t *testing.T) {
	body := FixtureIssuesOpened
	header := sign("s3kr1t", body)

	t.Run("wrong secret", func(t *testing.T) {
		if VerifySignature("wrong-secret", body, header) {
			t.Fatal("expected verification to fail with the wrong secret")
		}
	})

	t.Run("tampered body", func(t *testing.T) {
		tampered := append([]byte(nil), body...)
		tampered[0] = 'X'
		if VerifySignature("s3kr1t", tampered, header) {
			t.Fatal("expected verification to fail against a tampered body")
		}
	})

	t.Run("malformed header", func(t *testing.T) {
		if VerifySignature("s3kr1t", body, "not-a-valid-signature") {
			t.Fatal("expected verification to fail on a malformed header")
		}
	})

	t.Run("wrong prefix", func(t *testing.T) {
		if VerifySignature("s3kr1t", body, "sha1=deadbeef") {
			t.Fatal("expected verification to reject a non-sha256 prefix")
		}
	})
}

func TestVerifySignatureMissing(t *testing.T) {
	if VerifySignature("s3kr1t", FixtureIssuesOpened, "") {
		t.Fatal("expected verification to fail with no signature header at all")
	}
}

func TestVerifySignatureEmptySecret(t *testing.T) {
	header := sign("", FixtureIssuesOpened)
	if VerifySignature("", FixtureIssuesOpened, header) {
		t.Fatal("expected verification to fail closed when no secret is configured")
	}
}

func TestResolveSecret(t *testing.T) {
	const envVar = "FIREPIT_GH_SECRET_TEST_RESOLVE"

	t.Run("unset", func(t *testing.T) {
		os.Unsetenv(envVar)
		secret, ok := ResolveSecret(envVar)
		if ok || secret != "" {
			t.Fatalf("expected an unset env var to resolve to (\"\", false), got (%q, %v)", secret, ok)
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv(envVar, "the-actual-secret")
		secret, ok := ResolveSecret(envVar)
		if !ok || secret != "the-actual-secret" {
			t.Fatalf("expected (%q, true), got (%q, %v)", "the-actual-secret", secret, ok)
		}
	})
}

func TestPeekRepoFullName(t *testing.T) {
	repo, err := peekRepoFullName(FixtureIssuesOpened)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != "catalystcommunity/firepit" {
		t.Fatalf("repo = %q, want catalystcommunity/firepit", repo)
	}
}

func TestPeekRepoFullNameMalformed(t *testing.T) {
	if _, err := peekRepoFullName([]byte("not json")); err == nil {
		t.Fatal("expected an error unmarshaling malformed JSON")
	}
}
