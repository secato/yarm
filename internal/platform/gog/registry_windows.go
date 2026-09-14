//go:build windows

package gog

import (
	"golang.org/x/sys/windows/registry"
)

// gamesKey is where every GOG installer — Galaxy or the offline kind —
// records the games it installed, one subkey per game. The 32-on-64
// view: GOG's installers are 32-bit and write there.
const gamesKey = `SOFTWARE\WOW6432Node\GOG.com\Games`

func defaultRegistry() registrySource { return windowsRegistry{} }

// windowsRegistry reads GOG's per-game subkeys.
type windowsRegistry struct{}

func (windowsRegistry) games() ([]gogGame, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, gamesKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		// GOG not installed is the normal case, not an error.
		return nil, nil
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	var out []gogGame
	for _, name := range names {
		gk, err := registry.OpenKey(k, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		id, _, _ := gk.GetStringValue("gameID")
		gname, _, _ := gk.GetStringValue("gameName")
		path, _, _ := gk.GetStringValue("path")
		dependsOn, _, _ := gk.GetStringValue("dependsOn")
		_ = gk.Close()

		if id == "" || path == "" {
			continue
		}
		out = append(out, gogGame{id: id, name: gname, path: path, dependsOn: dependsOn})
	}
	return out, nil
}
