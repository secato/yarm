// Package battlenet implements a platform.Provider that discovers games
// from Battle.net by reading the Battle.net agent's product.db. On
// Windows that file sits in ProgramData; on Linux, where Battle.net has
// no native client, the games people do have live inside whatever wine
// prefix something else set up to run the Windows client — Lutris'
// managed prefixes (found by querying pga.db) and the fixed prefix path
// distro installers like Omarchy's umu-launcher-based one use (found
// without querying anything) are both covered, and the same product.db
// parser serves every path.
package battlenet

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/platform/sources"
	"github.com/secato/yarm/internal/platform/sources/lutris"
)

// ProviderName identifies this provider in game.Game.Provider and IDs.
const ProviderName = sources.StoreBattleNet

// productDB is one product.db to read. prefix is the wine prefix its
// install paths must be translated against — empty on native Windows,
// where product.db already holds real paths.
type productDB struct {
	path   string
	prefix string
}

// Provider discovers Battle.net games.
type Provider struct {
	// fixedDBs are product.db locations known without touching the
	// disk: the native Windows ProgramData one (prefix "") and, on
	// Linux, the fixed wine prefix Omarchy's Battle.net installer uses
	// (see roots_linux.go).
	fixedDBs []productDB
	// lutrisDBs are Lutris pga.db paths. Each one's Battle.net wine
	// prefixes are looked up at Discover time — a sqlite query, so it
	// stays out of construction, the same as the gog and epic providers.
	lutrisDBs []string
}

// New returns a Provider using this OS's default sources.
func New() *Provider { return NewWith(defaultProductDBs(), lutris.DefaultDBPaths()) }

// NewWith reads exactly the sources given. Exported for tests, which
// point it at fixture files; production code should use New.
func NewWith(fixedDBs []productDB, lutrisDBs []string) *Provider {
	return &Provider{fixedDBs: fixedDBs, lutrisDBs: lutrisDBs}
}

// Name implements platform.Provider.
func (p *Provider) Name() string { return ProviderName }

// Discover implements platform.Provider. A product.db that is missing
// (Battle.net not installed anywhere) yields nothing without an error;
// one that exists but cannot be parsed is logged and skipped — the
// agent rewrites the file while it works, so a torn read has to be
// survivable rather than fatal to the rest of discovery.
func (p *Provider) Discover(_ context.Context) ([]game.Game, error) {
	dbs := append([]productDB(nil), p.fixedDBs...)
	for _, pgaDB := range p.lutrisDBs {
		prefixes, err := lutris.BattleNetPrefixes(pgaDB)
		if err != nil {
			// A Lutris database that exists but cannot be read is worth a
			// debug line, not a failure: the gog and epic providers report
			// the same problem through their own discovery.
			slog.Debug("battlenet: lutris unreadable", "db", pgaDB, "error", err)
			continue
		}
		for _, prefix := range prefixes {
			dbs = append(dbs, productDB{
				path:   filepath.Join(prefix, "drive_c", "ProgramData", "Battle.net", "Agent", "product.db"),
				prefix: prefix,
			})
		}
	}

	var entries []sources.Entry
	for _, db := range dbs {
		products, err := readProductDB(db.path)
		if err != nil {
			slog.Debug("battlenet: product.db unreadable", "path", db.path, "error", err)
			continue
		}
		for _, prod := range products {
			// The agent and the Battle.net client itself are products
			// too. Whether a product is actually installed is decided
			// downstream by sources.Entry.Game's own stat of the install
			// path, not by the agent's cached_product_state — see the
			// package doc comment on why that field is not trusted here.
			if skippedUIDs[prod.uid] || prod.installPath == "" {
				continue
			}
			root := prod.installPath
			if db.prefix != "" {
				root = hostPath(db.prefix, root)
			}
			title := gameTitles[prod.uid]
			if title == "" {
				// A new title the table has not caught up with is still
				// a game; its folder name is the best honest label.
				title = filepath.Base(filepath.Clean(root))
			}
			entries = append(entries, sources.Entry{
				Store:   ProviderName,
				StoreID: prod.uid,
				Title:   title,
				Root:    root,
			})
		}
	}

	return sources.Games(entries), nil
}

// hostPath translates one of product.db's install paths into a real
// filesystem path. Battle.net never runs natively on Linux, only under
// wine, so on Linux every install path it records is Windows-side —
// "C:/Program Files (x86)/Diablo IV", drive letter and all, observed
// with forward slashes in the wild despite the backslash convention the
// rest of Windows uses — and has to be resolved against prefix's own
// virtual C: drive to become a path yarm can stat and plan writes into.
func hostPath(prefix, winPath string) string {
	if len(winPath) >= 2 && winPath[1] == ':' {
		winPath = winPath[2:]
	}
	parts := strings.FieldsFunc(winPath, func(r rune) bool { return r == '/' || r == '\\' })
	return filepath.Join(append([]string{prefix, "drive_c"}, parts...)...)
}
