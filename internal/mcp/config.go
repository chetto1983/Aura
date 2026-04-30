package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// ServerConfig describes a single MCP server connection. Use Command+Args for
// stdio transport, or URL+Headers for Streamable-HTTP transport.
type ServerConfig struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// File is the JSON schema for the mcp.json config file.
//
//	{
//	  "mcpServers": {
//	    "name": { "command": "...", "args": [...] },
//	    "remote": { "url": "https://...", "headers": {"Authorization": "..."} }
//	  }
//	}
type File struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

var serverNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// LoadServers reads and validates an mcp.json file. An empty path or a missing
// file is treated as "no MCP servers configured" (returns an empty map, nil
// error) so MCP support is opt-in.
func LoadServers(path string) (map[string]ServerConfig, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]ServerConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]ServerConfig{}, nil
		}
		return nil, fmt.Errorf("read mcp config %q: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]ServerConfig{}, nil
	}
	var f File
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse mcp config %q: %w", path, err)
	}
	out := make(map[string]ServerConfig, len(f.MCPServers))
	for name, cfg := range f.MCPServers {
		if !serverNameRE.MatchString(name) {
			return nil, fmt.Errorf("mcp config: invalid server name %q (allowed: %s)", name, serverNameRE.String())
		}
		hasCmd := strings.TrimSpace(cfg.Command) != ""
		hasURL := strings.TrimSpace(cfg.URL) != ""
		if hasCmd == hasURL {
			return nil, fmt.Errorf("mcp config: server %q must set exactly one of 'command' or 'url'", name)
		}
		out[name] = cfg
	}
	return out, nil
}
