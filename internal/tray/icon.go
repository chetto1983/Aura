//go:build windows

package tray

func chooseTrayIcon(primary, fallback []byte) []byte {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}
