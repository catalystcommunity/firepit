// Command firepit-api is the firepit backend service: a single Go binary
// that serves CSIL-RPC (CBOR over POST /csil/v1/rpc), the linkkeys auth
// callback (B2), the GitHub webhook receiver (B8), and /healthz — see
// PLANDOC.md §3.
//
// Boot sequence (task B1): load config -> open the Postgres connection ->
// optionally run coredb's goose migrations -> construct the (today: all
// stub) service implementations -> serve HTTP until SIGINT/SIGTERM, then
// drain in-flight requests before exiting.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/config"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/server"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	log.Info("firepit-api starting")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.WithError(err).Error("firepit-api exited with error")
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg := config.Load()

	if cfg.MigrateOnBoot {
		log.Info("running coredb migrations")
		if err := coredb.Up(cfg.DBURI); err != nil {
			return err
		}
	} else {
		log.Info("FIREPIT_MIGRATE_ON_BOOT=false: skipping migrations")
	}

	gdb, err := store.Open(cfg.DBURI)
	if err != nil {
		return err
	}
	st := store.New(gdb)

	// notify.NewDBPublisher (api/internal/notify/publisher.go, task B7) is
	// the real fan-out Publisher: ThreadService.CreatePost/CreateComment/
	// EditPost/EditComment and EndorsementService.Endorse are each meant to
	// call Publish inside their own write transaction (notify.go's
	// contract), so NewThreadService/NewEndorsementService need to accept a
	// notify.Publisher. As of this branch those two constructors still only
	// take *store.Store (B4/B5 land that constructor change separately, in
	// their own worktrees) — wiring `pub` into their New* calls here would
	// reference a parameter that doesn't exist yet and fail to compile, so
	// it's deliberately left as this comment rather than a broken edit.
	// Once B4/B5 add that parameter (each presumably defaulting it to
	// notify.Noop{} in the interim, per notify.go's doc comment), replace
	// their Noop with this pub at the merge that combines those branches:
	//
	//	pub := notify.NewDBPublisher()
	//	...
	//	Thread:      csilservices.NewThreadService(st, pub),
	//	Endorsement: csilservices.NewEndorsementService(st, pub),
	svcs := server.Services{
		Auth:         csilservices.NewAuthService(st, cfg),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st, notify.Noop{}),
		Endorsement:  csilservices.NewEndorsementService(st, notify.Noop{}),
		Settings:     csilservices.NewSettingsService(st),
		Social:       csilservices.NewSocialService(st),
		Subscription: csilservices.NewSubscriptionService(st),
		Read:         csilservices.NewReadService(st),
		Notification: csilservices.NewNotificationService(st),
		Integration:  csilservices.NewIntegrationService(st),
	}

	srv := server.New(cfg, st, svcs)
	log.WithField("port", cfg.Port).Info("firepit-api ready")
	return srv.ListenAndServe(ctx)
}
