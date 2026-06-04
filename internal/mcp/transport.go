package mcp

import (
	"context"
	"strings"
)

type Transport interface {
	ListTools(context.Context) ([]ToolDef, error)
	CallTool(context.Context, string, map[string]any) (string, error)
	Ping(context.Context) error
	Close() error
}

func OpenServer(ctx context.Context, name string, server ManagedServer) (Transport, error) {
	if normalizedServerType(server) == ServerTypeStreamableHTTP {
		headers, bearer := httpAuthFromEnv(server.Env)
		return OpenHTTP(ctx, name, HTTPConfig{URL: server.URL, Headers: headers, BearerToken: bearer})
	}
	return Open(ctx, name, ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
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
