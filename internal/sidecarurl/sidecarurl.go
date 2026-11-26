// Package sidecarurl says where Aura reaches each first-party sidecar: its Compose name inside the
// appliance (AURA_IN_CONTAINER=1), its loopback publish for a native Aura process. The MCP manager
// mounts the sidecars at these addresses and config defaults the Settings relays to them, so the
// two cannot disagree: a fixed Compose default in config once left a native Aura with a calendar
// that mounted and a mail relay with no host to dial (CI run 45864462788).
package sidecarurl

import (
	"os"
	"strconv"
	"strings"
)

// base is a sidecar's base URL. The host port comes from portEnv, validated as a TCP port
// (1-65535) before interpolation: anything else (e.g. "8096@evil.example" via the URL userinfo
// trick) would retarget the loopback-by-construction URL off-host, so garbage falls back to
// defaultPort (WR-01).
func base(containerURL, portEnv, defaultPort string) string {
	if os.Getenv("AURA_IN_CONTAINER") == "1" {
		return containerURL
	}
	port := strings.TrimSpace(os.Getenv(portEnv))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = defaultPort
	}
	return "http://127.0.0.1:" + port
}

// PIM is aura-pim-mcp's base, for MCP at its root and its /admin REST API (AURA_PIM_MCP_PORT,
// default 8093).
func PIM() string { return base("http://aura-pim-mcp:8080", "AURA_PIM_MCP_PORT", "8093") }

// Memory is the ArcadeDB memory MCP's base (AURA_ARCADEDB_MCP_PORT, default 8096).
func Memory() string {
	return base("http://aura-arcadedb-mcp:8096", "AURA_ARCADEDB_MCP_PORT", "8096")
}

// WhatsAppMCP is the WhatsApp sidecar's MCP base (AURA_WHATSAPP_MCP_PORT, default 8092).
func WhatsAppMCP() string { return base("http://whatsapp:8080", "AURA_WHATSAPP_MCP_PORT", "8092") }

// WhatsAppBridge is the WhatsApp bridge's management REST base (AURA_WHATSAPP_BRIDGE_PORT,
// default 8094).
func WhatsAppBridge() string {
	return base("http://whatsapp:8081", "AURA_WHATSAPP_BRIDGE_PORT", "8094")
}
