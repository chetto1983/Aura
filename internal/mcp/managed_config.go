package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManagedConfig is Aura's durable MCP server registry. It intentionally keeps the
// Claude-Desktop mcpServers shape so users can recognize and migrate config, while
// adding small Aura-owned metadata such as enabled/source.
type ManagedConfig struct {
	MCPServers map[string]ManagedServer `json:"mcpServers"`
}

// ManagedServer is one configured MCP server in Aura's local registry. When
// Enabled is nil the server is enabled, matching the least-surprising behavior for
// imported Claude-style config.
type ManagedServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
	Source  string   `json:"source,omitempty"`
}

// ManagedConfigPath returns Aura's managed MCP config path. AURA_MCP_CONFIG is a
// test/operator override; otherwise the file lives beside Aura's other user config.
func ManagedConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("AURA_MCP_CONFIG")); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".aura", "mcp", "servers.json"), nil
}

// LoadManagedConfig reads path, returning an empty registry when it does not yet
// exist. A malformed file is an error because chat/tools must not silently ignore
// operator-intended MCP servers.
func LoadManagedConfig(path string) (ManagedConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-owned config path
	if os.IsNotExist(err) {
		return ManagedConfig{MCPServers: map[string]ManagedServer{}}, nil
	}
	if err != nil {
		return ManagedConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return ManagedConfig{MCPServers: map[string]ManagedServer{}}, nil
	}
	var doc ManagedConfig
	if err := json.Unmarshal(data, &doc); err != nil {
		return ManagedConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]ManagedServer{}
	}
	return doc, nil
}

// SaveManagedConfig writes path as indented JSON with user-only permissions. MCP
// env entries commonly hold tokens, so the file should not be group/world-readable.
func SaveManagedConfig(path string, doc ManagedConfig) error {
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]ManagedServer{}
	}
	if err := validateManagedServers(doc.MCPServers); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create MCP config dir: %w", err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600) // best effort on Windows; enforced on POSIX
	return nil
}

// EnabledServers returns only enabled servers as runtime launch configs.
func (c ManagedConfig) EnabledServers() (map[string]ServerConfig, error) {
	if err := validateManagedServers(c.MCPServers); err != nil {
		return nil, err
	}
	out := make(map[string]ServerConfig)
	names := make([]string, 0, len(c.MCPServers))
	for name := range c.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := c.MCPServers[name]
		if s.Enabled != nil && !*s.Enabled {
			continue
		}
		out[name] = ServerConfig{Command: s.Command, Args: s.Args, Env: s.Env}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateManagedServers(in map[string]ManagedServer) error {
	for name, cfg := range in {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("MCP managed config: server name cannot be empty")
		}
		if strings.TrimSpace(cfg.Command) == "" {
			return fmt.Errorf("MCP managed config: server %q command cannot be empty", name)
		}
	}
	return nil
}
