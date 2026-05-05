//go:build windows

package tray

import (
	_ "embed"
	"log/slog"
	"os/exec"

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
		if opts.DashboardURL != "" {
			mDash := systray.AddMenuItem("Open Dashboard", "Open the Aura dashboard in your browser")
			go func() {
				for range mDash.ClickedCh {
					openBrowser(opts.DashboardURL)
				}
			}()
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

func openBrowser(rawURL string) {
	u, err := validateDashboardURL(rawURL)
	if err != nil {
		slog.Warn("tray: refusing to open dashboard url", "url", rawURL, "error", err)
		return
	}
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start(); err != nil {
		slog.Warn("tray: open dashboard failed", "url", u, "error", err)
	}
}
