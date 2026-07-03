// Command firepit-seed is the CLI backing `./tools.sh seed`: an idempotent
// data seeder for a firepit deployment (task D3, PLANDOC.md §7 D3 / §8
// milestone M3's "dogfood setup"). It is deliberately a separate binary
// from firepit-api (cmd/firepit-api) rather than a boot-time hook, so
// seeding is an explicit, re-runnable operator action — never something
// that fires as a side effect of the api process starting up.
//
// # What it seeds, and when
//
// Every run (no flags required) seeds:
//
//   - The project boards this org's own repos get discussed in, plus a
//     general-purpose "general" board and an announce-kind
//     "announcements" board — see boards.go's projectBoards.
//
// `--admin domain:user_id` (repeatable) additionally bootstraps instance
// admins: upserts a `users` row for that linkkeys identity (creating one if
// this is its first appearance) and grants it the "admin" role
// (csilservices.RoleAdmin) if it doesn't already have it. THIS IS THE
// documented admin bootstrap path (see docs/OPERATING.md) — there is no
// CSIL op that grants the first admin, since every admin-only op requires
// an existing admin to call it.
//
// `--demo` additionally seeds a handful of demo users and a few threads
// with nested comments, endorsements, and subscriptions, so a fresh dev
// environment is immediately browsable. Skip this in any real deployment
// (see demo.go's package doc comment for exactly what it creates).
//
// `--github-mappings` additionally creates GitHub webhook routing for a
// few real catalystcommunity repos (github_mappings rows only — it does
// NOT set the HMAC secret env vars themselves, and does not install
// webhooks on GitHub's side; see docs/OPERATING.md's GitHub webhook setup
// section and github.go's package doc comment). Off by default so a prod
// run of `firepit-seed` doesn't silently opt an instance into ingesting
// from repos its operator hasn't deliberately wired secrets for.
//
// # Idempotency
//
// Every step here is safe to run twice: boards upsert by slug, the admin
// bootstrap upserts by (linkkeys_domain, linkkeys_user_id) and only grants
// the admin role if missing, GitHub mappings upsert by repo, and the demo
// dataset checks for its own canary post before creating anything (see
// demo.go). Nothing here ever deletes or overwrites content a previous run
// (or a real user) created.
//
// # Database connection
//
// Reads FIREPIT_DB_URI (matching firepit-api's own config.Config.DBURI env
// var), falling back to DB_URI (matching coredb/cmd/migrate's env var, so
// `./tools.sh migrate && ./tools.sh seed` need only one exported var if a
// caller prefers that name), falling back to the same default the compose
// stack's postgres service uses. It does NOT run migrations itself — run
// `./tools.sh migrate` first against a fresh database.
package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/logger"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// defaultDBURI matches docker-compose.yaml's postgres service and
// coredb/cmd/migrate's own defaultDBURL — the one connection string every
// "just run it against local dev" tool in this repo defaults to.
const defaultDBURI = "postgresql://firepit:devpass123@localhost:5432/firepit_db?sslmode=disable"

// repeatedFlag collects every occurrence of a repeatable CLI flag (e.g.
// `--admin a:b --admin c:d`) in the order given. flag.Value's minimal
// interface (String/Set) is all a flag.Var registration needs.
type repeatedFlag []string

func (r *repeatedFlag) String() string {
	if r == nil {
		return ""
	}
	return strings.Join(*r, ",")
}

func (r *repeatedFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	var admins repeatedFlag
	demo := flag.Bool("demo", false, "seed demo users, threads, endorsements, and subscriptions (dev only; skip in prod)")
	githubMappings := flag.Bool("github-mappings", false, "seed GitHub webhook mappings for firepit/csilgen/reactorcide (prod opt-in; see docs/OPERATING.md)")
	flag.Var(&admins, "admin", "domain:user_id of a linkkeys identity to grant instance-admin (repeatable)")
	flag.Parse()

	if err := run(context.Background(), []string(admins), *demo, *githubMappings); err != nil {
		log.WithError(err).Error("firepit-seed failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, adminSpecs []string, demo, githubMappingsFlag bool) error {
	dbURI := resolveDBURI()
	log.WithField("db", redactDBURI(dbURI)).Info("firepit-seed: connecting")

	gdb, err := store.Open(dbURI)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	// Every upsert helper in this program (upsertBoard, ensureSeedBotUser,
	// bootstrapAdmins, upsertGithubMapping, ...) does a First()-then-maybe-
	// Create() lookup, and "no row yet" is the expected, common case on a
	// fresh database — not a fault. gorm's default logger writes every
	// ErrRecordNotFound to stderr as an error-level line regardless, which
	// would otherwise bury this program's own structured logrus output
	// under noise on every single run. IgnoreRecordNotFoundError silences
	// exactly that one case while leaving every other gorm-level warning/
	// error (a real constraint violation, a connection failure) visible.
	gdb.Logger = logger.New(
		stdlog.New(os.Stderr, "", stdlog.LstdFlags),
		logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		},
	)
	st := store.New(gdb)

	bot, err := ensureSeedBotUser(ctx, st)
	if err != nil {
		return fmt.Errorf("ensuring seed-bot system user: %w", err)
	}
	log.WithField("user_id", bot.ID).Info("firepit-seed: seed-bot system user ready")

	boardIDs, err := seedBoards(ctx, st, bot.ID)
	if err != nil {
		return fmt.Errorf("seeding boards: %w", err)
	}
	log.WithField("count", len(boardIDs)).Info("firepit-seed: boards ready")

	if len(adminSpecs) > 0 {
		if err := bootstrapAdmins(ctx, st, adminSpecs); err != nil {
			return fmt.Errorf("bootstrapping admins: %w", err)
		}
	}

	if githubMappingsFlag {
		if err := seedGithubMappings(ctx, st, bot.ID, boardIDs); err != nil {
			return fmt.Errorf("seeding github mappings: %w", err)
		}
	}

	if demo {
		if err := seedDemo(ctx, st, boardIDs); err != nil {
			return fmt.Errorf("seeding demo content: %w", err)
		}
	}

	log.Info("firepit-seed: done")
	return nil
}

// resolveDBURI mirrors coredb/cmd/migrate's DB_URI convention but prefers
// FIREPIT_DB_URI first (firepit-api's own config.Config env var name) so
// operators who only ever export the api's own var still get the right
// default for seeding too. Both env vars, and the hardcoded fallback, name
// the exact same local dev database.
func resolveDBURI() string {
	if v := os.Getenv("FIREPIT_DB_URI"); v != "" {
		return v
	}
	if v := os.Getenv("DB_URI"); v != "" {
		return v
	}
	return defaultDBURI
}

// redactDBURI strips a userinfo (user:password@) component before logging
// a connection string, so a real deployment's seed run never writes a
// plaintext DB password into its own logs.
func redactDBURI(uri string) string {
	at := strings.Index(uri, "@")
	scheme := strings.Index(uri, "://")
	if at == -1 || scheme == -1 || at < scheme {
		return uri
	}
	return uri[:scheme+3] + "***@" + uri[at+1:]
}
