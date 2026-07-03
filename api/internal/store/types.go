// Package store holds gorm model structs that mirror every table in
// coredb's baseline migration (PLANDOC.md §4), plus a thin constructor
// wrapping a *gorm.DB. There is no query/business logic here beyond what
// the round-trip integration test needs — services in later tasks (B3-B9)
// build repositories on top of these models.
package store

import (
	"database/sql/driver"
	"fmt"

	"github.com/lib/pq"
)

// Ltree scans/values a Postgres `ltree` column as its dotted-label text
// representation (e.g. "01Habc.01Hdef"). gorm/pgx have no built-in ltree
// support, so this is a minimal Scanner/Valuer — no path arithmetic here,
// that lives in the ThreadService layer (task B4).
type Ltree string

// Scan implements sql.Scanner.
func (l *Ltree) Scan(value any) error {
	if value == nil {
		*l = ""
		return nil
	}
	switch v := value.(type) {
	case string:
		*l = Ltree(v)
	case []byte:
		*l = Ltree(v)
	default:
		return fmt.Errorf("store: unsupported type %T for Ltree", value)
	}
	return nil
}

// Value implements driver.Valuer.
func (l Ltree) Value() (driver.Value, error) {
	return string(l), nil
}

// StringArray scans/values a Postgres `text[]` column. It wraps
// pq.StringArray rather than using it directly because pq.StringArray(nil)
// values to a SQL NULL, which violates the NOT NULL constraint every
// text[] column in the schema (users.roles, github_mappings.events)
// carries — a nil-vs-empty-slice footgun for any caller that doesn't
// remember to initialize the field. StringArray(nil) values to '{}'
// instead.
type StringArray pq.StringArray

// Scan implements sql.Scanner.
func (a *StringArray) Scan(value any) error {
	var arr pq.StringArray
	if err := arr.Scan(value); err != nil {
		return err
	}
	*a = StringArray(arr)
	return nil
}

// Value implements driver.Valuer.
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		a = StringArray{}
	}
	return pq.StringArray(a).Value()
}
