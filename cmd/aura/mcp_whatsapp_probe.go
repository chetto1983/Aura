package main

// mcp_whatsapp_probe.go carries the WhatsApp bridge health probe that `aura mcp doctor`
// prints beside the MCP result. Split out of mcp.go on the 600-LOC cap (CLAUDE.md
// refactor-on-touch): the probe is a self-contained concern — reach the bridge over HTTP,
// or through a WSL shim when the server command runs there — and nothing else in the MCP
// command surface calls into it.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

func writeWhatsAppBridgeHealth(ctx context.Context, out io.Writer, cfg mcp.ServerConfig) error {
	status := probeWhatsAppBridge(ctx, cfg)
	return writef(out, "whatsapp bridge: %s\n", status)
}

func probeWhatsAppBridge(ctx context.Context, cfg mcp.ServerConfig) string {
	baseURL, overridden := whatsAppBridgeBaseURL()
	if !overridden && isWSLCommand(cfg.Command) {
		status, err := runWhatsAppBridgeWSLProbe(ctx, cfg)
		if err != nil {
			return fmt.Sprintf("REST :8080 in WSL unreachable (%v)", err)
		}
		if status == http.StatusMethodNotAllowed {
			return "REST :8080 in WSL reachable (GET /api/send -> 405)"
		}
		return fmt.Sprintf("REST :8080 in WSL unexpected status (GET /api/send -> %d)", status)
	}
	return probeWhatsAppBridgeHTTP(ctx, baseURL)
}

func whatsAppBridgeBaseURL() (string, bool) {
	if v := strings.TrimSpace(os.Getenv("AURA_MCP_WHATSAPP_BRIDGE_URL")); v != "" {
		return strings.TrimRight(v, "/"), true
	}
	return mcpmanager.WhatsAppBridgeBaseURL(), false
}

func probeWhatsAppBridgeHTTP(ctx context.Context, baseURL string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/api/status"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil) //nolint:gosec // operator-owned doctor URL; default is local loopback
	if err != nil {
		return fmt.Sprintf("REST %s invalid (%v)", baseURL, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("REST %s unreachable (%v)", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("REST %s unexpected status (GET /api/status -> %d)", baseURL, resp.StatusCode)
	}
	var body struct {
		State       string `json:"state"`
		Paired      bool   `json:"paired"`
		QRAvailable bool   `json:"qr_available"`
		JID         string `json:"jid"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&body); err != nil {
		return fmt.Sprintf("REST %s reachable but status payload invalid (%v)", baseURL, err)
	}
	state := strings.TrimSpace(body.State)
	if state == "" {
		state = "unknown"
	}
	parts := []string{
		"state=" + state,
		fmt.Sprintf("paired=%t", body.Paired),
		fmt.Sprintf("qr_available=%t", body.QRAvailable),
	}
	if body.JID != "" {
		parts = append(parts, "jid="+body.JID)
	}
	return fmt.Sprintf("REST %s reachable (GET /api/status -> %s)", baseURL, strings.Join(parts, ", "))
}

var runWhatsAppBridgeWSLProbe = func(ctx context.Context, cfg mcp.ServerConfig) (int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	args := append(wslProbePrefixArgs(cfg.Args), "-e", "bash", "-lc", wslHTTPProbeScript)
	cmd := exec.CommandContext(probeCtx, cfg.Command, args...) //nolint:gosec // operator-owned MCP config; doctor reuses the configured WSL executable.
	out, err := cmd.CombinedOutput()
	if err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return 0, fmt.Errorf("%w: %s", err, s)
		}
		return 0, err
	}
	return parseHTTPStatusLine(string(out))
}

const wslHTTPProbeScript = `exec 3<>/dev/tcp/127.0.0.1/8080 || exit 111
printf 'GET /api/send HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n' >&3
IFS=$'\r' read -r line <&3
printf '%s\n' "$line"`

func isWSLCommand(command string) bool {
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(command), "\\", "/"))
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base == "wsl"
}

func wslProbePrefixArgs(args []string) []string {
	for i, arg := range args {
		if arg == "-e" || arg == "--exec" {
			return append([]string(nil), args[:i]...)
		}
	}
	return nil
}

func parseHTTPStatusLine(raw string) (int, error) {
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return 0, fmt.Errorf("missing HTTP status line in %q", strings.TrimSpace(raw))
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("decode HTTP status line %q: %w", strings.TrimSpace(raw), err)
	}
	return status, nil
}
