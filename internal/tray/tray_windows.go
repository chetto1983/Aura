//go:build windows

package tray

import (
	_ "embed"

	"fyne.io/systray"
)

//go:embed icon.ico
var iconBytes []byte

func run(opts Options) error {
	onReady := func() {
		systray.SetIcon(iconBytes)
		if opts.Title != "" {
			systray.SetTitle(opts.Title)
		}
		if opts.Tooltip != "" {
			systray.SetTooltip(opts.Tooltip)
		}
		if opts.Version != "" {
			header := systray.AddMenuItem("Aura "+opts.Version, "")
			header.Disable()
			systray.AddSeparator()
		}
		mQuit := systray.AddMenuItem("Quit Aura", "Stop the bot and exit")
		go func() {
			<-mQuit.ClickedCh
			systray.Quit()
		}()
	}
	systray.Run(onReady, func() {})
	return nil
}

func stop() { systray.Quit() }
