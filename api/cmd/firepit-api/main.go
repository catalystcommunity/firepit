// Command firepit-api is the firepit backend service: a single Go binary
// that serves CSIL-RPC (CBOR over POST /csil/v1/rpc), the linkkeys auth
// callback, the GitHub webhook receiver, and /healthz — see PLANDOC.md §3.
//
// This is a scaffold stub (task A1): it boots, logs, and exits cleanly on
// SIGINT/SIGTERM. Config loading, migrations, the CSIL-RPC dispatcher, and
// the linkkeys RP client are wired in task B1.
package main

import (
	"context"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	log.Info("firepit-api starting")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// TODO(B1): load config (FIREPIT_* env, app-utils-go), run coredb
	// migrations, start the CSIL-RPC dispatcher + linkkeys RP client,
	// serve /healthz. This scaffold only boots and waits for a signal.
	log.Info("firepit-api ready (scaffold placeholder — no server wired yet)")

	<-ctx.Done()
	log.Info("firepit-api shutting down")
}
