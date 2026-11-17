package ui

import (
	_ "embed"
	"hash/fnv"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Fonts: both are SIL OFL, licenses next to the files. Embedded so the look
// does not depend on what the system has; the families fall back to the
// system's when registering fails. Body text uses the system's own UI font.
var (
	//go:embed assets/Rajdhani-Bold.ttf
	displayTTF []byte
	//go:embed assets/JetBrainsMono-Regular.ttf
	monoTTF []byte
)

const (
	fontDisplay = "Rajdhani"
	fontMono    = "JetBrains Mono"
)

// Icons, parsed once. They take the color of the text around them.
var (
	//go:embed assets/search.svg
	searchSVG []byte
	//go:embed assets/refresh.svg
	refreshSVG []byte
	//go:embed assets/plus.svg
	plusSVG []byte
	//go:embed assets/folder.svg
	folderSVG []byte
	//go:embed assets/sliders.svg
	slidersSVG []byte
	//go:embed assets/download.svg
	downloadSVG []byte
	//go:embed assets/trash.svg
	trashSVG []byte
	//go:embed assets/check.svg
	checkSVG []byte
	//go:embed assets/warn.svg
	warnSVG []byte
	//go:embed assets/info.svg
	infoSVG []byte
	//go:embed assets/layers.svg
	layersSVG []byte
	//go:embed assets/close.svg
	closeSVG []byte
	//go:embed assets/chevron.svg
	chevronSVG []byte
	//go:embed assets/logo.svg
	logoSVG []byte

	icons struct {
		search, refresh, plus, folder, sliders, download, trash *ui.SVG
		check, warn, info, layers, close, chevron, logo         *ui.SVG
	}
	setupOnce sync.Once
)

// setup registers the fonts and parses the icons; safe to call every frame.
func setup() {
	setupOnce.Do(func() {
		_ = ui.RegisterFont(displayTTF, fontDisplay)
		_ = ui.RegisterFont(monoTTF, fontMono)
		for src, dst := range map[*[]byte]**ui.SVG{
			&searchSVG: &icons.search, &refreshSVG: &icons.refresh, &plusSVG: &icons.plus,
			&folderSVG: &icons.folder, &slidersSVG: &icons.sliders, &downloadSVG: &icons.download,
			&trashSVG: &icons.trash, &checkSVG: &icons.check, &warnSVG: &icons.warn,
			&infoSVG: &icons.info, &layersSVG: &icons.layers, &closeSVG: &icons.close,
			&chevronSVG: &icons.chevron, &logoSVG: &icons.logo,
		} {
			if s, err := ui.ParseSVG(*src); err == nil {
				*dst = s
			}
		}
	})
}

// icon draws an icon if it parsed; a missing icon is just absent.
func icon(c *ui.Context, s *ui.SVG, size float32, color ui.Color) {
	if s != nil {
		ui.Icon(c, s).Width(size).Height(size).TextColor(color)
	}
}

// palette is every color of the look, by what it means. The accent (brand
// violet) always means "this will be installed, or is selected"; green is
// installed, amber is a warning or something in the way, cyan is
// information, red is removed or failed. There is one dark and one light
// palette; the accent does not follow the desktop, so the brand stays the
// same everywhere.
type palette struct {
	dark                                          bool
	ground, panel, card, cardOn, line, lineStrong ui.Color
	surface, surfaceHover, surfacePressed         ui.Color
	text, muted, faint                            ui.Color
	// accent is violet as text, border and focus ring; accentFill1/2 are
	// the gradient of a primary button; onAccent is the text on it.
	accent, accentFill1, accentFill2, accentDim, accentGlow, onAccent ui.Color
	amber, amberDim, amberBorder                                      ui.Color
	cyan, cyanDim, cyanBorder                                         ui.Color
	green, greenDim, greenBorder                                      ui.Color
	red, redDim, redBorder                                            ui.Color
	code                                                              ui.Color
	// shadow is the opacity of the dock's and dialogs' shadow.
	shadow float32
}

var darkPalette = palette{
	dark:   true,
	ground: ui.Hex("#0B0A12"), panel: ui.Hex("#100E1A"), card: ui.Hex("#14111F"), cardOn: ui.Hex("#1A1530"),
	line: ui.Hex("#221E33"), lineStrong: ui.Hex("#342E4D"),
	surface: ui.Hex("#1B1730"), surfaceHover: ui.Hex("#231E3D"), surfacePressed: ui.Hex("#2B2549"),
	text: ui.Hex("#ECEAF6"), muted: ui.Hex("#A19BC0"), faint: ui.Hex("#8A84AA"),
	accent: ui.Hex("#A78BFA"), accentFill1: ui.Hex("#7C3AED"), accentFill2: ui.Hex("#6D28D9"),
	accentDim: ui.Hex("#2A1F52"), accentGlow: ui.RGBA(139, 92, 246, 0.45), onAccent: ui.Hex("#FFFFFF"),
	amber: ui.Hex("#FFB454"), amberDim: ui.Hex("#2A1D07"), amberBorder: ui.Hex("#6B4A12"),
	cyan: ui.Hex("#3DD9FF"), cyanDim: ui.Hex("#102A33"), cyanBorder: ui.Hex("#1B4452"),
	green: ui.Hex("#4ADE80"), greenDim: ui.Hex("#0F2A1A"), greenBorder: ui.Hex("#1F5A36"),
	red: ui.Hex("#FF7A7A"), redDim: ui.Hex("#2A1114"), redBorder: ui.Hex("#5C2329"),
	code:   ui.Hex("#08070D"),
	shadow: 0.55,
}

// lightPalette keeps the same meanings on a pale ground: the bright colors of
// the dark look become deep ones that read as text, and the dark tints become
// pale ones.
var lightPalette = palette{
	ground: ui.Hex("#F1EFF8"), panel: ui.Hex("#F7F5FC"), card: ui.Hex("#FFFFFF"), cardOn: ui.Hex("#F1EBFF"),
	line: ui.Hex("#DDD8EC"), lineStrong: ui.Hex("#BDB5D8"),
	surface: ui.Hex("#FFFFFF"), surfaceHover: ui.Hex("#F0ECFA"), surfacePressed: ui.Hex("#E4DEF5"),
	text: ui.Hex("#171326"), muted: ui.Hex("#524B70"), faint: ui.Hex("#5E5880"),
	accent: ui.Hex("#6D28D9"), accentFill1: ui.Hex("#7C3AED"), accentFill2: ui.Hex("#6D28D9"),
	accentDim: ui.Hex("#E6DCFF"), accentGlow: ui.RGBA(109, 40, 217, 0.28), onAccent: ui.Hex("#FFFFFF"),
	amber: ui.Hex("#8F5000"), amberDim: ui.Hex("#FFF1DA"), amberBorder: ui.Hex("#E6B567"),
	cyan: ui.Hex("#00728F"), cyanDim: ui.Hex("#DDF3F9"), cyanBorder: ui.Hex("#8CCFE0"),
	green: ui.Hex("#15803D"), greenDim: ui.Hex("#E2F5E8"), greenBorder: ui.Hex("#8CCFA3"),
	red: ui.Hex("#C0272F"), redDim: ui.Hex("#FCEBEC"), redBorder: ui.Hex("#EDA5A9"),
	code:   ui.Hex("#E9E5F5"),
	shadow: 0.18,
}

// pal is the palette of the frame being drawn; View sets it at the top of
// every frame from the desktop's appearance, so the rest of the code just
// reads it.
var pal = darkPalette

// paletteFor is the palette for a dark or light window.
func paletteFor(dark bool) palette {
	if dark {
		return darkPalette
	}
	return lightPalette
}

// yarmTheme is the theme every widget inherits, built on pal.
func yarmTheme() *ui.Theme {
	t := ui.LightTheme()
	if pal.dark {
		t = ui.DarkTheme()
	}
	t.Background = pal.ground
	t.Surface, t.SurfaceHover, t.SurfacePressed = pal.surface, pal.surfaceHover, pal.surfacePressed
	t.Border = pal.lineStrong
	t.Text, t.TextMuted = pal.text, pal.muted
	t.Accent, t.AccentHover, t.AccentPressed = pal.accentFill1, pal.accentFill1.Mix(ui.RGB(255, 255, 255), 0.15), pal.accentFill2
	t.AccentText = pal.onAccent
	t.Warning, t.Success = pal.amber, pal.green
	t.Focus = pal.accent
	t.Radius = 10
	return t
}

func gameHue(name string) float32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return float32(h.Sum32() % 360)
}

// washColor is the tint of a game's header: its tile's color, faded toward
// the window's.
func washColor(name string) ui.Color {
	if pal.dark {
		return ui.Oklch(0.27, 0.07, gameHue(name))
	}
	return ui.Oklch(0.90, 0.05, gameHue(name))
}

// tileColors gives each game its own tile gradient, from its name, so the
// list is easy to scan.
func tileColors(name string) (from, to ui.Color) {
	hue := gameHue(name)
	return ui.Oklch(0.52, 0.13, hue), ui.Oklch(0.30, 0.09, hue)
}

func ternary[T any](ok bool, a, b T) T {
	if ok {
		return a
	}
	return b
}
