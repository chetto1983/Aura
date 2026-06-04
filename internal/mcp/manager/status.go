package manager

import (
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

const (
	StartupStarting = "starting"
	StartupReady    = "ready"
	StartupFailed   = "failed"
	StartupBlocked  = "blocked"
	StartupDisabled = "disabled"
	StartupUnknown  = "unknown"

	AuthUnsupported = "unsupported"
	AuthNotLoggedIn = "notLoggedIn"
	AuthBearerToken = "bearerToken"
	AuthOAuth       = "oAuth"
)

type StatusSnapshot struct {
	Name          string   `json:"name"`
	Profiles      []string `json:"profiles,omitempty"`
	Trust         string   `json:"trust"`
	Runtime       string   `json:"runtime"`
	StartupState  string   `json:"startupState"`
	AuthStatus    string   `json:"authStatus"`
	ToolCount     int      `json:"toolCount"`
	LastError     string   `json:"lastError,omitempty"`
	PolicySummary string   `json:"policySummary,omitempty"`
}

func SnapshotStatus(doc mcp.ManagedConfig) []StatusSnapshot {
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]StatusSnapshot, 0, len(names))
	for _, name := range names {
		server := doc.MCPServers[name]
		trust := doc.NormalizedTrust(name)
		state := StartupUnknown
		switch {
		case server.Enabled != nil && !*server.Enabled:
			state = StartupDisabled
		case trust == mcp.TrustBlocked:
			state = StartupBlocked
		}
		out = append(out, StatusSnapshot{
			Name:          name,
			Profiles:      profilesForServer(doc, name),
			Trust:         trust,
			Runtime:       runtimeName(server),
			StartupState:  state,
			AuthStatus:    authStatus(server),
			PolicySummary: policySummary(server),
		})
	}
	return out
}

func RedactSecrets(s string) string {
	return mcp.RedactSecrets(s)
}

func profilesForServer(doc mcp.ManagedConfig, server string) []string {
	names := make([]string, 0, len(doc.Profiles))
	for profile, cfg := range doc.Profiles {
		for _, name := range cfg.Servers {
			if name == server {
				names = append(names, profile)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

func runtimeName(server mcp.ManagedServer) string {
	if strings.TrimSpace(server.Runtime.Kind) != "" {
		return strings.TrimSpace(server.Runtime.Kind)
	}
	if strings.TrimSpace(server.URL) != "" || server.Type == mcp.ServerTypeStreamableHTTP {
		return mcp.TrustRemoteHTTP
	}
	return "local"
}

func authStatus(server mcp.ManagedServer) string {
	for _, entry := range server.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "OAUTH") {
			return AuthOAuth
		}
		if strings.Contains(upper, "BEARER") || strings.Contains(upper, "TOKEN") || strings.EqualFold(key, "Authorization") {
			if strings.TrimSpace(value) == "" || strings.Contains(value, "CHANGE_ME") || strings.HasPrefix(value, "${") {
				return AuthNotLoggedIn
			}
			return AuthBearerToken
		}
	}
	return AuthUnsupported
}

func policySummary(server mcp.ManagedServer) string {
	parts := []string{}
	if len(server.RiskLabels) > 0 {
		parts = append(parts, "risk="+strings.Join(server.RiskLabels, ","))
	}
	if len(server.ToolPolicy.Allow) > 0 {
		parts = append(parts, "allow="+strings.Join(server.ToolPolicy.Allow, ","))
	}
	if len(server.ToolPolicy.Deny) > 0 {
		parts = append(parts, "deny="+strings.Join(server.ToolPolicy.Deny, ","))
	}
	if len(server.ToolPolicy.DenyRisk) > 0 {
		parts = append(parts, "denyRisk="+strings.Join(server.ToolPolicy.DenyRisk, ","))
	}
	return strings.Join(parts, " ")
}
