package manager

import (
	"fmt"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// ImportOptions controls how ImportProfile merges an incoming managed config,
// notably whether existing secret credentials are overwritten or preserved.
type ImportOptions struct {
	OverwriteCredentials bool
}

// ExportProfile extracts a single profile and its servers from doc into a
// shareable ManagedConfig, redacting secret env values into ${KEY} placeholders.
func ExportProfile(doc mcp.ManagedConfig, profile string) (mcp.ManagedConfig, error) {
	names := doc.ProfileServerNames(profile)
	if len(names) == 0 {
		return mcp.ManagedConfig{}, fmt.Errorf("profile %q has no servers to export", profile)
	}
	out := mcp.ManagedConfig{
		Version:       mcp.ManagedConfigVersion,
		ActiveProfile: profile,
		Profiles:      map[string]mcp.ManagedProfile{},
		MCPServers:    map[string]mcp.ManagedServer{},
	}
	if p, ok := doc.Profiles[profile]; ok {
		out.Profiles[profile] = p
	} else {
		out.Profiles[profile] = mcp.ManagedProfile{Servers: names}
	}
	for _, name := range names {
		server := doc.MCPServers[name]
		server.Env = RedactEnv(server.Env)
		out.MCPServers[name] = server
	}
	return out, nil
}

// ImportProfile merges incoming profiles and servers into base in place,
// preserving existing credentials for known secret env keys unless
// opts.OverwriteCredentials is set.
func ImportProfile(base *mcp.ManagedConfig, incoming mcp.ManagedConfig, opts ImportOptions) error {
	if base.MCPServers == nil {
		base.MCPServers = map[string]mcp.ManagedServer{}
	}
	if base.Profiles == nil {
		base.Profiles = map[string]mcp.ManagedProfile{}
	}
	if base.Version == 0 {
		base.Version = mcp.ManagedConfigVersion
	}
	for name, profile := range incoming.Profiles {
		base.Profiles[name] = profile
	}

	names := make([]string, 0, len(incoming.MCPServers))
	for name := range incoming.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		next := incoming.MCPServers[name]
		if existing, ok := base.MCPServers[name]; ok && !opts.OverwriteCredentials {
			next.Env = mergeEnvPreserveCredentials(existing.Env, next.Env)
		}
		base.MCPServers[name] = next
	}
	return nil
}

// RedactEnv replaces values of secret env entries with ${KEY} placeholders,
// leaving non-secret and already-placeholder entries unchanged.
func RedactEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, value, ok := cutEnv(entry)
		if !ok {
			out = append(out, entry)
			continue
		}
		if isSecretEnvKey(key) && !isPlaceholderValue(key, value) {
			out = append(out, key+"=${"+key+"}")
			continue
		}
		out = append(out, entry)
	}
	return out
}

func mergeEnvPreserveCredentials(existing, incoming []string) []string {
	existingByKey := map[string]string{}
	existingOrder := make([]string, 0, len(existing))
	for _, entry := range existing {
		key, _, ok := cutEnv(entry)
		if !ok {
			continue
		}
		if _, seen := existingByKey[key]; !seen {
			existingOrder = append(existingOrder, key)
		}
		existingByKey[key] = entry
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(incoming)+len(existing))
	for _, entry := range incoming {
		key, value, ok := cutEnv(entry)
		if !ok {
			out = append(out, entry)
			continue
		}
		seen[key] = struct{}{}
		if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && isPlaceholderValue(key, value) {
			out = append(out, prior)
			continue
		}
		if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && !isPlaceholderValue(key, value) {
			out = append(out, prior)
			continue
		}
		out = append(out, entry)
	}
	for _, key := range existingOrder {
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, existingByKey[key])
	}
	return out
}

func cutEnv(entry string) (key, value string, ok bool) {
	key, value, ok = strings.Cut(entry, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", false
	}
	return key, value, true
}

func isSecretEnvKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range []string{"token", "secret", "pass", "key", "auth", "bearer", "credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isPlaceholderValue(key, value string) bool {
	return value == "${"+key+"}" || value == ""
}
