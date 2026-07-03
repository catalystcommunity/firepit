// Package config loads firepit-api's runtime configuration from FIREPIT_*
// environment variables (PLANDOC.md §7, task B1). It is deliberately plain
// stdlib (os.Getenv + strconv), not app-utils-go/config: the sibling repo
// this project follows most closely, longhouse
// (api/internal/config/config.go), doesn't use that package either — it's a
// flat set of getenv-with-default helpers. This package keeps that same
// getenv-with-default approach but groups the results into a Config struct
// (rather than longhouse's package-level vars) so the server can be
// constructed with an explicit, testable dependency instead of reaching for
// globals — the one deliberate deviation from the longhouse pattern, noted
// here since B2-B9 don't need to touch this file at all.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config is firepit-api's full runtime configuration, loaded once at boot
// (see Load) and threaded explicitly through main.go to whatever needs it.
type Config struct {
	// Port is the HTTP port firepit-api listens on.
	Port int
	// DBURI is the Postgres connection string (gorm/pgx DSN).
	DBURI string
	// CORSOrigins is the allow-list passed to rs/cors. A single entry of
	// "*" (the default) allows any origin, which is fine for local dev but
	// should be narrowed in any real deployment.
	CORSOrigins []string
	// MigrateOnBoot runs coredb's goose migrations against DBURI before
	// serving traffic. Defaults to true (dev convenience); real deployments
	// may want migrations run as a separate release step instead and can
	// set FIREPIT_MIGRATE_ON_BOOT=false.
	MigrateOnBoot bool

	// --- linkkeys RP config (PLANDOC.md §2, §3) ---
	//
	// Struct fields only — task B2 (AuthService + linkkeys RP client) owns
	// reading, validating, and acting on these. Names mirror longhouse's
	// LONGHOUSE_LINKKEYS_* env vars 1:1 (api/internal/config/config.go
	// there), just under the FIREPIT_ prefix, so porting
	// longhouse/api/internal/linkkeys needs zero renaming.

	// LinkkeysDomain is firepit's own relying-party DNS identity.
	LinkkeysDomain string
	// LinkkeysURL is the linkkeys RP sidecar's base URL (HTTP transport).
	LinkkeysURL string
	// LinkkeysPKIURL/LinkkeysPKIAPIKey/LinkkeysPKIAllowInvalidCerts
	// configure the RP sidecar's PKI verification endpoint.
	LinkkeysPKIURL               string
	LinkkeysPKIAPIKey            string
	LinkkeysPKIAllowInvalidCerts bool
	// LinkkeysTransport selects how the api reaches the linkkeys RP: "http"
	// or "tcp".
	LinkkeysTransport string
	// TCP transport knobs, used only when LinkkeysTransport == "tcp".
	LinkkeysTCPAddr          string
	LinkkeysTCPFingerprints  string
	LinkkeysTCPAllowInsecure bool
	// LinkkeysIDPDomain/LinkkeysIDPURL identify the IDP the login flow
	// redirects to (firepit.catalystsquad.com's first deployment IDP is
	// linkkeys.todandlorna.com per PLANDOC.md §1).
	LinkkeysIDPDomain string
	LinkkeysIDPURL    string
}

// Load reads Config from FIREPIT_* environment variables, filling in
// dev-friendly defaults for anything unset.
func Load() Config {
	return Config{
		Port:          getEnvAsIntOrDefault("FIREPIT_PORT", 8080),
		DBURI:         getEnvOrDefault("FIREPIT_DB_URI", "postgresql://firepit:devpass123@localhost:5432/firepit_db?sslmode=disable"),
		CORSOrigins:   getEnvAsListOrDefault("FIREPIT_CORS_ORIGINS", []string{"*"}),
		MigrateOnBoot: getEnvAsBoolOrDefault("FIREPIT_MIGRATE_ON_BOOT", true),

		LinkkeysDomain:               getEnvOrDefault("FIREPIT_LINKKEYS_DOMAIN", ""),
		LinkkeysURL:                  getEnvOrDefault("FIREPIT_LINKKEYS_URL", ""),
		LinkkeysPKIURL:               getEnvOrDefault("FIREPIT_LINKKEYS_PKI_URL", ""),
		LinkkeysPKIAPIKey:            getEnvOrDefault("FIREPIT_LINKKEYS_PKI_API_KEY", ""),
		LinkkeysPKIAllowInvalidCerts: getEnvAsBoolOrDefault("FIREPIT_LINKKEYS_PKI_ALLOW_INVALID_CERTS", false),
		LinkkeysTransport:            getEnvOrDefault("FIREPIT_LINKKEYS_TRANSPORT", "http"),
		LinkkeysTCPAddr:              getEnvOrDefault("FIREPIT_LINKKEYS_TCP_ADDR", ""),
		LinkkeysTCPFingerprints:      getEnvOrDefault("FIREPIT_LINKKEYS_TCP_FINGERPRINTS", ""),
		LinkkeysTCPAllowInsecure:     getEnvAsBoolOrDefault("FIREPIT_LINKKEYS_TCP_ALLOW_INSECURE", false),
		LinkkeysIDPDomain:            getEnvOrDefault("FIREPIT_LINKKEYS_IDP_DOMAIN", ""),
		LinkkeysIDPURL:               getEnvOrDefault("FIREPIT_LINKKEYS_IDP_URL", ""),
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvAsBoolOrDefault(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

// getEnvAsListOrDefault reads a comma-separated env var into a string slice,
// trimming whitespace and dropping empty entries.
func getEnvAsListOrDefault(key string, defaultVal []string) []string {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	var out []string
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return defaultVal
	}
	return out
}
