package ui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run opens the window and blocks until it closes.
func Run(opts Options) error {
	app := New(opts)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "YARM — ReShade manager",
			Width:     1180,
			Height:    860,
			MinWidth:  900,
			MinHeight: 600,
			Content:   ui.View(app.View),
		})
		app.Start(win)
	})
	return mygo.App.Run()
}
