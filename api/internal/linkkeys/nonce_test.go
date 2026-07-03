package linkkeys

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"
)

// mintNonceAt builds a nonce expiring secondsFromNow from now (negative =
// already expired). In-package so it can reuse the real MAC + layout.
func mintNonceAt(secret []byte, secondsFromNow int64) string {
	body := make([]byte, nonceRandLen+nonceExpLen)
	exp := time.Now().UTC().Unix() + secondsFromNow
	binary.BigEndian.PutUint64(body[nonceRandLen:], uint64(exp))
	mac := nonceMAC(secret, body)
	return base64.RawURLEncoding.EncodeToString(append(body, mac...))
}

func TestNonce_RoundTrip(t *testing.T) {
	secret := []byte("nonce-secret")
	n := MintNonce(secret)
	if err := VerifyNonce(secret, n); err != nil {
		t.Fatalf("fresh nonce should verify: %v", err)
	}
}

func TestNonce_WrongSecret(t *testing.T) {
	n := MintNonce([]byte("alice"))
	if err := VerifyNonce([]byte("bob"), n); err != ErrNonceInvalid {
		t.Errorf("got %v, want ErrNonceInvalid", err)
	}
}

func TestNonce_Garbage(t *testing.T) {
	for _, n := range []string{"", "!!!", base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if err := VerifyNonce([]byte("k"), n); err != ErrNonceInvalid {
			t.Errorf("VerifyNonce(%q) = %v, want ErrNonceInvalid", n, err)
		}
	}
}

func TestNonce_Tampered(t *testing.T) {
	secret := []byte("k")
	n := MintNonce(secret)
	raw, _ := base64.RawURLEncoding.DecodeString(n)
	raw[0] ^= 0xff // flip a random byte in the body
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if err := VerifyNonce(secret, tampered); err != ErrNonceInvalid {
		t.Errorf("got %v, want ErrNonceInvalid", err)
	}
}

func TestNonce_Expired(t *testing.T) {
	secret := []byte("k")
	n := mintNonceAt(secret, -1) // expired 1s ago
	if err := VerifyNonce(secret, n); err != ErrNonceExpired {
		t.Errorf("got %v, want ErrNonceExpired", err)
	}
}

func TestNonce_DifferentNoncesAreUnpredictable(t *testing.T) {
	secret := []byte("k")
	a := MintNonce(secret)
	b := MintNonce(secret)
	if a == b {
		t.Fatal("two mints produced the same nonce")
	}
}
