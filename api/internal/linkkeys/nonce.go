package linkkeys

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"
)

// Login nonces are self-verifying: they carry their own expiry and an HMAC,
// so the server keeps NO server-side state between AuthService.begin-login
// and GET /auth/callback. We mint a nonce, the RP binds it into the signed
// request, the IDP echoes it back inside the assertion, and the callback
// re-checks the HMAC + expiry.
//
// This proves "firepit issued this login recently" without a database row or
// in-memory map — ported from longhouse's api/internal/auth/nonce.go
// (PLANDOC.md §2's "port near-verbatim" instruction), relocated to this
// package because firepit's begin-login (a CSIL-RPC op with no response-header
// channel — see csilservices/auth.go's package doc) can't set the literal
// "nonce cookie" PLANDOC.md §3 describes; a self-verifying nonce achieves the
// same "prove this login round-trip is ours" property without one, and is
// what B2's ownership scope (users/sessions tables only, no new nonce table)
// implies anyway. It does NOT enforce single-use within the window; that
// protection leans on the assertion's own short expiry and linkkeys'
// single-use of the sealed encrypted_token.
const NonceTTL = 10 * time.Minute

const (
	nonceRandLen = 16
	nonceExpLen  = 8 // unix seconds, big-endian
	nonceMACLen  = sha256.Size
	nonceRawLen  = nonceRandLen + nonceExpLen + nonceMACLen
)

var (
	ErrNonceInvalid = errors.New("linkkeys: login nonce is invalid")
	ErrNonceExpired = errors.New("linkkeys: login nonce has expired")
)

// MintNonce returns a fresh self-verifying nonce valid for NonceTTL.
func MintNonce(secret []byte) string {
	body := make([]byte, nonceRandLen+nonceExpLen)
	_, _ = rand.Read(body[:nonceRandLen])
	exp := time.Now().UTC().Add(NonceTTL).Unix()
	binary.BigEndian.PutUint64(body[nonceRandLen:], uint64(exp))
	mac := nonceMAC(secret, body)
	return base64.RawURLEncoding.EncodeToString(append(body, mac...))
}

// VerifyNonce checks a nonce's HMAC and expiry. Returns nil when the nonce
// is one we minted and is still fresh.
func VerifyNonce(secret []byte, nonce string) error {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != nonceRawLen {
		return ErrNonceInvalid
	}
	body, mac := raw[:nonceRandLen+nonceExpLen], raw[nonceRandLen+nonceExpLen:]
	if !hmac.Equal(nonceMAC(secret, body), mac) {
		return ErrNonceInvalid
	}
	exp := int64(binary.BigEndian.Uint64(body[nonceRandLen:]))
	if time.Now().UTC().Unix() >= exp {
		return ErrNonceExpired
	}
	return nil
}

func nonceMAC(secret, body []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("firepit-login-nonce\x00")) // domain separation from any other HMAC use of the secret
	m.Write(body)
	return m.Sum(nil)
}
