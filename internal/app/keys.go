package app

import "charm.land/bubbles/v2/key"

// KeyMap is every binding the UI responds to, in one place so the help
// overlay and the screens cannot drift apart.
type KeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Back    key.Binding
	Filter  key.Binding
	Rescan  key.Binding
	AddGame key.Binding
	Cache   key.Binding
	Custom  key.Binding
	Setting key.Binding
	Help    key.Binding
	Quit    key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

// DefaultKeyMap returns the standard bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Rescan:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
		AddGame: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add folder")),
		Cache:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cache")),
		Custom:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "custom")),
		Setting: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "settings")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Confirm: key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y", "confirm")),
		Cancel:  key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "cancel")),
	}
}

// ShortHelp implements help.KeyMap for the status bar.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap for the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Filter, k.Rescan, k.AddGame},
		{k.Cache, k.Custom, k.Setting},
		{k.Help, k.Quit},
	}
}
