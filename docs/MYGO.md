# MyGo notes (as used in yarm)

Module `github.com/egoist/mygo` v0.7.4, native `ui` package (GPU drawn, no webview, no cgo).
Docs: https://github.com/egoist/mygo/tree/main/docs/ui. The repo's HEAD docs can be newer than
v0.7.4; check the real API with `go doc github.com/egoist/mygo/ui <Symbol>` before using one.
The same library and look as the owner's `slom` project, which is a good place to see a
pattern worked out.

## Patterns
- Immediate mode: `App.View(c *ui.Context)` rebuilds every frame from `App` state. Widgets take
  pointers (`ui.Switch(c, &on)`, `ui.Select(c, &label, labels)`) and report `Changed()`. Bind to a
  local copy of the model value, then call the model on `Changed()`: the switch writes the copy, the
  model decides (`Session.TogglePackage`) and the next frame reads the model again. Select labels
  differ from values; the view maps label to value.
- Element values are valid for one build pass; never keep them in `App`. A persistent handle
  (`ui.Handle`, bound with `.Bind(&a.search)`) is how `/` and Ctrl+F reach the search box.
- Keys: `c.Key(x)` before a stateful widget. A keyed parent scopes its children; give repeated
  cards their own key (`card(c, key, …)`, `c.Key("game-"+id)`).
- Tooltips: `Element.Tooltip(text)`, after a short hover. A disabled control shows none.
- Dialogs: `ui.Modal(c, &open, fn)` (our `modal` wrapper gives it the panel look);
  `ui.AlertDialog(c, &open, title, msg, buttons...)` returns the index clicked. Esc closes a modal.
  Toasts: `c.Toast(msg)`.
- Shortcuts: `c.Shortcut(ui.Cmd, ui.KeyR)`; `ui.Cmd` is Ctrl on Linux. They wait while a dialog is open.
- Background work: `App.bg(work, done)` runs `work` on a goroutine and `done` on the main thread via
  `mygo.RunOnMain`, then invalidates. `mygo.Dialog.Open` blocks until the person chooses, so it runs in
  `bg` too (`App.pick`).
- Custom widgets are bases with our own look: `ButtonBase` (`button`, `segment`, game rows, cards),
  `TabsBase` with `TabsParts.Tab(i)` (bottom border: `BorderWidth(0,0,2,0).BorderColor`). Query
  `Hovered()` before `Children` to restyle on hover; read `Clicked()` after `Children`.
- Layout: `ui.Grid(c).Columns(2)` with `ColumnSpan(-1)`; `Row(c).Wrap()` for pill rows; `MinWidth(0)` on a
  growing column so long content does not overflow; `ui.Scroll(c).Grow(1)`. In a `Row`, children are
  centered vertically: use `FillHeight()` (not `Fill()`, which also takes the width) for a column that
  should be as tall as the window.
- Look: `Gradient`, `Shadow(0,0,blur,0,color)` for glow, `Opacity(0.4)` for disabled, `ui.Icon` with an
  embedded SVG (`ui.ParseSVG` once, in `setup()`) in the text color.
- Fonts are embedded (`go:embed`, OFL licenses next to them in `internal/ui/assets/`) and registered
  once in `setup()`. Use `Font("Rajdhani, system-ui")` so a failed registration falls back.
- Theme: at the top of every frame `View` sets `pal = a.look(c)` and then `c.SetTheme(yarmTheme())`.
  `c.Theme().Dark` read before `SetTheme` is the system's appearance. Colors live in `pal`, named by
  meaning (see [architecture/gui.md](architecture/gui.md#the-look)).

## Testing with `ui.NewTester(view, w, h)`
Methods: `Click("text")`, `HasText`, `Texts`, `Find`, `Key(mods, key)`, `Advance(d)` (fake clock),
`Frame`, `Image()`. Text-input contents are not in `Texts()`: assert on the model.
- `Click` finds the first element with that text or label. The sidebar repeats names ("Resources",
  "Steam", a game's name, "Add a game"), so give a control a distinct `Label` when its name repeats, and
  name the sidebar's icon buttons differently from a dialog's confirm button.
- After changing the model directly, call `tt.Frame()` before the next `Click`: rows move, and the click
  would land on the old layout.
- Switches slide and dialogs fade. `Advance(time.Second)` before `Image()`, or a switch is drawn half way.
- `testApp` runs background work inline (`spawn` and `onMain` are fields), so a click and its
  consequences are over when `Click` returns. To watch something mid-run, set the model's state directly.
- A cache or config directory the code expects must exist in the temp dir (`os.MkdirAll`).
- `YARM_SHOTS=<dir>` makes `shot` write PNGs; `scripts/screenshots.sh` makes the README pictures.
