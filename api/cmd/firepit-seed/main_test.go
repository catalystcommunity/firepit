package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// --- pure helpers: no database required ---

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Ada Lovelace":    "ada-lovelace",
		"  spaced  out  ": "spaced-out",
		"already-slug":    "already-slug",
		"todhansmann":     "todhansmann",
		"MiXeD_Case.99":   "mixed-case-99",
		"---":             "",
		"":                "",
	}
	for in, want := range cases {
		require.Equal(t, want, slugify(in), "slugify(%q)", in)
	}
}

func TestTitleCaseWords(t *testing.T) {
	cases := map[string]string{
		"firepit-seed-bot": "Firepit Seed Bot",
		"alice":            "Alice",
		"todd_hansmann":    "Todd Hansmann",
		"already good":     "Already Good",
		"":                 "",
	}
	for in, want := range cases {
		require.Equal(t, want, titleCaseWords(in), "titleCaseWords(%q)", in)
	}
}

func TestParseAdminSpec(t *testing.T) {
	t.Run("valid spec", func(t *testing.T) {
		domain, userID, err := parseAdminSpec("todandlorna.com:tod")
		require.NoError(t, err)
		require.Equal(t, "todandlorna.com", domain)
		require.Equal(t, "tod", userID)
	})

	t.Run("user_id may itself contain a colon", func(t *testing.T) {
		// Splitting on the FIRST colon (not the last) is deliberate — see
		// parseAdminSpec's doc comment.
		domain, userID, err := parseAdminSpec("example.com:weird:id")
		require.NoError(t, err)
		require.Equal(t, "example.com", domain)
		require.Equal(t, "weird:id", userID)
	})

	for _, bad := range []string{
		"",
		"nocolon",
		":missing-domain",
		"missing-userid:",
	} {
		t.Run("rejects "+bad, func(t *testing.T) {
			_, _, err := parseAdminSpec(bad)
			require.Error(t, err)
		})
	}
}

func TestStringSlicesEqual(t *testing.T) {
	require.True(t, stringSlicesEqual(nil, nil))
	require.True(t, stringSlicesEqual([]string{}, nil))
	require.True(t, stringSlicesEqual([]string{"a", "b"}, []string{"a", "b"}))
	require.False(t, stringSlicesEqual([]string{"a", "b"}, []string{"b", "a"}))
	require.False(t, stringSlicesEqual([]string{"a"}, []string{"a", "b"}))
}

func TestRedactDBURI(t *testing.T) {
	cases := map[string]string{
		"postgresql://firepit:devpass123@localhost:5432/firepit_db?sslmode=disable": "postgresql://***@localhost:5432/firepit_db?sslmode=disable",
		"postgresql://localhost:5432/firepit_db": "postgresql://localhost:5432/firepit_db",
		"not-a-uri":                              "not-a-uri",
	}
	for in, want := range cases {
		require.Equal(t, want, redactDBURI(in), "redactDBURI(%q)", in)
	}
}

func TestResolveDBURI(t *testing.T) {
	t.Setenv("FIREPIT_DB_URI", "")
	t.Setenv("DB_URI", "")
	require.Equal(t, defaultDBURI, resolveDBURI())

	t.Setenv("DB_URI", "postgresql://from-db-uri/db")
	require.Equal(t, "postgresql://from-db-uri/db", resolveDBURI())

	t.Setenv("FIREPIT_DB_URI", "postgresql://from-firepit-db-uri/db")
	require.Equal(t, "postgresql://from-firepit-db-uri/db", resolveDBURI(), "FIREPIT_DB_URI takes precedence over DB_URI")
}
