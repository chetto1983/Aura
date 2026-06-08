package setup

// qrSVG is the DEFERRED qr_svg body (OQ4 / research A5). The HTML frontend is
// deferred to the next milestone (D-03), so the SVG generator is not built this
// phase — the JSON field stays in the contract for forward-compat returning the
// empty string. The terminal ASCII QR (qrterminal.Generate in handleOnboardLink)
// is the live onboarding path this phase. When the frontend lands this returns a
// real SVG (boombuler/barcode is the maintained generator; skip2/go-qrcode is
// rejected as a 2020 pseudo-version).
func qrSVG(deepLink string) string {
	_ = deepLink // forward-compat: the SVG body is deferred (OQ4), not the field
	return ""
}
