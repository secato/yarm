// Package lutris reads the games database of Lutris, the Linux game
// launcher: pga.db, a SQLite file in Lutris' data directory. Lutris
// tags every installed game with the store it came from, which makes
// it a discovery source for the gog and epic providers, and its
// Battle.net client entry carries the wine prefix the battlenet
// provider reads Battle.net's own product database from.
//
// The database is opened read-only through the pure-Go SQLite driver:
// yarm builds with CGO_ENABLED=0, so a cgo driver was never an option,
// and pga.db belongs to a directory yarm does not own — writing to it,
// or even letting the driver create -wal/-shm sidecar files there, is
// out of the question.
package lutris

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/secato/yarm/internal/platform/sources"

	_ "modernc.org/sqlite" // registers the "sqlite" driver, pure Go
)

// Games returns the installed store-tagged games one pga.db describes.
// A missing database is not an error — callers probe the native and
// Flatpak locations — but an unreadable or corrupt one is, and costs
// the Lutris games of the provider that called this, never the other
// providers.
func Games(dbPath string) ([]sources.Entry, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT name, service, service_id, directory
		FROM games
		WHERE installed = 1
		  AND directory IS NOT NULL AND directory != ''
		  AND service_id IS NOT NULL AND service_id != ''
		  AND service IN ('gog', 'egs')`)
	if err != nil {
		return nil, fmt.Errorf("lutris: %s: %w", dbPath, err)
	}
	defer func() { _ = rows.Close() }()

	// Lutris calls the Epic store "egs"; the entries yarm hands onward
	// speak the providers' names.
	storeByService := map[string]string{
		"gog": sources.StoreGOG,
		"egs": sources.StoreEpic,
	}

	var entries []sources.Entry
	for rows.Next() {
		var name, service, serviceID, directory string
		if err := rows.Scan(&name, &service, &serviceID, &directory); err != nil {
			return nil, fmt.Errorf("lutris: %s: %w", dbPath, err)
		}
		store, ok := storeByService[service]
		if !ok {
			continue
		}
		entries = append(entries, sources.Entry{
			Store:   store,
			StoreID: serviceID,
			Title:   name,
			Root:    directory,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lutris: %s: %w", dbPath, err)
	}
	return entries, nil
}

// BattleNetPrefixes returns the wine prefixes hosting a Battle.net
// client install, one pga.db's worth. Lutris installs the client as a
// game of its own (slug "battlenet") whose directory points inside the
// prefix; the prefix is everything before drive_c, which is how
// Lutris' own Battle.net support derives it.
func BattleNetPrefixes(dbPath string) ([]string, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT directory
		FROM games
		WHERE installed = 1
		  AND directory IS NOT NULL AND directory != ''
		  AND slug IN ('battlenet', 'battle.net')`)
	if err != nil {
		return nil, fmt.Errorf("lutris: %s: %w", dbPath, err)
	}
	defer func() { _ = rows.Close() }()

	var prefixes []string
	seen := make(map[string]bool)
	for rows.Next() {
		var directory string
		if err := rows.Scan(&directory); err != nil {
			return nil, fmt.Errorf("lutris: %s: %w", dbPath, err)
		}
		prefix, ok := prefixOf(directory)
		if !ok || seen[prefix] {
			continue
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lutris: %s: %w", dbPath, err)
	}
	return prefixes, nil
}

// prefixOf derives a wine prefix root from a path inside its drive_c.
func prefixOf(directory string) (string, bool) {
	i := strings.Index(directory, "drive_c")
	if i <= 0 {
		return "", false
	}
	return strings.TrimRight(directory[:i], "/\\"), true
}

// dsn builds the read-only SQLite URI for dbPath. A URI rather than a
// bare path so the driver can neither write nor create; percent-
// escaping keeps characters that are legal in a game directory's name
// (spaces, '#', '?') from being read as URI syntax.
func dsn(dbPath string) string {
	p := url.PathEscape(filepath.ToSlash(dbPath))
	return "file:" + p + "?mode=ro"
}
