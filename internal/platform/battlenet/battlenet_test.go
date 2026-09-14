package battlenet

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/secato/yarm/internal/platform/sources/lutris"
	"github.com/secato/yarm/internal/platform/sources/lutris/lutristest"
)

// --- a minimal protobuf encoder, the mirror of the wire parser ---

func encodeVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func fieldTag(num, wire int) []byte {
	return encodeVarint(uint64(num)<<3 | uint64(wire))
}

func bytesField(num int, payload []byte) []byte {
	out := fieldTag(num, wireBytes)
	out = append(out, encodeVarint(uint64(len(payload)))...)
	return append(out, payload...)
}

func stringField(num int, s string) []byte {
	return bytesField(num, []byte(s))
}

func boolField(num int, v bool) []byte {
	out := fieldTag(num, wireVarint)
	if v {
		return append(out, 1)
	}
	return append(out, 0)
}

func concat(fields ...[]byte) []byte { return bytes.Join(fields, nil) }

// productInstallMessage builds one ProductInstall message the way the
// agent writes it: uid (1), product_code (2), settings (3) with
// install_path (1), cached_product_state (4) with base_product_state
// (1) and its installed flag (1). The installed flag's value is fixed
// rather than a parameter: this package does not read it (see the
// package doc comment on productdb.go for why), so a caller-supplied
// value would only mislead a reader into thinking it mattered.
func productInstallMessage(uid, installPath string) []byte {
	settings := stringField(1, installPath)
	baseState := boolField(1, true)
	cachedState := bytesField(1, baseState)
	return concat(
		stringField(1, uid),
		stringField(2, "CODE"),
		bytesField(3, settings),
		bytesField(4, cachedState),
	)
}

func productDBMessage(installs ...[]byte) []byte {
	var out []byte
	for _, m := range installs {
		out = append(out, bytesField(1, m)...)
	}
	return out
}

func writeProductDB(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "product.db")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mkdirGame(t *testing.T, parts ...string) string {
	t.Helper()
	root := filepath.Join(append([]string{t.TempDir()}, parts...)...)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDiscover(t *testing.T) {
	diablo := mkdirGame(t, "games", "Diablo IV")
	overwatch := mkdirGame(t, "games", "Overwatch")
	client := mkdirGame(t, "battle.net", "drive_c", "Program Files (x86)", "Battle.net")
	// A removed game's entry lingers in product.db, but its directory is
	// gone — that absence, not the (untrusted) installed flag, is what
	// discovery leans on to leave it out.
	uninstalled := filepath.Join(t.TempDir(), "games", "Gone")

	db := writeProductDB(t, productDBMessage(
		productInstallMessage("fenris", diablo),
		productInstallMessage("prometheus", overwatch),
		productInstallMessage("bna", client),        // the client app, not a game
		productInstallMessage("agent_beta", client), // the agent, not a game
		productInstallMessage("odin", uninstalled),  // removed: nothing at that path anymore
	))

	games, err := NewWith([]productDB{{path: db}}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("Discover() returned %d games, want 2: %+v", len(games), games)
	}
	byID := make(map[string]string, len(games))
	for _, g := range games {
		byID[g.ID] = g.Name
	}
	if byID["battlenet:fenris"] != "Diablo IV" {
		t.Errorf("fenris = %q, want the title from the uid table (got %v)", byID["battlenet:fenris"], byID)
	}
	if byID["battlenet:prometheus"] != "Overwatch 2" {
		t.Errorf("prometheus = %q, want Overwatch 2", byID["battlenet:prometheus"])
	}
}

func TestDiscoverUnknownUIDFallsBackToFolderName(t *testing.T) {
	gameDir := mkdirGame(t, "games", "Some New Game")
	db := writeProductDB(t, productDBMessage(
		productInstallMessage("brandnew", gameDir),
	))

	games, err := NewWith([]productDB{{path: db}}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("Discover() returned %d games, want 1: %+v", len(games), games)
	}
	if games[0].Name != "Some New Game" {
		t.Errorf("Name = %q, want the install folder's name", games[0].Name)
	}
	if games[0].ID != "battlenet:brandnew" {
		t.Errorf("ID = %q, want the uid as the store id", games[0].ID)
	}
}

func TestDiscoverMissingDBIsNotAnError(t *testing.T) {
	games, err := NewWith([]productDB{{path: filepath.Join(t.TempDir(), "product.db")}}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil for a missing database", err)
	}
	if games != nil {
		t.Errorf("Discover() = %+v, want nil", games)
	}
}

func TestDiscoverGarbageDBIsSkippedNotFatal(t *testing.T) {
	good := mkdirGame(t, "games", "Diablo IV")
	goodDB := writeProductDB(t, productDBMessage(productInstallMessage("fenris", good)))
	badDB := writeProductDB(t, []byte{0xff, 0xff, 0xff, 0xff})

	games, err := NewWith([]productDB{{path: badDB}, {path: goodDB}}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v, want a torn file skipped, not a failure", err)
	}
	if len(games) != 1 || games[0].ID != "battlenet:fenris" {
		t.Errorf("Discover() = %+v, want the good database's game", games)
	}
}

func TestReadProductDBOversized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "product.db")
	if err := os.WriteFile(path, make([]byte, maxProductDBBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readProductDB(path); err == nil {
		t.Error("readProductDB() error = nil, want an error for a file over the size limit")
	}
}

func TestParseWireFieldShapes(t *testing.T) {
	// One message exercising every wire type: a varint (1), a
	// length-delimited field (2), a fixed64 (3, skipped value) and a
	// fixed32 (4, skipped value), plus an unknown field number that
	// must be skipped without asking what it is.
	msg := concat(
		boolField(1, true),
		stringField(2, "hello"),
		append(fieldTag(3, wireFixed64), 1, 2, 3, 4, 5, 6, 7, 8),
		append(fieldTag(4, wireFixed32), 1, 2, 3, 4),
		stringField(99, "unknown"),
	)

	fields, err := parseWireFields(msg)
	if err != nil {
		t.Fatalf("parseWireFields() error = %v", err)
	}
	if len(fields) != 5 {
		t.Fatalf("parseWireFields() returned %d fields, want 5: %+v", len(fields), fields)
	}
	if fields[0].num != 1 || fields[0].wire != wireVarint || fields[0].varint != 1 {
		t.Errorf("varint field = %+v", fields[0])
	}
	if fields[1].num != 2 || string(fields[1].data) != "hello" {
		t.Errorf("bytes field = %+v", fields[1])
	}
}

func TestParseWireFieldsRefusesMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"truncated varint", []byte{0x80}},
		{"length overruns body", append(fieldTag(1, wireBytes), 0x7f)},
		{"truncated fixed64", append(fieldTag(1, wireFixed64), 1, 2, 3)},
		{"truncated fixed32", append(fieldTag(1, wireFixed32), 1, 2, 3)},
		{"group wire type", fieldTag(1, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseWireFields(tc.body); err == nil {
				t.Errorf("parseWireFields() error = nil, want an error for %s", tc.name)
			}
		})
	}
}

func TestDiscoverFromLutrisPrefix(t *testing.T) {
	// Lutris and its wine prefixes are a Linux-only concept — and
	// os.UserHomeDir() honors $HOME only on Unix, not %USERPROFILE% —
	// so this has nothing to exercise on Windows.
	if runtime.GOOS == "windows" {
		t.Skip("Lutris wine prefixes are a Linux-only concept")
	}

	// A fake home carrying a Lutris install whose Battle.net client
	// prefix holds a product.db: the full Linux root chain, end to end,
	// through Discover rather than a path-building helper — the prefix
	// lookup itself now happens at Discover time, not construction.
	// The XDG variables are pinned so the real session's settings
	// cannot point discovery somewhere else.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	prefix := filepath.Join(home, "Games", "battle.net")
	clientDir := filepath.Join(prefix, "drive_c", "Program Files (x86)", "Battle.net")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pgaDB := filepath.Join(home, ".local", "share", "lutris", "pga.db")
	if err := os.MkdirAll(filepath.Dir(pgaDB), 0o755); err != nil {
		t.Fatal(err)
	}
	db := lutristest.Create(t, lutristest.Row{
		Name: "Battle.net", Slug: "battlenet", Directory: clientDir, Installed: true,
	})
	if err := os.Rename(db, pgaDB); err != nil {
		t.Fatal(err)
	}

	// product.db always holds Windows-side paths — even here, under a
	// wine prefix Lutris manages — so the game's real directory lives
	// under the prefix's own drive_c, not wherever this string points.
	diabloHost := filepath.Join(prefix, "drive_c", "Program Files (x86)", "Diablo IV")
	if err := os.MkdirAll(diabloHost, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(prefix, "drive_c", "ProgramData", "Battle.net", "Agent", "product.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := productDBMessage(productInstallMessage("fenris", `C:\Program Files (x86)\Diablo IV`))
	if err := os.WriteFile(dbPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	games, err := NewWith(nil, lutris.DefaultDBPaths()).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "battlenet:fenris" || games[0].Root != diabloHost {
		t.Errorf("Discover() = %+v, want the one game at %q, translated out of its Windows-side path", games, diabloHost)
	}
}

func TestHostPath(t *testing.T) {
	prefix := t.TempDir()
	cases := []struct {
		name    string
		winPath string
	}{
		{"backslashes", `C:\Program Files (x86)\Diablo IV`},
		{"forward slashes, as Battle.net actually writes them", "C:/Program Files (x86)/Diablo IV"},
		{"lowercase drive letter", `c:\Program Files (x86)\Diablo IV`},
	}
	want := filepath.Join(prefix, "drive_c", "Program Files (x86)", "Diablo IV")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostPath(prefix, tc.winPath); got != want {
				t.Errorf("hostPath(%q, %q) = %q, want %q", prefix, tc.winPath, got, want)
			}
		})
	}
}

func TestName(t *testing.T) {
	if got := (&Provider{}).Name(); got != "battlenet" {
		t.Errorf("Name() = %q, want battlenet", got)
	}
}
