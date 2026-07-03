package linkkeys

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(srv *httptest.Server) *Client {
	return &Client{BaseURL: srv.URL, APIKey: "test", HTTPClient: srv.Client()}
}

func TestVerifyAssertion_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1alpha/verify-assertion.json" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"verified":true,"assertion":{"user_id":"u1","domain":"idp.example","display_name":"Tod"}}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).VerifyAssertion("ASSERT", "idp.example")
	if err != nil {
		t.Fatalf("VerifyAssertion: %v", err)
	}
	if got.UserID != "u1" || got.Domain != "idp.example" || got.DisplayName != "Tod" {
		t.Errorf("got %+v", got)
	}
}

func TestVerifyAssertion_NotVerified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"verified":false,"assertion":{}}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).VerifyAssertion("ASSERT", "idp.example"); err == nil {
		t.Fatal("expected error when verified=false")
	}
}

func TestVerifyAssertion_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("nope"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).VerifyAssertion("ASSERT", "idp.example")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got %v", err)
	}
}

func TestSignRequest_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1alpha/sign-request.json" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"signed_request":"signed-blob"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).SignRequest("https://app/callback", "nonce-1")
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	if got != "signed-blob" {
		t.Errorf("SignRequest = %q, want %q", got, "signed-blob")
	}
}

func TestDecryptToken_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1alpha/decrypt-token.json" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"signed_assertion":"signed-assertion-blob"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).DecryptToken("enc-token")
	if err != nil {
		t.Fatalf("DecryptToken: %v", err)
	}
	if got != "signed-assertion-blob" {
		t.Errorf("DecryptToken = %q, want %q", got, "signed-assertion-blob")
	}
}
