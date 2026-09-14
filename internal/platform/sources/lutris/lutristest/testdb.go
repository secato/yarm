// Package lutristest builds Lutris pga.db fixtures for the tests of
// the providers that read one. The database is created through SQLite
// itself, the same way Lutris creates the real thing, so the readers
// are exercised against a genuine file rather than hand-crafted bytes.
package lutristest

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Row is one games-table row, as much of it as discovery reads. An
// empty ServiceID or Directory becomes NULL, the shape a
// half-installed or imported row really has.
type Row struct {
	Name      string
	Slug      string
	Service   string
	ServiceID string
	Directory string
	Installed bool
}

// Create writes a pga.db with Lutris' schema and the given rows, and
// returns its path.
func Create(t *testing.T, rows ...Row) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pga.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Lutris' own schema, from lutris/database/schema.py.
	if _, err := db.Exec(`CREATE TABLE games (
		id INTEGER PRIMARY KEY,
		name TEXT,
		sortname TEXT,
		slug TEXT,
		installer_slug TEXT,
		parent_slug TEXT,
		platform TEXT,
		runner TEXT,
		executable TEXT,
		directory TEXT,
		updated DATETIME,
		lastplayed INTEGER,
		installed INTEGER,
		installed_at INTEGER,
		year INTEGER,
		configpath TEXT,
		has_custom_banner INTEGER,
		has_custom_icon INTEGER,
		has_custom_coverart_big INTEGER,
		playtime REAL,
		service TEXT,
		service_id TEXT,
		discord_id TEXT
	)`); err != nil {
		t.Fatal(err)
	}

	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	for _, r := range rows {
		installed := 0
		if r.Installed {
			installed = 1
		}
		if _, err := db.Exec(
			`INSERT INTO games (name, slug, service, service_id, directory, installed) VALUES (?, ?, ?, ?, ?, ?)`,
			r.Name, r.Slug, r.Service, nullable(r.ServiceID), nullable(r.Directory), installed,
		); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
