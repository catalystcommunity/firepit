package store

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Store wraps a *gorm.DB configured for the Firepit schema.
type Store struct {
	DB *gorm.DB
}

// New wraps an already-open *gorm.DB.
func New(db *gorm.DB) *Store {
	return &Store{DB: db}
}

// Open opens a *gorm.DB against dsn (a Postgres connection string). It
// does not run migrations — callers run coredb's goose migrations
// separately (see coredb.Up) before using the returned Store.
func Open(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}
