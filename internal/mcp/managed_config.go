package mcp

import (
	"fmt"
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

// SourceRecipeMemory marks the shared, admin-governed ArcadeDB memory MCP. The
// manager catalog stamps it onto the memory recipe so the boundary is keyed on the
// recipe, not on the server's name — an operator can rename the server without
// turning shared infrastructure into an ordinary per-identity one.
const SourceRecipeMemory = "recipe:memory"

// IsSharedAdminGoverned reports whether s is the shared memory MCP. It is the one
// server class an identity never governs: the mount is deployment-wide and only an
// admin changes it through the shared catalog.
func IsSharedAdminGoverned(s ManagedServer) bool {
	return strings.TrimSpace(s.Source) == SourceRecipeMemory
}

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
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
	Source  string   `json:"source,omitempty"`
	Type    string   `json:"type,omitempty"`
	URL     string   `json:"url,omitempty"`
	// No omitempty on these two: encoding/json ignores it for struct values, so it
	// never did anything. Dropping it is byte-identical on the wire; switching to
	// omitzero would NOT be — a zero Trust/Runtime would stop being written to the
	// registry's stored JSON, which is a format change, not a lint fix.
	Trust   ManagedTrust   `json:"trust"`
	Runtime ManagedRuntime `json:"runtime"`
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

// PrepareForWrite normalizes doc and refuses it if any server is malformed or has a launch
// declaration shaped like a backdoor. Every write to the registry goes through it.
//
// It is deliberately NOT run on reads. A shape refusal on read would let ONE planted entry
// make the whole registry unreadable and take every healthy server down with it; refusing
// the write while letting the read through keeps the spawn-time checkpoint
// (OpenSDKSessionForConfig) the loud one. See stdio_shape.go for what "backdoor-shaped"
// means and why the list is three shapes rather than a general policy.
func PrepareForWrite(doc *ManagedConfig) error {
	normalizeManagedConfig(doc)
	if err := validateManagedServers(doc.MCPServers); err != nil {
		return err
	}
	return checkManagedServersShape(doc.MCPServers)
}

// Normalize fills in a document's defaults without validating it — the read-side half, used
// where a malformed entry must not cost the caller every other server.
func Normalize(doc *ManagedConfig) {
	normalizeManagedConfig(doc)
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

// NormalizedTrust resolves a server's effective trust class. It is a thin
// wrapper over Classify (D-01): a missing server is TrustBlocked, and a
// Classify error (an ambiguous or internally-inconsistent server) falls back
// to the conservative TrustBlocked default rather than surfacing an error
// through this pre-existing error-free signature.
func (c ManagedConfig) NormalizedTrust(name string) string {
	server, ok := c.MCPServers[name]
	if !ok {
		return TrustBlocked
	}
	_, trust, err := Classify(server)
	if err != nil {
		return TrustBlocked
	}
	return trust
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

// validateManagedServers dispatches every server through Classify (D-01): an
// ambiguous or internally-inconsistent entry (mixed url+command, an unknown
// type, or an explicit type<->trust mismatch) fails validation with Classify's
// own error, so it can never be saved and never reaches OpenServer.
func validateManagedServers(in map[string]ManagedServer) error {
	memoryName := ""
	for name, cfg := range in {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("MCP managed config: server name cannot be empty")
		}
		serverType, _, err := Classify(cfg)
		if err != nil {
			return fmt.Errorf("MCP managed config: server %q: %w", name, err)
		}
		switch serverType {
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
		}
		if cfg.Trust.Class != "" && !isKnownTrust(cfg.Trust.Class) {
			return fmt.Errorf("MCP managed config: server %q has unknown trust class %q", name, cfg.Trust.Class)
		}
		if IsSharedAdminGoverned(cfg) {
			if memoryName != "" {
				return fmt.Errorf("MCP managed config: duplicate memory recipe sources %q and %q", memoryName, name)
			}
			memoryName = name
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

// normalizedServerType resolves cfg's effective transport type. It is a thin
// wrapper over Classify (D-01). Classify can reject a server outright (a mixed
// url+command entry, or an unknown/inconsistent explicit type), but this
// pre-existing signature has no error to surface that through; callers that
// must observe a rejection dispatch through Classify directly instead
// (OpenServer, validateManagedServers) rather than through this wrapper.
func normalizedServerType(cfg ManagedServer) string {
	serverType, _, err := Classify(cfg)
	if err != nil {
		return ServerTypeStdio
	}
	return serverType
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
