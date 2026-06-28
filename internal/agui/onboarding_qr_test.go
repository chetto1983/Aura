package agui

import (
	"strings"
	"testing"
)

func TestRenderQRSVG(t *testing.T) {
	const deepLink = "https://t.me/AuraBot?start=abc123token"
	svg, err := renderQRSVG(deepLink)
	if err != nil {
		t.Fatalf("renderQRSVG: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Fatalf("not an SVG: %.60q", svg)
	}
	if !strings.Contains(svg, "<rect") {
		t.Error("QR SVG has no module rects")
	}
	if strings.Contains(svg, "123456:ABC") {
		t.Error("QR SVG leaked a bot token")
	}
}
