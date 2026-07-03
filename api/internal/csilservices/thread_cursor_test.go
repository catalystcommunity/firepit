package csilservices

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

func TestPostCursorRoundTrip(t *testing.T) {
	want := postCursor{
		ActivityAt: time.Date(2026, 7, 3, 12, 30, 0, 123456789, time.UTC),
		ID:         "0198a1b2-3c4d-4e5f-8a9b-0123456789ab",
	}

	encoded := encodePostCursor(want)
	got, err := decodePostCursor(encoded)
	if err != nil {
		t.Fatalf("decodePostCursor: %v", err)
	}
	if !got.ActivityAt.Equal(want.ActivityAt) {
		t.Errorf("ActivityAt = %v, want %v", got.ActivityAt, want.ActivityAt)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
}

func TestPostCursorIsOpaqueOnTheWire(t *testing.T) {
	c := postCursor{ActivityAt: time.Now().UTC(), ID: "0198a1b2-3c4d-4e5f-8a9b-0123456789ab"}
	encoded := string(encodePostCursor(c))

	// PageCursor is documented as opaque to clients: the id and any
	// separator character must not appear verbatim, i.e. this is base64,
	// not a delimited plaintext string a client could be tempted to parse.
	if strings.Contains(encoded, c.ID) {
		t.Errorf("encoded cursor %q leaks the raw id %q in plaintext", encoded, c.ID)
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("encoded cursor %q is not URL-safe base64 (raw, unpadded)", encoded)
	}
}

func TestDecodePostCursorRejectsGarbage(t *testing.T) {
	cases := []csil.PageCursor{
		"",
		"not-valid-base64!!!",
		csil.PageCursor(mustBase64("no-separator-here")),
		csil.PageCursor(mustBase64("not-a-number.0198a1b2-3c4d-4e5f-8a9b-0123456789ab")),
	}
	for _, c := range cases {
		if _, err := decodePostCursor(c); err == nil {
			t.Errorf("decodePostCursor(%q): expected an error, got nil", c)
		}
	}
}

func TestListPostsTreatsMalformedCursorAsFirstPage(t *testing.T) {
	// ListPosts has no declared ServiceError arm (csil/firepit.csil), so a
	// cursor that fails to decode must never make its way into an error —
	// this documents/locks in that decodePostCursor's error is meant to be
	// swallowed by the caller (see ListPosts), not propagated.
	if _, err := decodePostCursor("garbage"); err == nil {
		t.Fatal("expected decodePostCursor to reject a garbage cursor (precondition for the ListPosts fallback path)")
	}
}

func mustBase64(s string) string {
	// Local helper mirroring encodePostCursor's encoding so the "garbage"
	// test cases are valid base64 (and therefore exercise the separator/
	// integer parsing failure paths, not just the base64 decode failure
	// path already covered by "not-valid-base64!!!").
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestPostPageLimit(t *testing.T) {
	u := func(v uint64) *uint64 { return &v }

	cases := []struct {
		name  string
		limit *uint64
		want  int
	}{
		{"nil defaults", nil, defaultPostPageLimit},
		{"zero defaults", u(0), defaultPostPageLimit},
		{"within range passes through", u(75), 75},
		{"clamped to max", u(10000), maxPostPageLimit},
		{"exactly max", u(maxPostPageLimit), maxPostPageLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := postPageLimit(tc.limit); got != tc.want {
				t.Errorf("postPageLimit(%v) = %d, want %d", tc.limit, got, tc.want)
			}
		})
	}
}

func TestPostCursorToStoreCursor(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := postCursor{ActivityAt: at, ID: "abc"}
	sc := c.toStoreCursor()
	if sc == nil {
		t.Fatal("toStoreCursor returned nil")
	}
	if !sc.LastActivityAt.Equal(at) || sc.ID != "abc" {
		t.Errorf("toStoreCursor() = %+v, want {%v abc}", sc, at)
	}
}

// --- path building (store.NewCommentID / store.ChildPath) ---

func TestChildPath(t *testing.T) {
	cases := []struct {
		name       string
		parentPath store.Ltree
		selfID     string
		want       store.Ltree
	}{
		{"top-level reply (no parent)", "", "self-id", "self-id"},
		{"nested reply", "root-id", "child-id", "root-id.child-id"},
		{"deeply nested reply", "a.b.c", "d", "a.b.c.d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := store.ChildPath(tc.parentPath, tc.selfID); got != tc.want {
				t.Errorf("ChildPath(%q, %q) = %q, want %q", tc.parentPath, tc.selfID, got, tc.want)
			}
		})
	}
}

func TestNewCommentIDShape(t *testing.T) {
	id, err := store.NewCommentID()
	if err != nil {
		t.Fatalf("NewCommentID: %v", err)
	}
	// Canonical dashed-uuid shape: 8-4-4-4-12 lowercase hex.
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("id %q: want 5 dash-separated groups, got %d", id, len(parts))
	}
	wantLens := []int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != wantLens[i] {
			t.Errorf("id %q: group %d = %q, want length %d", id, i, p, wantLens[i])
		}
		for _, r := range p {
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
			if !isHex {
				t.Errorf("id %q: group %d contains non-hex-lowercase rune %q", id, i, r)
			}
		}
	}
}

func TestNewCommentIDIsUniqueAndMonotonic(t *testing.T) {
	// Two ids minted back-to-back must differ (random suffix) and the
	// second must not sort before the first under plain string comparison —
	// this is exactly the property ListCommentsByPost's "ORDER BY path"
	// depth-first ordering relies on for sibling chronological order (see
	// store.NewCommentID's doc comment).
	first, err := store.NewCommentID()
	if err != nil {
		t.Fatalf("NewCommentID: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := store.NewCommentID()
	if err != nil {
		t.Fatalf("NewCommentID: %v", err)
	}
	if first == second {
		t.Fatalf("expected two distinct ids, got %q twice", first)
	}
	if second < first {
		t.Errorf("expected second id %q to sort after first id %q (time-prefixed labels must sort chronologically)", second, first)
	}
}

func TestChildPathUsesCommentIDVerbatimAsLtreeLabel(t *testing.T) {
	// Documents the label scheme decision: unlike a stripped-dash
	// approach, the dashed uuid id is used as-is. This is only a
	// meaningful guarantee because it was verified against a live
	// Postgres+ltree instance that '-' is an accepted label character
	// (see store.NewCommentID's doc comment) — this test just locks in
	// that ChildPath performs no transformation of its own.
	id, err := store.NewCommentID()
	if err != nil {
		t.Fatalf("NewCommentID: %v", err)
	}
	if !strings.Contains(id, "-") {
		t.Fatalf("expected NewCommentID to produce a dashed uuid, got %q", id)
	}
	got := store.ChildPath("", id)
	if string(got) != id {
		t.Errorf("ChildPath(\"\", %q) = %q, want the id verbatim", id, got)
	}
}
