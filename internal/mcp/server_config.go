package mcp

import "time"

// server_config.go holds the launch declaration for one stdio MCP server. It is a
// config shape, not a client type: internal/config/config_mcp.go builds
// map[string]ServerConfig straight from the mcpServers JSON. That is why it is
// relocated here rather than deleted with client.go in plan 45.1-03.

// ServerConfig declares how to launch one stdio MCP server (Claude-Desktop shape).
// Env entries ("KEY=value") are explicit operator-declared child environment.
type ServerConfig struct {
	Command     string        `json:"command"`
	Args        []string      `json:"args,omitempty"`
	Env         []string      `json:"env,omitempty"`
	CallTimeout time.Duration `json:"-"`
}
