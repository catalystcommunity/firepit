// Command migrate is the CLI backing `./tools.sh migrate`: it runs goose
// up/down/status against a Postgres DB URL, embedding coredb's migrations.
package main

import (
	"fmt"
	"os"

	"github.com/catalystcommunity/firepit/coredb"
)

// defaultDBURL matches the postgres service in the repo-root
// docker-compose.yaml, for local `./tools.sh dev` usage.
const defaultDBURL = "postgresql://firepit:devpass123@localhost:5432/firepit_db?sslmode=disable"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help") {
		usage(os.Stdout)
		return
	}
	if len(os.Args) != 2 {
		usage(os.Stderr)
		os.Exit(1)
	}

	dbURL := os.Getenv("DB_URI")
	if dbURL == "" {
		dbURL = defaultDBURL
	}

	var err error
	switch verb := os.Args[1]; verb {
	case "up":
		err = coredb.Up(dbURL)
	case "down":
		err = coredb.Reset(dbURL)
	case "status":
		err = coredb.Status(dbURL)
	default:
		usage(os.Stderr)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage(w *os.File) {
	fmt.Fprintln(w, "Usage: migrate <up|down|status>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run Firepit database migrations against DB_URI.")
	fmt.Fprintln(w, "If DB_URI is not set, use the local Docker Compose database.")
}
