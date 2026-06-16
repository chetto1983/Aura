//go:build integrations_integration

// Live integrations-proxy tier: drives BOTH managed-sidecar legs (calendar PIM +
// WhatsApp bridge) end-to-end through Aura's actual newIntegrationsProxy() handler
// against the running containers. Proves the cockpit's connect data plane works for
// both MCP backends without the cockpit ever holding a sidecar token.
//
// No-skip-as-green (CLAUDE.md): with the sidecar ports unset under $CI this t.Fatals;
// locally it t.Skips. Bring both sidecars up first (see cmd: docker run the published
// GHCR images), then:
//
//	AURA_PIM_MCP_PORT=8093 AURA_PIM_MCP_ADMIN_TOKEN=<tok> AURA_WHATSAPP_BRIDGE_PORT=8094 \
//	  go test -tags integrations_integration -run TestIntegrationsProxyLiveBothMCP ./cmd/aura/
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func liveIntegrationsOrGate(t *testing.T) {
	t.Helper()
	pim := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_PORT"))
	wa := strings.TrimSpace(os.Getenv("AURA_WHATSAPP_BRIDGE_PORT"))
	if pim != "" && wa != "" {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_PIM_MCP_PORT + AURA_WHATSAPP_BRIDGE_PORT must be set under CI — a skipped integrations_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
	}
	t.Skip("set AURA_PIM_MCP_PORT + AURA_WHATSAPP_BRIDGE_PORT (+ AURA_PIM_MCP_ADMIN_TOKEN) and bring both sidecars up to run the integrations_integration tier")
}

// getThrough drives a GET through the live proxy and returns status + body.
func getThrough(t *testing.T, base, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestIntegrationsProxyLiveBothMCP(t *testing.T) {
	liveIntegrationsOrGate(t)
	reapProxyIdleConns(t)

	ts := httptest.NewServer(newIntegrationsProxy())
	t.Cleanup(ts.Close)

	// calendar leg: the PIM /admin/accounts read (token injected by the proxy). Clean
	// even with zero connected accounts.
	t.Run("calendar/accounts", func(t *testing.T) {
		code, body := getThrough(t, ts.URL, "/api/integrations/calendar/accounts")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", code, body)
		}
		if !strings.Contains(body, "accounts") {
			t.Fatalf("calendar accounts payload missing 'accounts': %s", body)
		}
	})

	// whatsapp leg: the bridge /api/status (no token). The bridge is REST-first so this
	// answers whether or not a device is linked.
	t.Run("whatsapp/status", func(t *testing.T) {
		code, body := getThrough(t, ts.URL, "/api/integrations/whatsapp/status")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", code, body)
		}
		if !strings.Contains(body, "state") {
			t.Fatalf("whatsapp status payload missing 'state': %s", body)
		}
	})

	// whatsapp leg: the QR endpoint. 200 {code} when waiting, 409 when already paired,
	// 503 before the first QR rotates in — all are valid live states.
	t.Run("whatsapp/qr", func(t *testing.T) {
		code, body := getThrough(t, ts.URL, "/api/integrations/whatsapp/qr")
		switch code {
		case http.StatusOK, http.StatusConflict, http.StatusServiceUnavailable:
		default:
			t.Fatalf("status = %d, want 200/409/503; body=%s", code, body)
		}
	})
}
