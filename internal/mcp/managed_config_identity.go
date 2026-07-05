package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/profile"
)

// SourceRecipeMemory is the Source marker of the shared, admin-governed agent-memory
// server (:8091) — the class-(b) server users cannot toggle per-identity (D-19). It is
// the single source of truth for that class marker; the manager catalog stamps it onto
// the memory recipe so MountForIdentity refuses a per-identity toggle by Source alone.
const SourceRecipeMemory = "recipe:memory"

// localMCPIdentity is the CLI / no-principal fallback identity (D-25): the CLI always
// runs as local, so an empty principal mounts local's per-identity view.
const localMCPIdentity = "local"

// identityConfigFile is the per-identity overlay filename under ~/.aura/mcp/{id}/.
const identityConfigFile = "servers.json"

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

// IsSharedAdminGoverned reports whether s is the class-(b) shared agent-memory server
// (keyed on the memory recipe Source, name-independent). Users cannot toggle shared
// infra per-identity (D-19); only an admin governs it via the shared catalog.
func IsSharedAdminGoverned(s ManagedServer) bool {
	return strings.TrimSpace(s.Source) == SourceRecipeMemory
}

// normalizeMCPIdentity trims identity and falls back to the local identity for the
// CLI / no-principal path (D-25).
func normalizeMCPIdentity(identity string) string {
	if id := strings.TrimSpace(identity); id != "" {
		return id
	}
	return localMCPIdentity
}

// identityMCPRoot is the directory holding both the shared servers.json and the
// per-identity {id}/servers.json overlays (the parent of ManagedConfigPath).
func identityMCPRoot() (string, error) {
	shared, err := ManagedConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(shared), nil
}

// IdentityConfigPath resolves ~/.aura/mcp/{id}/servers.json through the profile
// containment guard, so a crafted identity can never escape the mcp root.
func IdentityConfigPath(identity string) (string, error) {
	root, err := identityMCPRoot()
	if err != nil {
		return "", err
	}
	dir, err := profile.RootIdentityDir(root, normalizeMCPIdentity(identity))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, identityConfigFile), nil
}

// LoadIdentityMCPConfig reads the per-identity overlay, returning an empty overlay when
// the file is absent or blank. A malformed file is an error.
func LoadIdentityMCPConfig(identity string) (IdentityMCPConfig, error) {
	path, err := IdentityConfigPath(identity)
	if err != nil {
		return IdentityMCPConfig{}, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // path rooted via profile.RootIdentityDir guard
	if os.IsNotExist(err) {
		return IdentityMCPConfig{Preferences: map[string]IdentityServerPref{}}, nil
	}
	if err != nil {
		return IdentityMCPConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return IdentityMCPConfig{Preferences: map[string]IdentityServerPref{}}, nil
	}
	var doc IdentityMCPConfig
	if err := json.Unmarshal(data, &doc); err != nil {
		return IdentityMCPConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Preferences == nil {
		doc.Preferences = map[string]IdentityServerPref{}
	}
	return doc, nil
}

// SaveIdentityMCPConfig writes the per-identity overlay as indented JSON with user-only
// permissions (trust/enable metadata should not be group/world-readable).
func SaveIdentityMCPConfig(identity string, cfg IdentityMCPConfig) error {
	path, err := IdentityConfigPath(identity)
	if err != nil {
		return err
	}
	if cfg.Version == 0 {
		cfg.Version = ManagedConfigVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create identity MCP dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode identity MCP config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600) // best effort on Windows; enforced on POSIX
	return nil
}

// MountForIdentity returns the effective ManagedConfig for identity: the shared catalog
// read-only, layered with the identity's class-(a) enable/trust overrides. The class-(b)
// shared agent-memory server keeps the shared catalog's enable/trust (admin-governed) —
// any overlay pref for it is ignored (defense in depth). The shared catalog on disk is
// never mutated; each server is a value copy before the overlay is applied.
func MountForIdentity(identity string) (ManagedConfig, error) {
	sharedPath, err := ManagedConfigPath()
	if err != nil {
		return ManagedConfig{}, err
	}
	shared, err := LoadManagedConfig(sharedPath)
	if err != nil {
		return ManagedConfig{}, err
	}
	overlay, err := LoadIdentityMCPConfig(identity)
	if err != nil {
		return ManagedConfig{}, err
	}
	eff := ManagedConfig{
		Version:       shared.Version,
		ActiveProfile: shared.ActiveProfile,
		Profiles:      shared.Profiles,
		MCPServers:    make(map[string]ManagedServer, len(shared.MCPServers)),
	}
	for name, srv := range shared.MCPServers {
		if !IsSharedAdminGoverned(srv) {
			if pref, ok := overlay.Preferences[name]; ok {
				if pref.Enabled != nil {
					srv.Enabled = pref.Enabled
				}
				if strings.TrimSpace(pref.Trust.Class) != "" {
					srv.Trust = pref.Trust
				}
			}
		}
		eff.MCPServers[name] = srv
	}
	return eff, nil
}

// SetEnabledForIdentity records a per-identity enable/disable for a class-(a) server.
func SetEnabledForIdentity(identity, name string, enabled bool) error {
	return mutateIdentityPref(identity, name, func(p *IdentityServerPref) {
		v := enabled
		p.Enabled = &v
	})
}

// SetTrustForIdentity records a per-identity trust class for a class-(a) server.
func SetTrustForIdentity(identity, name, class string) error {
	if !isKnownTrust(class) {
		return fmt.Errorf("mcp: unknown trust class %q", class)
	}
	return mutateIdentityPref(identity, name, func(p *IdentityServerPref) {
		p.Trust = ManagedTrust{Class: strings.TrimSpace(class)}
	})
}

// mutateIdentityPref validates the identity path, confirms name is a class-(a)
// shared-catalog server (refusing class-(b) shared infra and unknown names), then
// applies mutate to the identity's overlay pref and persists it.
func mutateIdentityPref(identity, name string, mutate func(*IdentityServerPref)) error {
	if _, err := IdentityConfigPath(identity); err != nil {
		return err
	}
	sharedPath, err := ManagedConfigPath()
	if err != nil {
		return err
	}
	shared, err := LoadManagedConfig(sharedPath)
	if err != nil {
		return err
	}
	srv, ok := shared.MCPServers[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownServer, name)
	}
	if IsSharedAdminGoverned(srv) {
		return fmt.Errorf("%w: %q", ErrSharedAdminGoverned, name)
	}
	overlay, err := LoadIdentityMCPConfig(identity)
	if err != nil {
		return err
	}
	if overlay.Preferences == nil {
		overlay.Preferences = map[string]IdentityServerPref{}
	}
	pref := overlay.Preferences[name]
	mutate(&pref)
	overlay.Preferences[name] = pref
	return SaveIdentityMCPConfig(identity, overlay)
}
