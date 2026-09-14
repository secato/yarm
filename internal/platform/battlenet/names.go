package battlenet

// gameTitles maps Battle.net product uids — the uninstall tag
// product.db carries, stable across the agent's versions — to display
// names. The table is hand-kept, from the same sources Lutris and
// dlss-swapper keep theirs: the uid set only changes when Blizzard
// ships a new title, and a uid missing here falls back to the install
// folder's name rather than hiding the game.
var gameTitles = map[string]string{
	// Blizzard classics and current titles.
	"wow":             "World of Warcraft",
	"wow_classic":     "World of Warcraft Classic",
	"wow_classic_era": "World of Warcraft Classic Era",
	"prometheus":      "Overwatch 2",
	"fenris":          "Diablo IV",
	"diablo3":         "Diablo III",
	"d3":              "Diablo III",
	"d3cn":            "暗黑破壞神III",
	"osi":             "Diablo II: Resurrected",
	"d1":              "Diablo",
	"heroes":          "Heroes of the Storm",
	"hero":            "Heroes of the Storm",
	"hs_beta":         "Hearthstone",
	"hsb":             "Hearthstone",
	"s1":              "StarCraft",
	"s2":              "StarCraft II",
	"w1":              "Warcraft: Orcs & Humans",
	"w1r":             "Warcraft I: Remastered",
	"w2":              "Warcraft II: Battle.net Edition",
	"w2bn":            "Warcraft II: Battle.net Edition",
	"w2r":             "Warcraft II: Remastered",
	"w3":              "Warcraft III: Reforged",
	"rtro":            "Blizzard Arcade Collection",
	"wlby":            "Crash Bandicoot 4: It's About Time",

	// Call of Duty, and the other publishers' games Battle.net sells.
	"auks":    "Call of Duty",
	"codhq":   "Call of Duty HQ",
	"nina":    "Call of Duty: Modern Warfare II",
	"odin":    "Call of Duty: Modern Warfare",
	"pinta":   "Call of Duty: Modern Warfare III",
	"lazarus": "Call of Duty: MW2 Campaign Remastered",
	"zeus":    "Call of Duty: Black Ops Cold War",
	"viper":   "Call of Duty: Black Ops 4",
	"fore":    "Call of Duty: Vanguard",
	"scor":    "Sea of Thieves",
	"aqua":    "Avowed",
	"aris":    "Doom: The Dark Ages",
	"lbra":    "Tony Hawk's Pro Skater 3+4",
	"ark":     "The Outer Worlds 2",
}

// skippedUIDs are the Battle.net agent and client apps that appear in
// product.db as products but are not games anyone would install
// ReShade into.
var skippedUIDs = map[string]bool{
	"agent":      true, // the agent itself
	"agent_beta": true,
	"beta":       true, // the beta client
	"bna":        true, // the Battle.net client app
	"battle.net": true,
}
