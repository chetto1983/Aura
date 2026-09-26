package mcp

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/secret"
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

	// RuntimeKindLocal starts a stdio server as a child process of Aura. `docker` and
	// `docker_gateway` were retired by amendment #209.
	RuntimeKindLocal = "local"
	// RuntimeKindBox starts a stdio server inside the calling identity's sandbox box, one
	// process per identity, reached over an exec's stdin and stdout (box_session.go).
	RuntimeKindBox = "box"
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

// ManagedRuntime carries a stdio server's launch declaration: where it runs (Kind: a local
// child process, or the calling identity's sandbox box). Image, CPUs, Memory and Profile
// left with the docker kinds they belonged to (amendment #209), and Network followed when
// the board stopped displaying it (#210) — its only enforcement had been the docker
// builder, so what remained was a claim nothing backed. A stored row carrying any of those
// keys loses them on its next read-modify-write, which is correct once nothing acts on
// them. Command and Mounts stay because stdio_shape.go assembles them into the argv it
// scans for backdoor shapes, whatever wrote them.
type ManagedRuntime struct {
	Kind    string   `json:"kind,omitempty"`
	Command []string `json:"command,omitempty"`
	Mounts  []string `json:"mounts,omitempty"`
	// InitTimeoutSec is the budget, in seconds, for a box server's first start in an
	// identity's box, when `npx -y` or `uvx` fetch the server into that identity's caches
	// (BoxInitTimeout). Box servers only; unset means DefaultBoxInitTimeout.
	InitTimeoutSec int `json:"initTimeoutSec,omitempty"`
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
			if err := validateStdioRuntime(name, cfg); err != nil {
				return err
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

func validateStdioRuntime(name string, cfg ManagedServer) error {
	kind := normalizedRuntimeKind(cfg)
	if kind != RuntimeKindLocal && kind != RuntimeKindBox {
		return fmt.Errorf("MCP managed config: server %q has unknown runtime kind %q", name, cfg.Runtime.Kind)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return fmt.Errorf("MCP managed config: server %q command cannot be empty", name)
	}
	switch {
	case kind != RuntimeKindBox && cfg.Runtime.InitTimeoutSec != 0:
		return fmt.Errorf("MCP managed config: server %q declares runtime.initTimeoutSec, which only a box server's first start uses", name)
	case cfg.Runtime.InitTimeoutSec < 0 || cfg.Runtime.InitTimeoutSec > maxBoxInitTimeoutSec:
		return fmt.Errorf("MCP managed config: server %q runtime.initTimeoutSec must be between 1 and %d", name, maxBoxInitTimeoutSec)
	case kind != RuntimeKindBox:
		return nil
	}
	return refuseBoxSecrets(name, cfg.Env)
}

// DefaultBoxInitTimeout is a box server's first-start budget when it declares none. A first
// start fetches the server into the identity's own npm or uv cache; measured 2026-09-26 in a
// box with empty caches, 3.5-5.4 s for light servers and 10.95 s for a numpy/scipy/sympy one,
// against 0.6-1.8 s warm. LibreChat's per-server initTimeout defaults to the same 30 s.
const DefaultBoxInitTimeout = 30 * time.Second

// maxBoxInitTimeoutSec bounds what a registry row may ask for: a start that needs longer is
// not a cold cache, and every caller waiting on it is a turn or a mount.
const maxBoxInitTimeoutSec = 600

// BoxInitTimeout is the budget s gets for its first start in a box: what it declares, or
// DefaultBoxInitTimeout. Zero for a server that does not run in a box.
func BoxInitTimeout(s ManagedServer) time.Duration {
	if !IsBoxRuntime(s) {
		return 0
	}
	if s.Runtime.InitTimeoutSec > 0 {
		return time.Duration(s.Runtime.InitTimeoutSec) * time.Second
	}
	return DefaultBoxInitTimeout
}

// refuseBoxSecrets rejects a credential declared for a box server. The box exec drops
// secret-shaped variables on the way in, because everything in a box is readable by the
// agent's own shell, so the server would start without it and fail for a reason nobody
// could see. Saying so at write time is the only honest place.
func refuseBoxSecrets(name string, env []string) error {
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		if secret.IsSecretEnvVar(key, value) {
			return fmt.Errorf("MCP managed config: server %q runs in the sandbox box, where %s would be readable by the agent's shell; box servers take no secrets", name, key)
		}
	}
	return nil
}

func normalizedRuntimeKind(cfg ManagedServer) string {
	if kind := strings.TrimSpace(cfg.Runtime.Kind); kind != "" {
		return kind
	}
	return RuntimeKindLocal
}

// IsBoxRuntime reports whether s is a stdio server that runs in the caller's sandbox box.
func IsBoxRuntime(s ManagedServer) bool {
	return normalizedServerType(s) == ServerTypeStdio && normalizedRuntimeKind(s) == RuntimeKindBox
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
