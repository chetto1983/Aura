package mcp

import "errors"

// SourceRecipeMemory is the Source marker of the shared, admin-governed agent-memory
// server (:8091) — the class-(b) server users cannot toggle per-identity (D-19). It is
// the single source of truth for that class marker; the manager catalog stamps it onto
// the memory recipe so MountForIdentity can refuse a per-identity toggle by Source alone.
const SourceRecipeMemory = "recipe:memory"

// localMCPIdentity is the CLI / no-principal fallback identity (D-25): the CLI always
// runs as local, so an empty principal mounts local's per-identity view.
const localMCPIdentity = "local"

var (
	// ErrSharedAdminGoverned is returned when a caller tries to toggle a class-(b)
	// shared server through the per-identity path. Only an admin governs shared infra.
	ErrSharedAdminGoverned = errors.New("mcp: server is shared admin-governed (class-b), not per-identity toggleable")
	// ErrUnknownServer is returned when a per-identity enable/trust targets a server
	// name that does not exist in the shared catalog.
	ErrUnknownServer = errors.New("mcp: server not found in shared catalog")
)

// IdentityServerPref is one identity's enable/trust override for a class-(a) server
// defined in the shared catalog. A nil Enabled leaves the shared default; an empty
// Trust.Class leaves the shared trust.
type IdentityServerPref struct {
	Enabled *bool        `json:"enabled,omitempty"`
	Trust   ManagedTrust `json:"trust,omitempty"`
}

// IdentityMCPConfig is the per-identity MCP overlay stored at
// ~/.aura/mcp/{id}/servers.json. It holds only each identity's enable/trust preference
// for class-(a) servers; the shared catalog stays read-only and class-(b) shared infra
// is never represented here.
type IdentityMCPConfig struct {
	Version     int                           `json:"version,omitempty"`
	Preferences map[string]IdentityServerPref `json:"servers,omitempty"`
}

// errNotImplemented is the RED-phase stub sentinel; it is removed once the real
// per-identity mount/overlay logic lands.
var errNotImplemented = errors.New("not implemented")

// IsSharedAdminGoverned reports whether s is the class-(b) shared agent-memory server.
func IsSharedAdminGoverned(s ManagedServer) bool { return false }

// IdentityConfigPath resolves the per-identity servers.json overlay path.
func IdentityConfigPath(identity string) (string, error) { return "", errNotImplemented }

// LoadIdentityMCPConfig reads the per-identity overlay, empty when absent.
func LoadIdentityMCPConfig(identity string) (IdentityMCPConfig, error) {
	return IdentityMCPConfig{}, errNotImplemented
}

// SaveIdentityMCPConfig writes the per-identity overlay with user-only permissions.
func SaveIdentityMCPConfig(identity string, cfg IdentityMCPConfig) error { return errNotImplemented }

// MountForIdentity returns the effective ManagedConfig for identity: the shared catalog
// read-only, layered with the identity's class-(a) enable/trust overrides.
func MountForIdentity(identity string) (ManagedConfig, error) {
	return ManagedConfig{}, errNotImplemented
}

// SetEnabledForIdentity records a per-identity enable/disable for a class-(a) server.
func SetEnabledForIdentity(identity, name string, enabled bool) error { return errNotImplemented }

// SetTrustForIdentity records a per-identity trust class for a class-(a) server.
func SetTrustForIdentity(identity, name, class string) error { return errNotImplemented }
