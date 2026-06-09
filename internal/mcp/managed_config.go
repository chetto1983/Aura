package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Managed config schema constants: the registry version, default profile name, and
// the recognized server type, trust class, and runtime kind enum values.
const (
	ManagedConfigVersion = 2
	DefaultMCPProfile    = "default"

	ServerTypeStdio          = "stdio"
	ServerTypeStreamableHTTP = "streamable_http"

	TrustTrustedRecipe  = "trusted_recipe"
	TrustTrustedLocal   = "trusted_local"
	TrustSandboxedLocal = "sandboxed_local"
	TrustRemoteHTTP     = "remote_http"
	TrustBlocked        = "blocked"

	RuntimeKindLocal         = "local"
	RuntimeKindDocker        = "docker"
	RuntimeKindDockerGateway = "docker_gateway"
)

// ManagedConfig is Aura's durable MCP server registry. It intentionally keeps the
// Claude-Desktop mcpServers shape so users can recognize and migrate config, while
// adding small Aura-owned metadata such as enabled/source.
type ManagedConfig struct {
	Version       int                       `json:"version,omitempty"`
	ActiveProfile string                    `json:"activeProfile,omitempty"`
	Profiles      map[string]ManagedProfile `json:"profiles,omitempty"`
	MCPServers    map[string]ManagedServer  `json:"mcpServers"`
}

// ManagedProfile is a named selection of servers, letting an operator scope
// which MCP servers are active at once.
type ManagedProfile struct {
	Servers []string `json:"servers,omitempty"`
}

// ManagedServer is one configured MCP server in Aura's local registry. When
// Enabled is nil the server is enabled, matching the least-surprising behavior for
// imported Claude-style config.
type ManagedServer struct {
	Command string         `json:"command"`
	Args    []string       `json:"args,omitempty"`
	Env     []string       `json:"env,omitempty"`
	Enabled *bool          `json:"enabled,omitempty"`
	Source  string         `json:"source,omitempty"`
	Type    string         `json:"type,omitempty"`
	URL     string         `json:"url,omitempty"`
	Trust   ManagedTrust   `json:"trust,omitempty"`
	Runtime ManagedRuntime `json:"runtime,omitempty"`
}

// ManagedTrust records the trust class assigned to a server and the audit trail for
// that decision (who approved it, when, and why).
type ManagedTrust struct {
	Class      string `json:"class,omitempty"`
	ApprovedBy string `json:"approvedBy,omitempty"`
	ApprovedAt string `json:"approvedAt,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// ManagedRuntime describes how a stdio server is launched (local process, Docker
// container, or Docker gateway) and the container isolation knobs that apply.
type ManagedRuntime struct {
	Kind    string   `json:"kind,omitempty"`
	Image   string   `json:"image,omitempty"`
	Command []string `json:"command,omitempty"`
	Mounts  []string `json:"mounts,omitempty"`
	Network []string `json:"network,omitempty"`
	CPUs    string   `json:"cpus,omitempty"`
	Memory  string   `json:"memory,omitempty"`
	Profile string   `json:"profile,omitempty"`
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
	normalizeManagedConfig(&doc)
	return doc, nil
}

// SaveManagedConfig writes path as indented JSON with user-only permissions. MCP
// env entries commonly hold tokens, so the file should not be group/world-readable.
func SaveManagedConfig(path string, doc ManagedConfig) error {
	normalizeManagedConfig(&doc)
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
		if normalizedServerType(s) == ServerTypeStreamableHTTP {
			continue
		}
		out[name] = ServerConfig{Command: s.Command, Args: s.Args, Env: s.Env}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ActiveProfileName returns the configured active profile, falling back to
// DefaultMCPProfile when none is set.
func (c ManagedConfig) ActiveProfileName() string {
	if strings.TrimSpace(c.ActiveProfile) != "" {
		return strings.TrimSpace(c.ActiveProfile)
	}
	return DefaultMCPProfile
}

// ProfileServerNames returns the sorted, de-duplicated server names selected by the
// given profile (defaulting to the active profile); when the profile is undefined
// it falls back to all enabled servers.
func (c ManagedConfig) ProfileServerNames(profile string) []string {
	if strings.TrimSpace(profile) == "" {
		profile = c.ActiveProfileName()
	}
	if p, ok := c.Profiles[profile]; ok {
		names := make([]string, 0, len(p.Servers))
		seen := map[string]struct{}{}
		for _, name := range p.Servers {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := c.MCPServers[name]; !ok {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}

	names := make([]string, 0, len(c.MCPServers))
	for name, server := range c.MCPServers {
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NormalizedTrust resolves a server's effective trust class, inferring it from the
// recipe source or HTTP type when unset and defaulting to TrustBlocked for unknown
// or missing servers.
func (c ManagedConfig) NormalizedTrust(name string) string {
	server, ok := c.MCPServers[name]
	if !ok {
		return TrustBlocked
	}
	if isKnownTrust(server.Trust.Class) {
		return server.Trust.Class
	}
	if strings.HasPrefix(strings.TrimSpace(server.Source), "recipe:") {
		return TrustTrustedRecipe
	}
	if normalizedServerType(server) == ServerTypeStreamableHTTP {
		return TrustRemoteHTTP
	}
	return TrustBlocked
}

func normalizeManagedConfig(doc *ManagedConfig) {
	if doc.Version == 0 {
		doc.Version = ManagedConfigVersion
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]ManagedServer{}
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]ManagedProfile{}
	}
}

func validateManagedServers(in map[string]ManagedServer) error {
	for name, cfg := range in {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("MCP managed config: server name cannot be empty")
		}
		switch normalizedServerType(cfg) {
		case ServerTypeStdio:
			switch normalizedRuntimeKind(cfg) {
			case RuntimeKindLocal:
				if strings.TrimSpace(cfg.Command) == "" {
					return fmt.Errorf("MCP managed config: server %q command cannot be empty", name)
				}
			case RuntimeKindDocker:
				if strings.TrimSpace(cfg.Runtime.Image) == "" {
					return fmt.Errorf("MCP managed config: server %q docker image cannot be empty", name)
				}
			case RuntimeKindDockerGateway:
				if strings.TrimSpace(cfg.Runtime.Profile) == "" {
					return fmt.Errorf("MCP managed config: server %q docker gateway profile cannot be empty", name)
				}
			default:
				return fmt.Errorf("MCP managed config: server %q has unknown runtime kind %q", name, cfg.Runtime.Kind)
			}
		case ServerTypeStreamableHTTP:
			if strings.TrimSpace(cfg.URL) == "" {
				return fmt.Errorf("MCP managed config: server %q url cannot be empty", name)
			}
		default:
			return fmt.Errorf("MCP managed config: server %q has unknown type %q", name, cfg.Type)
		}
		if cfg.Trust.Class != "" && !isKnownTrust(cfg.Trust.Class) {
			return fmt.Errorf("MCP managed config: server %q has unknown trust class %q", name, cfg.Trust.Class)
		}
	}
	return nil
}

func normalizedRuntimeKind(cfg ManagedServer) string {
	switch strings.TrimSpace(cfg.Runtime.Kind) {
	case "":
		return RuntimeKindLocal
	case RuntimeKindLocal:
		return RuntimeKindLocal
	case RuntimeKindDocker:
		return RuntimeKindDocker
	case RuntimeKindDockerGateway:
		return RuntimeKindDockerGateway
	default:
		return strings.TrimSpace(cfg.Runtime.Kind)
	}
}

func normalizedServerType(cfg ManagedServer) string {
	switch strings.TrimSpace(cfg.Type) {
	case "":
		if strings.TrimSpace(cfg.URL) != "" && strings.TrimSpace(cfg.Command) == "" {
			return ServerTypeStreamableHTTP
		}
		return ServerTypeStdio
	case ServerTypeStdio:
		return ServerTypeStdio
	case ServerTypeStreamableHTTP:
		return ServerTypeStreamableHTTP
	default:
		return strings.TrimSpace(cfg.Type)
	}
}

func isKnownTrust(class string) bool {
	switch strings.TrimSpace(class) {
	case TrustTrustedRecipe, TrustTrustedLocal, TrustSandboxedLocal, TrustRemoteHTTP, TrustBlocked:
		return true
	default:
		return false
	}
}

// IsKnownTrust reports whether class is a recognized trust class. It lets callers
// outside this package (e.g. the manager runtime) gate an explicit Trust.Class the
// same way NormalizedTrust does, instead of trusting an arbitrary string.
func IsKnownTrust(class string) bool {
	return isKnownTrust(class)
}
