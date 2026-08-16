package mcp

import (
	"context"
	"fmt"
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

// ssrfEnforceFromEnv and httpAuthFromEnv moved to sdkclient.go: they outlive this
// file, which is deleted in plan 45.1-03.
