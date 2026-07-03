package linkkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/catalystcommunity/firepit/api/internal/transport"
)

func TestParseLinkkeysTXT(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		txt  string
		want map[string]string
	}{
		{"apis", "v=lk1 tcp=idp.example:4987 https=idp.example/api", map[string]string{"v": "lk1", "tcp": "idp.example:4987", "https": "idp.example/api"}},
		{"no-port", "v=lk1 tcp=idp.example", map[string]string{"v": "lk1", "tcp": "idp.example"}},
		{"repeated-key-keeps-first", "v=lk1 tcp=a tcp=b", map[string]string{"v": "lk1", "tcp": "a"}},
		{"not-lk1", "v=spf1 include:_spf.google.com", nil},
		{"junk", "totally unrelated text", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLinkkeysTXT(tc.txt); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseLinkkeysTXT(%q) = %v, want %v", tc.txt, got, tc.want)
			}
		})
	}
}

func TestEnsurePort(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"idp.example":      "idp.example:4987",
		"idp.example:9000": "idp.example:9000",
		"127.0.0.1":        "127.0.0.1:4987",
	}
	for in, want := range cases {
		if got := ensurePort(in, DefaultTCPPort); got != want {
			t.Errorf("ensurePort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSPKIFingerprint asserts our fingerprint matches linkkeys' scheme:
// lowercase hex of SHA-256 over the raw Ed25519 public key bytes (which for
// Ed25519 equal the SubjectPublicKey bit-string contents).
func TestSPKIFingerprint(t *testing.T) {
	t.Parallel()
	pub, _, cert := newTestCert(t)
	want := sha256.Sum256(pub)
	got, err := spkiFingerprint(cert)
	if err != nil {
		t.Fatalf("spkiFingerprint: %v", err)
	}
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("spkiFingerprint = %s, want %s", got, hex.EncodeToString(want[:]))
	}
}

func TestTCPClient_SignRequest_RoundTrip(t *testing.T) {
	t.Parallel()
	recvd := make(chan *transport.RpcRequest, 1)
	_, tlsCert, cert := newTestCert(t)
	handler := func(req *transport.RpcRequest) transport.HandlerOutcome {
		recvd <- req
		if req.Service != rpService || req.Op != opSignRequest {
			return transport.Transport(transport.StatusUnknownServiceOrOp, "bad route")
		}
		if req.Auth == nil || *req.Auth != "secret-key" {
			return transport.Transport(transport.StatusUnauthenticated, "bad auth")
		}
		var in struct {
			CallbackURL string `cbor:"callback_url"`
			Nonce       string `cbor:"nonce"`
		}
		if err := cbor.Unmarshal(req.Payload, &in); err != nil {
			return transport.Transport(transport.StatusInternal, err.Error())
		}
		b, _ := cbor.Marshal(map[string]string{"signed_request": "signed:" + in.CallbackURL + ":" + in.Nonce})
		return transport.Reply("RpSignResponse", b)
	}
	addr := startTestRPCServer(t, tlsCert, handler)

	// Pin against the server cert's real fingerprint to exercise VerifyConnection.
	fp, err := spkiFingerprint(cert)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewTCPClient(TCPConfig{Addr: addr, Fingerprints: []string{fp}, APIKey: "secret-key"})
	if err != nil {
		t.Fatalf("NewTCPClient: %v", err)
	}

	got, err := c.SignRequest("https://app/callback", "nonce-123")
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	if want := "signed:https://app/callback:nonce-123"; got != want {
		t.Fatalf("SignRequest = %q, want %q", got, want)
	}
	req := <-recvd
	if req.Service != "Rp" || req.Op != "sign-request" {
		t.Fatalf("envelope routed to %s/%s, want Rp/sign-request", req.Service, req.Op)
	}
}

func TestTCPClient_VerifyAssertion_RoundTrip(t *testing.T) {
	t.Parallel()
	_, tlsCert, _ := newTestCert(t)
	handler := func(req *transport.RpcRequest) transport.HandlerOutcome {
		var in struct {
			SignedAssertion string `cbor:"signed_assertion"`
			ExpectedDomain  string `cbor:"expected_domain"`
		}
		_ = cbor.Unmarshal(req.Payload, &in)
		b, _ := cbor.Marshal(map[string]any{
			"verified": true,
			"assertion": map[string]any{
				"user_id":           "u1",
				"domain":            in.ExpectedDomain,
				"audience":          "rp.example",
				"nonce":             "n1",
				"issued_at":         "2026-01-01T00:00:00Z",
				"expires_at":        "2026-01-01T00:10:00Z",
				"authorized_claims": []string{"email"}, // unknown to Assertion; must be ignored
				"display_name":      "Ada",
			},
		})
		return transport.Reply("RpVerifyResponse", b)
	}
	addr := startTestRPCServer(t, tlsCert, handler)
	c, err := NewTCPClient(TCPConfig{Addr: addr, Insecure: true, APIKey: "k"})
	if err != nil {
		t.Fatalf("NewTCPClient: %v", err)
	}
	a, err := c.VerifyAssertion("signed-blob", "idp.example")
	if err != nil {
		t.Fatalf("VerifyAssertion: %v", err)
	}
	if a.UserID != "u1" || a.Domain != "idp.example" || a.DisplayName != "Ada" {
		t.Fatalf("assertion decoded wrong: %+v", a)
	}
}

func TestTCPClient_VerifyAssertion_RejectedWhenUnverified(t *testing.T) {
	t.Parallel()
	_, tlsCert, _ := newTestCert(t)
	handler := func(req *transport.RpcRequest) transport.HandlerOutcome {
		b, _ := cbor.Marshal(map[string]any{"verified": false, "assertion": map[string]any{}})
		return transport.Reply("RpVerifyResponse", b)
	}
	addr := startTestRPCServer(t, tlsCert, handler)
	c, _ := NewTCPClient(TCPConfig{Addr: addr, Insecure: true})
	if _, err := c.VerifyAssertion("x", "y"); err == nil {
		t.Fatal("expected error when verified=false, got nil")
	}
}

func TestTCPClient_FetchUserInfo_RoundTrip(t *testing.T) {
	t.Parallel()
	_, tlsCert, _ := newTestCert(t)
	handler := func(req *transport.RpcRequest) transport.HandlerOutcome {
		if req.Op != opUserInfo {
			return transport.Transport(transport.StatusUnknownServiceOrOp, "bad op")
		}
		var in struct {
			Token   string `cbor:"token"`
			APIBase string `cbor:"api_base"`
			Domain  string `cbor:"domain"`
		}
		_ = cbor.Unmarshal(req.Payload, &in)
		if in.APIBase != "https://idp.example" || in.Domain != "idp.example" {
			return transport.Transport(transport.StatusInternal, "unexpected request fields")
		}
		b, _ := cbor.Marshal(map[string]any{
			"user_id":      "u1",
			"domain":       "idp.example",
			"display_name": "Ada Lovelace",
			"claims": []map[string]any{
				{"claim_id": "c1", "claim_type": "email", "claim_value": []byte("ada@example.com")},
			},
		})
		return transport.Reply("UserInfo", b)
	}
	addr := startTestRPCServer(t, tlsCert, handler)
	c, _ := NewTCPClient(TCPConfig{Addr: addr, Insecure: true, APIBase: "https://idp.example", Domain: "idp.example"})

	info, err := c.FetchUserInfo("tok", "") // empty domain falls back to client domain
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.DisplayName != "Ada Lovelace" {
		t.Fatalf("display name = %q", info.DisplayName)
	}
	if len(info.Claims) != 1 || info.Claims[0].ClaimType != "email" || string(info.Claims[0].ClaimValue) != "ada@example.com" {
		t.Fatalf("claims decoded wrong: %+v", info.Claims)
	}
}

func TestTCPClient_FingerprintMismatchFails(t *testing.T) {
	t.Parallel()
	_, tlsCert, _ := newTestCert(t)
	handler := func(*transport.RpcRequest) transport.HandlerOutcome {
		b, _ := cbor.Marshal(map[string]string{"signed_request": "x"})
		return transport.Reply("RpSignResponse", b)
	}
	addr := startTestRPCServer(t, tlsCert, handler)
	// Pin a bogus fingerprint; the TLS handshake must fail.
	c, _ := NewTCPClient(TCPConfig{Addr: addr, Fingerprints: []string{"00" + hex.EncodeToString(make([]byte, 31))}})
	if _, err := c.SignRequest("cb", "n"); err == nil {
		t.Fatal("expected handshake failure on fingerprint mismatch, got nil")
	}
}

// --- test helpers ---

// newTestCert returns a self-signed Ed25519 leaf and parsed cert for pinning.
func newTestCert(t *testing.T) (ed25519.PublicKey, tls.Certificate, *x509.Certificate) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "linkkeys-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost", "127.0.0.1"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pub, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, cert
}

// startTestRPCServer runs a TLS listener that serves CSIL-RPC frames via the
// same transport package the real linkkeys server uses, dispatching each
// request through handler. Returns the dial address.
func startTestRPCServer(t *testing.T, tlsCert tls.Certificate, handler func(*transport.RpcRequest) transport.HandlerOutcome) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{tlsCert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				srv := transport.NewRpcServer(transport.NewStreamCarrier(conn))
				for {
					served, err := srv.ServeOne(handler)
					if err != nil || !served {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}
