package mcp

import (
	"context"
	"fmt"
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
// stdio servers. It classifies the server via Classify first (D-01) and returns
// Classify's own error for a mixed/ambiguous/inconsistent entry instead of ever
// falling through to stdio Open -- the highest-priority SC1 gate: a rejected
// server must never reach a stdio subprocess spawn (D-02, closes F-027).
func OpenServer(ctx context.Context, name string, server ManagedServer) (Transport, error) {
	return OpenServerWithEgress(ctx, name, server, RuntimeEgressPolicy(false, server))
}

// OpenServerWithEgress is OpenServer with a composition-root-resolved network
// policy. Runtime entry points use it so strict profiles cannot be weakened by
// an environment toggle; direct/dev callers retain OpenServer's legacy default.
func OpenServerWithEgress(ctx context.Context, name string, server ManagedServer, egress EgressPolicy) (Transport, error) {
	serverType, _, err := Classify(server)
	if err != nil {
		return nil, fmt.Errorf("mcp open %q: %w", name, err)
	}
	if serverType == ServerTypeStreamableHTTP {
		headers, bearer := httpAuthFromEnv(server.Env)
		identityArgument := ""
		if strings.TrimSpace(server.Source) == SourceRecipeMemory {
			// The ArcadeDB MCP resolves one database and derived credential per identity.
			// This guard prevents an unscoped call from leaving Aura even if a direct
			// caller bypasses bridge injection.
			identityArgument = "user_identifier"
		}
		return OpenHTTP(ctx, name, HTTPConfig{
			URL:                  server.URL,
			Headers:              headers,
			BearerToken:          bearer,
			ToolIdentityArgument: identityArgument,
			EgressPolicy:         egress,
		})
	}
	return Open(ctx, name, ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
}

// ssrfEnforceFromEnv reads the AURA_MCP_SSRF_ENFORCE knob (AURA_<DOMAIN>_<UNIT>).
// Default OFF: an unset/empty/false value keeps lenient profiles compatible with
// local sidecars. RuntimeEgressPolicy ORs this with strict-profile enforcement,
// so the knob can tighten a lenient profile but cannot weaken a strict one.
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
