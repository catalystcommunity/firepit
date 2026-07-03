package csilservices

import (
	"context"
	"testing"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// --- slug validation ---

func TestValidateSlug(t *testing.T) {
	cases := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"minimum length", "abc", false},
		{"maximum length", stringOfLen(50, 'a'), false},
		{"simple word", "general", false},
		{"multi-segment", "my-cool-board", false},
		{"digits allowed", "board-42", false},

		{"too short", "ab", true},
		{"too long", stringOfLen(51, 'a'), true},
		{"empty", "", true},
		{"uppercase", "MyBoard", true},
		{"leading hyphen", "-board", true},
		{"trailing hyphen", "board-", true},
		{"double hyphen", "my--board", true},
		{"underscore not allowed", "my_board", true},
		{"space not allowed", "my board", true},
		{"unicode not allowed", "bœard", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSlug(tc.slug)
			if tc.wantErr && err == nil {
				t.Fatalf("validateSlug(%q): expected an error, got nil", tc.slug)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateSlug(%q): expected no error, got %v", tc.slug, err)
			}
			if err != nil {
				if err.Code != CodeValidation {
					t.Errorf("expected CodeValidation, got %d", err.Code)
				}
				if err.Field != "slug" {
					t.Errorf("expected Field=slug, got %q", err.Field)
				}
			}
		})
	}
}

func stringOfLen(n int, c byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestValidateTitle(t *testing.T) {
	if err := validateTitle(""); err == nil {
		t.Error("empty title: expected an error")
	}
	if err := validateTitle(stringOfLen(257, 'x')); err == nil {
		t.Error("257-char title: expected an error")
	}
	if err := validateTitle("General Discussion"); err != nil {
		t.Errorf("valid title: unexpected error %v", err)
	}
}

func TestValidateDescription(t *testing.T) {
	if err := validateDescription(""); err != nil {
		t.Errorf("empty description is allowed: unexpected error %v", err)
	}
	if err := validateDescription(stringOfLen(2001, 'x')); err == nil {
		t.Error("2001-char description: expected an error")
	}
}

func TestValidateBoardKind(t *testing.T) {
	if err := validateBoardKind(csil.BoardKind("discussion")); err != nil {
		t.Errorf("discussion: unexpected error %v", err)
	}
	if err := validateBoardKind(csil.BoardKind("announce")); err != nil {
		t.Errorf("announce: unexpected error %v", err)
	}
	if err := validateBoardKind(csil.BoardKind("")); err == nil {
		t.Error("empty kind: expected an error")
	}
	if err := validateBoardKind(csil.BoardKind("bogus")); err == nil {
		t.Error("bogus kind: expected an error")
	}
}

func TestNormalizeBoardRole(t *testing.T) {
	if role, err := normalizeBoardRole(csil.BoardRole("maintainer")); err != nil || role != store.BoardRoleMaintainer {
		t.Errorf("maintainer: got (%q, %v)", role, err)
	}
	if role, err := normalizeBoardRole(csil.BoardRole("moderator")); err != nil || role != store.BoardRoleModerator {
		t.Errorf("moderator: got (%q, %v)", role, err)
	}
	if _, err := normalizeBoardRole(csil.BoardRole("owner")); err == nil {
		t.Error("bogus role: expected an error")
	}
}

// --- cursor encode/decode ---

func TestBoardCursorRoundTrip(t *testing.T) {
	cursor := encodeBoardCursor("General", "01H000000000000000000000")
	title, id, err := decodeBoardCursor(&cursor)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if title != "General" || id != "01H000000000000000000000" {
		t.Errorf("got (%q, %q)", title, id)
	}
}

func TestBoardCursorNilOrEmptyIsFirstPage(t *testing.T) {
	title, id, err := decodeBoardCursor(nil)
	if err != nil || title != "" || id != "" {
		t.Fatalf("nil cursor: got (%q, %q, %v)", title, id, err)
	}
	empty := csil.PageCursor("")
	title, id, err = decodeBoardCursor(&empty)
	if err != nil || title != "" || id != "" {
		t.Fatalf("empty cursor: got (%q, %q, %v)", title, id, err)
	}
}

func TestBoardCursorMalformedDegradesGracefully(t *testing.T) {
	// Not valid base64 at all.
	bogus := csil.PageCursor("!!! not base64 !!!")
	if _, _, err := decodeBoardCursor(&bogus); err == nil {
		t.Fatal("expected an error decoding non-base64 cursor")
	}

	// Valid base64, but no separator once decoded.
	noSep := csil.PageCursor("bm8tc2VwYXJhdG9yLWhlcmU=") // "no-separator-here"
	if _, _, err := decodeBoardCursor(&noSep); err == nil {
		t.Fatal("expected an error decoding a cursor with no separator")
	}
}

// --- boardToWire ---

func TestBoardToWire(t *testing.T) {
	b := store.Board{
		ID:        "board-1",
		Slug:      "general",
		Title:     "General",
		Kind:      "discussion",
		CreatedBy: "user-1",
	}
	wire := boardToWire(b)
	if wire.Description != nil {
		t.Errorf("empty description should map to nil, got %v", *wire.Description)
	}
	if wire.ArchivedAt != nil {
		t.Errorf("unarchived board should have nil ArchivedAt")
	}

	b.Description = "a place to talk"
	wire = boardToWire(b)
	if wire.Description == nil || *wire.Description != "a place to talk" {
		t.Errorf("expected description to round-trip, got %v", wire.Description)
	}
}

// --- authz matrix (pure logic; DB-touching paths are covered by the
// integration suite in board_integration_test.go) ---

func TestIsInstanceAdmin(t *testing.T) {
	if IsInstanceAdmin(nil) {
		t.Error("nil (anonymous) user must never be admin")
	}
	if IsInstanceAdmin(&store.User{Roles: store.StringArray{}}) {
		t.Error("user with no roles must not be admin")
	}
	if IsInstanceAdmin(&store.User{Roles: store.StringArray{"member"}}) {
		t.Error("user with unrelated roles must not be admin")
	}
	if !IsInstanceAdmin(&store.User{Roles: store.StringArray{"member", RoleAdmin}}) {
		t.Error("user whose roles contain \"admin\" must be admin")
	}
}

// canManageBoardMetadata's admin branch must short-circuit before touching
// the store — verified here by passing a nil *store.Store, which would
// panic if the function tried to query it.
func TestCanManageBoardMetadataAdminShortCircuit(t *testing.T) {
	admin := &store.User{ID: "u-admin", Roles: store.StringArray{RoleAdmin}}
	allowed, err := canManageBoardMetadata(context.Background(), nil, admin, "board-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("an instance admin must be allowed to manage any board's metadata")
	}
}

// BoardRole (authz.go) must resolve to ("", nil) without issuing a query
// when userID or boardID is empty — verified with a nil store, same
// reasoning as above.
func TestBoardRoleEmptyIDsShortCircuit(t *testing.T) {
	role, err := BoardRole(context.Background(), nil, "", "board-1")
	if err != nil || role != "" {
		t.Fatalf("empty userID: got (%q, %v)", role, err)
	}
	role, err = BoardRole(context.Background(), nil, "user-1", "")
	if err != nil || role != "" {
		t.Fatalf("empty boardID: got (%q, %v)", role, err)
	}
}
