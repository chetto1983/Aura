//go:build windows

package tray

import (
	_ "embed"
	"log/slog"
	"os/exec"

	"fyne.io/systray"
)

//go:embed icon_app.ico
var iconBytes []byte

//go:embed icon.ico
var fallbackIconBytes []byte

func run(opts Options) error {
	slog.Info("tray: starting", "dashboard_url", opts.DashboardURL, "primary_icon_bytes", len(iconBytes), "fallback_icon_bytes", len(fallbackIconBytes))
	onReady := func() {
		icon := chooseTrayIcon(iconBytes, fallbackIconBytes)
		systray.SetIcon(icon)
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
		slog.Info("tray: ready", "dashboard_url", opts.DashboardURL, "icon_bytes", len(icon))
	}
	systray.Run(onReady, func() {
		slog.Info("tray: exiting")
	})
	return nil
}

func stop() {
	slog.Info("tray: stop requested")
	systray.Quit()
}

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
