// Command firepit-api serves the CSIL-RPC API, the linkkeys callback, the
// GitHub webhook receiver, and the health endpoint. It drains active requests
// when it receives SIGINT or SIGTERM.
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

	// Services publish notifications in their write transactions so that the
	// content and its notifications commit together.
	pub := notify.NewDBPublisher()
	svcs := server.Services{
		Auth:         csilservices.NewAuthService(st, cfg),
		Board:        csilservices.NewBoardService(st),
		Category:     csilservices.NewCategoryService(st),
		Thread:       csilservices.NewThreadService(st, pub),
		Endorsement:  csilservices.NewEndorsementService(st, pub),
		Settings:     csilservices.NewSettingsService(st),
		Social:       csilservices.NewSocialService(st),
		Subscription: csilservices.NewSubscriptionService(st),
		Read:         csilservices.NewReadService(st),
		Notification: csilservices.NewNotificationService(st),
		Integration:  csilservices.NewIntegrationService(st),
	}

	srv := server.New(cfg, st, svcs, pub)
	log.WithField("port", cfg.Port).Info("firepit-api ready")
	return srv.ListenAndServe(ctx)
}
