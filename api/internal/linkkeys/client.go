// Package linkkeys is a thin client for the linkkeys RP sidecar firepit runs
// alongside the api (PLANDOC.md §2, §3: "we run a linkkeys server in RP mode
// ... as a sidecar"). The api never touches private keys; the sidecar holds
// them and exposes sign-request / decrypt-token / verify-assertion /
// userinfo-fetch over one of two transports:
//
//   - client.go (this file): the legacy JSON-over-HTTP PKI shim.
//   - tcp.go: canonical CSIL-RPC over TCP+TLS, with server-cert fingerprint
//     pinning instead of CA trust (linkkeys derives its cert from a domain
//     key and publishes fingerprints over DNS).
//
// Ported near-verbatim from longhouse's api/internal/linkkeys (PLANDOC.md §2:
// "the api/internal/linkkeys/ RP client package (port near-verbatim)") —
// same wire shapes and behavior, adapted only to firepit's own config/log
// wiring (api/internal/config, sirupsen/logrus) and its own vendored
// api/internal/transport package (tcp.go's TCP transport).
package linkkeys

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the HTTP (JSON) transport to the linkkeys RP PKI sidecar.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// Assertion mirrors the relevant fields of linkkeys' IdentityAssertion. The
// json tags carry the HTTP RP shim's responses; the cbor tags carry the
// CSIL-RPC/TCP transport's (see tcp.go). Both use the same snake_case wire
// names, and unknown fields (e.g. authorized_claims) are ignored on decode.
type Assertion struct {
	UserID      string `json:"user_id" cbor:"user_id"`
	Domain      string `json:"domain" cbor:"domain"`
	Audience    string `json:"audience" cbor:"audience"`
	Nonce       string `json:"nonce" cbor:"nonce"`
	IssuedAt    string `json:"issued_at" cbor:"issued_at"`
	ExpiresAt   string `json:"expires_at" cbor:"expires_at"`
	DisplayName string `json:"display_name,omitempty" cbor:"display_name,omitempty"`
	// AuthorizedClaims is the set of claim_types the user's consent + the IDP's
	// policy released to us for this redemption — the negotiated grant. The
	// claim VALUES come from FetchUserInfo; this is just which were allowed.
	// Only the CSIL-RPC transport populates it (the HTTP shim omits it).
	AuthorizedClaims []string `json:"authorized_claims,omitempty" cbor:"authorized_claims,omitempty"`
}

// New builds a Client. allowInvalidCerts skips TLS verification — only for
// dev clusters with self-signed RP certs.
func New(baseURL, apiKey string, allowInvalidCerts bool) *Client {
	tr := &http.Transport{}
	if allowInvalidCerts {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // dev-only opt-in
	}
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Transport: tr, Timeout: 15 * time.Second},
	}
}

// SignRequest asks the RP to sign an AuthRequest bound to callbackURL +
// nonce. The returned signed_request is base64url and gets appended to the
// IDP authorize redirect.
func (c *Client) SignRequest(callbackURL, nonce string) (string, error) {
	var out struct {
		SignedRequest string `json:"signed_request"`
	}
	if err := c.post("/v1alpha/sign-request.json",
		map[string]string{"callback_url": callbackURL, "nonce": nonce},
		&out); err != nil {
		return "", err
	}
	return out.SignedRequest, nil
}

// DecryptToken decrypts the encrypted_token the IDP returns to the callback.
// The result is still only signed — callers must then VerifyAssertion it.
func (c *Client) DecryptToken(encryptedToken string) (string, error) {
	var out struct {
		SignedAssertion string `json:"signed_assertion"`
	}
	if err := c.post("/v1alpha/decrypt-token.json",
		map[string]string{"encrypted_token": encryptedToken},
		&out); err != nil {
		return "", err
	}
	return out.SignedAssertion, nil
}

// VerifyAssertion verifies a signed assertion against expectedDomain's
// published linkkeys keys (fetched via DNS by the sidecar). Returns the
// inner assertion fields when the signature checks out.
func (c *Client) VerifyAssertion(signedAssertion, expectedDomain string) (*Assertion, error) {
	var out struct {
		Assertion Assertion `json:"assertion"`
		Verified  bool      `json:"verified"`
	}
	if err := c.post("/v1alpha/verify-assertion.json",
		map[string]string{"signed_assertion": signedAssertion, "expected_domain": expectedDomain},
		&out); err != nil {
		return nil, err
	}
	if !out.Verified {
		return nil, fmt.Errorf("linkkeys: assertion rejected by RP")
	}
	return &out.Assertion, nil
}

func (c *Client) post(path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("do %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("linkkeys %s: %s: %s", path, resp.Status, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
