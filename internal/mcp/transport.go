package mcp

import (
	"context"
	"os"
	"strings"
)

// Transport is the common interface over an MCP server connection, implemented by
// both the stdio Client and the Streamable-HTTP HTTPClient.
type Transport interface {
	ListTools(context.Context) ([]ToolDef, error)
	CallTool(context.Context, string, map[string]any) (string, error)
	Ping(context.Context) error
	Close() error
}

// OpenServer opens the appropriate transport for a managed server, dispatching to
// OpenHTTP for streamable-HTTP servers (deriving auth from env) and to Open for
// stdio servers.
func OpenServer(ctx context.Context, name string, server ManagedServer) (Transport, error) {
	if normalizedServerType(server) == ServerTypeStreamableHTTP {
		headers, bearer := httpAuthFromEnv(server.Env)
		return OpenHTTP(ctx, name, HTTPConfig{
			URL:         server.URL,
			Headers:     headers,
			BearerToken: bearer,
			Enforce:     ssrfEnforceFromEnv(),
		})
	}
	return Open(ctx, name, ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
}

// ssrfEnforceFromEnv reads the AURA_MCP_SSRF_ENFORCE knob (AURA_<DOMAIN>_<UNIT>).
// Default OFF: an unset/empty/false value keeps dev byte-behaviour-identical
// (loopback + private compose-DNS sidecars reachable, http.DefaultClient retained).
// Phase 33 (PROF-01/PROF-04) will bind this to the runtime profile.
func ssrfEnforceFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AURA_MCP_SSRF_ENFORCE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func httpAuthFromEnv(env []string) (map[string]string, string) {
	headers := map[string]string{}
	bearer := ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch {
		case key == "MCP_BEARER_TOKEN":
			bearer = value
		case strings.HasPrefix(key, "MCP_HEADER_"):
			header := strings.ReplaceAll(strings.TrimPrefix(key, "MCP_HEADER_"), "_", "-")
			if header != "" {
				headers[header] = value
			}
		}
	}
	if len(headers) == 0 {
		headers = nil
	}
	return headers, bearer
}
