// Command firepit-seed adds initial data to a Firepit database. It is a
// separate command so that seeding is an explicit operator action. Repeated
// runs do not create duplicate seed data. See docs/OPERATING.md for details.
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
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: firepit-seed [flags]")
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Seed project boards and optional deployment data.")
		fmt.Fprintln(flag.CommandLine.Output(), "Run database migrations before this command.")
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	var admins repeatedFlag
	var trustedDomains repeatedFlag
	demo := flag.Bool("demo", false, "seed demo users, threads, endorsements, and subscriptions (dev only; skip in prod)")
	githubMappings := flag.Bool("github-mappings", false, "seed GitHub webhook mappings for firepit/csilgen/reactorcide (prod opt-in; see docs/OPERATING.md)")
	flag.Var(&admins, "admin", "domain:user_id of a linkkeys identity to grant instance-admin (repeatable)")
	flag.Var(&trustedDomains, "trusted-domain", "linkkeys identity domain to add to the instance trust list (repeatable)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "firepit-seed: unexpected argument: %s\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}

	if err := run(context.Background(), []string(admins), []string(trustedDomains), *demo, *githubMappings); err != nil {
		log.WithError(err).Error("firepit-seed failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, adminSpecs, trustedDomainSpecs []string, demo, githubMappingsFlag bool) error {
	dbURI := resolveDBURI()
	log.WithField("db", redactDBURI(dbURI)).Info("firepit-seed: connecting")

	gdb, err := store.Open(dbURI)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	// Missing rows are expected because each seed operation uses an upsert.
	// Suppress only those messages so that real database warnings stay visible.
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

	if len(trustedDomainSpecs) > 0 {
		if err := seedTrustedDomains(ctx, st, bot.ID, trustedDomainSpecs); err != nil {
			return fmt.Errorf("seeding trusted domains: %w", err)
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

// resolveDBURI accepts both the API and migration environment variable names.
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
