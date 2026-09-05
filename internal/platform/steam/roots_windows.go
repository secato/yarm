//go:build windows

package steam

import "golang.org/x/sys/windows/registry"

// defaultRoots returns candidate Steam install directories on Windows:
// the per-user registry path, the machine-wide 32-on-64 registry path, and
// the conventional default as a last resort (docs/plan/04-external-sources.md §4.5).
func defaultRoots() []string {
	var roots []string
	if p, ok := registryString(registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"); ok {
		roots = append(roots, p)
	}
	if p, ok := registryString(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"); ok {
		roots = append(roots, p)
	}
	roots = append(roots, `C:\Program Files (x86)\Steam`)
	return roots
}

func registryString(root registry.Key, path, name string) (string, bool) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()

	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}
