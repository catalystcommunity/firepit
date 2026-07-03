// Package coredb embeds the goose migrations that define firepit's
// PostgreSQL schema. api/cmd/firepit-api wires Migrations into goose at
// boot to run `migrate`.
//
// This is a scaffold stub (task A1) — migrations/ is empty except for
// .gitkeep. The baseline migration (000001_baseline.sql, per PLANDOC.md §4)
// lands in task A3.
package coredb

import "embed"

//go:embed all:migrations
var Migrations embed.FS
