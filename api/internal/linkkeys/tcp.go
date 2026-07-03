// CSIL-RPC/TCP transport for the linkkeys RP service.
//
// This is the non-browser path: instead of POSTing JSON to an HTTP RP PKI
// sidecar (see client.go), we dial linkkeys' canonical CSIL-RPC endpoint over
// TCP+TLS and speak the same envelope the SPA already uses. It implements the
// same csilservices.PKIClient surface (SignRequest/DecryptToken/
// VerifyAssertion) so the auth service is transport-agnostic, plus the
// userinfo-fetch op (FetchUserInfo) the HTTP shim never exposed.
//
// Wire compatibility: the envelope is encoded by api/internal/transport (this
// module's own vendored csilgen transport — a separate copy from longhouse's,
// but implementing the identical csil-rpc-transport.md conventions), which
// wraps the request payload in CBOR tag 24 exactly as linkkeys' server-side
// csilgen-transport expects. The inner request/response bodies are plain CBOR
// maps with snake_case keys matching the linkkeys CSIL types, encoded here via
// github.com/fxamacker/cbor/v2 (the payload codec; api/internal/transport only
// owns the envelope, not arbitrary struct marshaling).
package linkkeys

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/firepit/api/internal/transport"
)

// DefaultTCPPort is linkkeys' default CSIL-RPC/TCP port (liblinkkeys::dns::
// DEFAULT_TCP_PORT). Used when a discovered or configured endpoint omits one.
const DefaultTCPPort = "4987"

const (
	rpService     = "Rp"
	opSignRequest = "sign-request"
	opDecryptTok  = "decrypt-token"
	opVerifyAsrt  = "verify-assertion"
	opUserInfo    = "userinfo-fetch"
)

// TCPConfig configures a TCPClient. Addr and Fingerprints are optional: when
// empty they are discovered from DNS off Domain (the IDP identity domain) via
// the _linkkeys_apis / _linkkeys TXT records linkkeys publishes.
type TCPConfig struct {
	Domain       string   // IDP/user identity domain (audience for userinfo, DNS discovery root)
	APIBase      string   // IDP https API base (userinfo-fetch api_base; self-call detection)
	APIKey       string   // RP API key, presented in the envelope auth field
	Addr         string   // optional host:port override (else DNS-discovered)
	Fingerprints []string // optional pinned SPKI sha256 hex (else DNS-discovered)
	Insecure     bool     // dev only: skip server-cert fingerprint pinning
	DialTimeout  time.Duration
	IOTimeout    time.Duration
}

// TCPClient speaks CSIL-RPC to linkkeys' Rp service over TCP+TLS. It is safe
// for concurrent use: each call opens, uses, and closes its own connection
// (logins are infrequent, so a per-call dial keeps the client stateless and
// avoids managing idle/half-open pooled sockets).
type TCPClient struct {
	addr         string
	serverName   string
	fingerprints []string
	apiKey       string
	insecure     bool
	apiBase      string
	domain       string
	dialTimeout  time.Duration
	ioTimeout    time.Duration
}

// UserInfo is linkkeys' Rp.userinfo-fetch / Identity UserInfo response: the
// identity plus the claims released for this RP. Richer than an assertion,
// which only carries a display name.
type UserInfo struct {
	UserID      string  `cbor:"user_id"`
	Domain      string  `cbor:"domain"`
	DisplayName string  `cbor:"display_name"`
	Claims      []Claim `cbor:"claims"`
}

// Claim is one released claim. We decode the addressable fields; signatures and
// other metadata on the wire are ignored.
type Claim struct {
	ClaimID    string `cbor:"claim_id"`
	ClaimType  string `cbor:"claim_type"`
	ClaimValue []byte `cbor:"claim_value"`
}

// ClaimValues flattens the released claims into a claim_type → value map. Claim
// values are opaque bytes on the wire; the well-known identity claims
// (display_name, email, avatar_url, handle, website, ...) are UTF-8 text, so we
// expose them as strings. Returns nil when no claims were released. The linkkeys
// server already scopes Claims to the authorized set, so every entry here is one
// the user/policy actually granted.
func (u *UserInfo) ClaimValues() map[string]string {
	if u == nil || len(u.Claims) == 0 {
		return nil
	}
	out := make(map[string]string, len(u.Claims))
	for _, c := range u.Claims {
		if c.ClaimType == "" {
			continue
		}
		out[c.ClaimType] = string(c.ClaimValue)
	}
	return out
}

// NewTCPClient builds a TCPClient, resolving the endpoint and pin set from DNS
// when not supplied explicitly. DNS discovery is skipped entirely when both
// Addr and Fingerprints are provided (or Insecure is set), which is how dev
// clusters with self-signed certs and no published records still connect.
func NewTCPClient(cfg TCPConfig) (*TCPClient, error) {
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	ioTimeout := cfg.IOTimeout
	if ioTimeout <= 0 {
		ioTimeout = 15 * time.Second
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		if cfg.Domain == "" {
			return nil, fmt.Errorf("linkkeys tcp: no addr and no domain to discover from")
		}
		discovered, err := discoverEndpoint(cfg.Domain)
		if err != nil {
			return nil, fmt.Errorf("linkkeys tcp: endpoint discovery for %q: %w", cfg.Domain, err)
		}
		addr = discovered
	}
	addr = ensurePort(addr, DefaultTCPPort)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("linkkeys tcp: bad addr %q: %w", addr, err)
	}

	fingerprints := normalizeFingerprints(cfg.Fingerprints)
	if !cfg.Insecure && len(fingerprints) == 0 {
		if cfg.Domain == "" {
			return nil, fmt.Errorf("linkkeys tcp: no fingerprints and no domain to discover from (set FIREPIT_LINKKEYS_TCP_FINGERPRINTS or FIREPIT_LINKKEYS_TCP_ALLOW_INSECURE)")
		}
		discovered, err := discoverFingerprints(cfg.Domain)
		if err != nil {
			return nil, fmt.Errorf("linkkeys tcp: fingerprint discovery for %q: %w", cfg.Domain, err)
		}
		fingerprints = discovered
	}

	return &TCPClient{
		addr:         addr,
		serverName:   host,
		fingerprints: fingerprints,
		apiKey:       cfg.APIKey,
		insecure:     cfg.Insecure,
		apiBase:      cfg.APIBase,
		domain:       cfg.Domain,
		dialTimeout:  dialTimeout,
		ioTimeout:    ioTimeout,
	}, nil
}

// SignRequest asks the RP to sign an AuthRequest bound to callbackURL + nonce.
func (c *TCPClient) SignRequest(callbackURL, nonce string) (string, error) {
	var out struct {
		SignedRequest string `cbor:"signed_request"`
	}
	in := struct {
		CallbackURL string `cbor:"callback_url"`
		Nonce       string `cbor:"nonce"`
	}{callbackURL, nonce}
	if err := c.call(opSignRequest, in, &out); err != nil {
		return "", err
	}
	return out.SignedRequest, nil
}

// DecryptToken decrypts the encrypted_token the IDP returns to the callback.
// The result is signed but not yet verified — callers VerifyAssertion it next.
func (c *TCPClient) DecryptToken(encryptedToken string) (string, error) {
	var out struct {
		SignedAssertion string `cbor:"signed_assertion"`
	}
	in := struct {
		EncryptedToken string `cbor:"encrypted_token"`
	}{encryptedToken}
	if err := c.call(opDecryptTok, in, &out); err != nil {
		return "", err
	}
	return out.SignedAssertion, nil
}

// VerifyAssertion verifies a signed assertion against expectedDomain's keys and
// returns the inner assertion fields when the signature checks out.
func (c *TCPClient) VerifyAssertion(signedAssertion, expectedDomain string) (*Assertion, error) {
	var out struct {
		Assertion Assertion `cbor:"assertion"`
		Verified  bool      `cbor:"verified"`
	}
	in := struct {
		SignedAssertion string `cbor:"signed_assertion"`
		ExpectedDomain  string `cbor:"expected_domain"`
	}{signedAssertion, expectedDomain}
	if err := c.call(opVerifyAsrt, in, &out); err != nil {
		return nil, err
	}
	if !out.Verified {
		return nil, fmt.Errorf("linkkeys: assertion rejected by RP")
	}
	return &out.Assertion, nil
}

// FetchUserInfo redeems a signed assertion token for the full UserInfo (display
// name + released claims). domain is the user's identity domain (the assertion
// domain); it lets linkkeys resolve the home IDP's tcp endpoint. The RP handles
// the mTLS + proof-of-possession to the IDP internally — we only present our
// API key, so this works for an RP that holds no domain key of its own.
func (c *TCPClient) FetchUserInfo(token, domain string) (*UserInfo, error) {
	if domain == "" {
		domain = c.domain
	}
	var out UserInfo
	in := struct {
		Token   string `cbor:"token"`
		APIBase string `cbor:"api_base"`
		Domain  string `cbor:"domain"`
	}{token, c.apiBase, domain}
	if err := c.call(opUserInfo, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// call marshals reqBody, runs one Rp/<op> round-trip over a fresh TLS
// connection, and unmarshals the typed reply into respBody.
func (c *TCPClient) call(op string, reqBody, respBody any) error {
	payload, err := cbor.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("linkkeys tcp: encode %s/%s: %w", rpService, op, err)
	}

	conn, err := net.DialTimeout("tcp", c.addr, c.dialTimeout)
	if err != nil {
		return fmt.Errorf("linkkeys tcp: dial %s: %w", c.addr, err)
	}
	defer conn.Close()
	if c.ioTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.ioTimeout))
	}

	tlsConn := tls.Client(conn, c.tlsConfig())
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("linkkeys tcp: tls handshake with %s: %w", c.addr, err)
	}

	client := transport.NewRpcClient(transport.NewStreamCarrier(tlsConn), false)
	var auth *string
	if c.apiKey != "" {
		auth = &c.apiKey
	}
	resp, err := client.Call(rpService, op, payload, auth)
	if err != nil {
		return fmt.Errorf("linkkeys tcp: %s/%s: %w", rpService, op, err)
	}
	if respBody != nil {
		if err := cbor.Unmarshal(resp.Payload, respBody); err != nil {
			return fmt.Errorf("linkkeys tcp: decode %s/%s reply: %w", rpService, op, err)
		}
	}
	return nil
}

// tlsConfig pins the server cert by SPKI fingerprint rather than trusting a CA
// chain (linkkeys derives its cert from a domain key and publishes the
// fingerprints over DNS). InsecureSkipVerify disables Go's chain validation;
// our VerifyConnection hook re-imposes the pin. In dev (Insecure) we skip the
// pin too and accept any cert.
func (c *TCPClient) tlsConfig() *tls.Config {
	cfg := &tls.Config{
		ServerName:         c.serverName,
		InsecureSkipVerify: true, //nolint:gosec // pin enforced in VerifyConnection below
	}
	if c.insecure {
		return cfg
	}
	pins := c.fingerprints
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("linkkeys tcp: server presented no certificate")
		}
		got, err := spkiFingerprint(cs.PeerCertificates[0])
		if err != nil {
			return fmt.Errorf("linkkeys tcp: fingerprint server cert: %w", err)
		}
		for _, want := range pins {
			if got == want {
				return nil
			}
		}
		return fmt.Errorf("linkkeys tcp: server cert fingerprint %s matches no pinned fingerprint", got)
	}
	return cfg
}

// spkiFingerprint computes linkkeys' cert fingerprint: lowercase hex of
// SHA-256 over the SubjectPublicKey bit-string bytes (the raw public key, not
// the full SubjectPublicKeyInfo) — matching liblinkkeys::crypto::fingerprint.
func spkiFingerprint(cert *x509.Certificate) (string, error) {
	var spki struct {
		Algorithm asn1.RawValue
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(cert.RawSubjectPublicKeyInfo, &spki); err != nil {
		return "", err
	}
	sum := sha256.Sum256(spki.PublicKey.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

// discoverEndpoint reads _linkkeys_apis.<domain> TXT and returns the tcp=
// host[:port] linkkeys advertises for CSIL-RPC clients.
func discoverEndpoint(domain string) (string, error) {
	txts, err := net.LookupTXT("_linkkeys_apis." + domain)
	if err != nil {
		return "", err
	}
	for _, txt := range txts {
		fields := parseLinkkeysTXT(txt)
		if fields == nil {
			continue
		}
		if tcp := fields["tcp"]; tcp != "" {
			return tcp, nil
		}
	}
	return "", fmt.Errorf("no tcp= endpoint in _linkkeys_apis.%s TXT", domain)
}

// discoverFingerprints reads _linkkeys.<domain> TXT and returns the published
// fp= server-cert fingerprints (the trust anchor).
func discoverFingerprints(domain string) ([]string, error) {
	txts, err := net.LookupTXT("_linkkeys." + domain)
	if err != nil {
		return nil, err
	}
	for _, txt := range txts {
		if !strings.Contains(txt, "v=lk1") {
			continue
		}
		var fps []string
		for _, tok := range strings.Fields(txt) {
			if v, ok := strings.CutPrefix(tok, "fp="); ok && v != "" {
				fps = append(fps, strings.ToLower(v))
			}
		}
		if len(fps) > 0 {
			return fps, nil
		}
	}
	return nil, fmt.Errorf("no fp= fingerprints in _linkkeys.%s TXT", domain)
}

// parseLinkkeysTXT parses a `v=lk1 k=v k=v` linkkeys TXT record into a map,
// returning nil for records that aren't lk1. Repeated keys keep the first.
func parseLinkkeysTXT(txt string) map[string]string {
	if !strings.Contains(txt, "v=lk1") {
		return nil
	}
	out := map[string]string{}
	for _, tok := range strings.Fields(txt) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// ensurePort appends :defaultPort to a host that lacks one.
func ensurePort(addr, defaultPort string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, defaultPort)
}

func normalizeFingerprints(in []string) []string {
	var out []string
	for _, fp := range in {
		fp = strings.ToLower(strings.TrimSpace(fp))
		if fp != "" {
			out = append(out, fp)
		}
	}
	return out
}
