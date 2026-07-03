// Package coredb embeds the goose migrations that define firepit's
// PostgreSQL schema and exposes Up/Reset/Status so both
// api/cmd/firepit-api and coredb/cmd/migrate (backing `./tools.sh migrate`)
// can apply them programmatically against a DB URL.
//
// See migrations/000001_baseline.sql (PLANDOC.md §4) for the schema itself.
package coredb

import "embed"

//go:embed all:migrations
var Migrations embed.FS

const migrationsDir = "migrations"
