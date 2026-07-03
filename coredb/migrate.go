package coredb

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// openGoose opens a *sql.DB against dbURL (a postgres connection string) and
// points goose at the embedded Migrations tree. Callers must Close() the
// returned *sql.DB.
func openGoose(dbURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	goose.SetBaseFS(Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting goose dialect: %w", err)
	}
	return db, nil
}

// Up runs every pending migration against dbURL.
func Up(dbURL string) error {
	db, err := openGoose(dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("running migrations up: %w", err)
	}
	return nil
}

// Reset rolls every applied migration back, leaving the database at version
// zero. This is what `./tools.sh migrate down` invokes: with a single
// baseline migration today, "down" and "down to zero" are the same
// operation, but Reset keeps that true as more migrations land.
func Reset(dbURL string) error {
	db, err := openGoose(dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.DownTo(db, migrationsDir, 0); err != nil {
		return fmt.Errorf("running migrations down to zero: %w", err)
	}
	return nil
}

// Status prints the applied/pending state of every migration to stdout via
// goose's own logger.
func Status(dbURL string) error {
	db, err := openGoose(dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Status(db, migrationsDir); err != nil {
		return fmt.Errorf("getting migration status: %w", err)
	}
	return nil
}
