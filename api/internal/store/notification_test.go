package store

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestNotificationCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 7, 3, 12, 34, 56, 789, time.UTC)
	id := "01HXYZ0000000000000000ABCD"

	token := EncodeNotificationCursor(createdAt, id)
	if token == "" {
		t.Fatal("EncodeNotificationCursor returned empty token")
	}

	got, ok := DecodeNotificationCursor(token)
	if !ok {
		t.Fatalf("DecodeNotificationCursor(%q) ok = false, want true", token)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, createdAt)
	}
	if got.ID != id {
		t.Errorf("ID = %q, want %q", got.ID, id)
	}
}

func TestDecodeNotificationCursorInvalid(t *testing.T) {
	cases := []string{
		"",
		"not-base64!!!",
		base64.RawURLEncoding.EncodeToString([]byte("missing-pipe")),
		base64.RawURLEncoding.EncodeToString([]byte("|")),
		base64.RawURLEncoding.EncodeToString([]byte("not-a-timestamp|01HXYZ")),
	}
	for _, c := range cases {
		if _, ok := DecodeNotificationCursor(c); ok {
			t.Errorf("DecodeNotificationCursor(%q) ok = true, want false", c)
		}
	}
}
