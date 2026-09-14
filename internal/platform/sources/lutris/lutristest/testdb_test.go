package lutristest

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCreateBuildsReadableDB(t *testing.T) {
	path := Create(t,
		Row{Name: "Puzzle Agent", Slug: "puzzle-agent", Service: "gog", ServiceID: "1207658924", Directory: "/games/gog/PuzzleAgent", Installed: true},
		Row{Name: "No directory", Slug: "empty", Service: "gog", ServiceID: "43", Directory: "", Installed: true},
	)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	var total, nulls int
	if err := db.QueryRow(`SELECT COUNT(*), COUNT(directory) FROM games`).Scan(&total, &nulls); err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 2 || nulls != 1 {
		t.Errorf("games table has %d rows and %d non-null directories, want 2 and 1 (empty becomes NULL)", total, nulls)
	}
}
