package agui

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// onboarding_qr.go renders a Telegram onboarding deep-link as a scannable QR SVG
// (ONBD-01b / D-08) using the already-vendored rsc.io/qr — NO new dependency. The QR
// encodes ONLY the t.me/<bot>?start=<onboarding-token> deep-link URL; the Telegram bot
// token is NEVER placed in the QR (or any response) — only the onboarding token (a
// single-use, 1h-TTL credential) rides in the URL. The output is a self-contained SVG the
// cockpit renders inline (no external image fetch, no DOM token injection beyond the
// deep-link URL itself).

// qrQuietZone is the white-border module count around the QR matrix (the standard quiet
// zone of 4 modules) so a scanner can lock onto the finder patterns.
const qrQuietZone = 4

// qrModulePx is the SVG user-space size of one QR module. The SVG is unitless + scales to
// its container; this only sets the viewBox proportion.
const qrModulePx = 4

// renderQRSVG encodes deepLink as a QR code (medium error correction — robust to a screen-
// photo scan) and emits a minimal self-contained SVG: a white background rect plus one
// black rect per dark module, offset by the quiet zone. An encode failure (e.g. a deep-
// link far past the QR capacity) is returned so the caller degrades gracefully (the wizard
// can still show the deep-link text). The bot token is never an input here — only the
// deep-link URL is encoded.
func renderQRSVG(deepLink string) (string, error) {
	code, err := qr.Encode(deepLink, qr.M)
	if err != nil {
		return "", fmt.Errorf("render qr: %w", err)
	}
	size := code.Size
	dim := (size + 2*qrQuietZone) * qrModulePx

	var b strings.Builder
	fmt.Fprintf(&b,
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="Telegram onboarding QR code">`,
		dim, dim, dim, dim)
	// White background (the quiet zone + the light modules).
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`, dim, dim)
	// One black rect per dark module, shifted by the quiet zone.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !code.Black(x, y) {
				continue
			}
			px := (x + qrQuietZone) * qrModulePx
			py := (y + qrQuietZone) * qrModulePx
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="#000000"/>`,
				px, py, qrModulePx, qrModulePx)
		}
	}
	b.WriteString(`</svg>`)
	return b.String(), nil
}
