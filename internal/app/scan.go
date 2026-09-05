package app

import "github.com/secato/yarm/internal/game"

// countExecutables reports how many candidate executables a folder holds,
// used to tell the user whether a path they typed looks like a game.
func countExecutables(root string) int {
	exes, err := game.Scan(root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range exes {
		if !e.Skipped {
			n++
		}
	}
	return n
}
