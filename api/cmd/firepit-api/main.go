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

	svcs := server.Services{
		Auth:         csilservices.NewAuthService(st),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st),
		Endorsement:  csilservices.NewEndorsementService(st),
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
