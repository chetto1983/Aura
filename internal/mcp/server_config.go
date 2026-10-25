package mcp

import (
	"errors"
	"slices"
	"time"
)

// BrowserRecipeSource grants only the fixed browser command a dedicated sandbox.
const BrowserRecipeSource = "recipe:browser"

// server_config.go holds the launch declaration for one stdio MCP server. It is a
// config shape, not a client type: internal/config/config_mcp.go builds
// map[string]ServerConfig straight from the mcpServers JSON. That is why it is
// relocated here rather than deleted with client.go in plan 45.1-03.

// ServerConfig declares how to launch one stdio MCP server (Claude-Desktop shape).
// Env entries ("KEY=value") are explicit operator-declared child environment.
type ServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	// Box runs the server in the calling identity's sandbox box instead of on the host
	// (RuntimeKindBox). It comes from the registry's runtime kind, never from this JSON.
	Box bool `json:"-"`
	// Browser is set only by the trusted, fixed browser recipe resolver, never JSON.
	Browser bool `json:"-"`
	// InitTimeout is a box server's first-start budget (BoxInitTimeout).
	InitTimeout time.Duration `json:"-"`
	// Dir and DataDir are a box server's in-box directories (ManagedRuntime).
	Dir     string `json:"-"`
	DataDir string `json:"-"`
}

// ManagedStdioConfig keeps direct probes and managed mounts on the same isolation policy.
func ManagedStdioConfig(server ManagedServer) (ServerConfig, error) {
	_, trust, err := Classify(server)
	if err != nil {
		return ServerConfig{}, err
	}
	browser := server.Source == BrowserRecipeSource && trust == TrustTrustedRecipe
	if browser && (server.Command != "agent-browser" || !slices.Equal(server.Args, []string{"mcp"}) || len(server.Env) != 0 || !IsBoxRuntime(server)) {
		return ServerConfig{}, errors.New("browser recipe requires its fixed box command and no custom environment")
	}
	return ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env,
		Box: IsBoxRuntime(server), InitTimeout: BoxInitTimeout(server), Browser: browser,
		Dir: server.Runtime.Dir, DataDir: server.Runtime.DataDir}, nil
}
