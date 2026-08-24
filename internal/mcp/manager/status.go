package manager

import (
	"slices"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// Startup states and auth-status values report a managed server's lifecycle and
// credential posture in status snapshots.
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

// StatusSnapshot is the per-server status view surfaced to the CLI/UI: trust,
// runtime, startup and auth state.
type StatusSnapshot struct {
	Name         string   `json:"name"`
	Profiles     []string `json:"profiles,omitempty"`
	Trust        string   `json:"trust"`
	Runtime      string   `json:"runtime"`
	StartupState string   `json:"startupState"`
	AuthStatus   string   `json:"authStatus"`
	LastError    string   `json:"lastError,omitempty"`
}

// SnapshotStatus computes a StatusSnapshot for every server in doc, sorted by
// name, deriving startup state from enabled/trust.
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
			Name:         name,
			Profiles:     profilesForServer(doc, name),
			Trust:        trust,
			Runtime:      runtimeName(server),
			StartupState: state,
			AuthStatus:   authStatus(server),
		})
	}
	return out
}

// RedactSecrets masks secret-looking values in s for safe display, delegating
// to mcp.RedactSecrets.
func RedactSecrets(s string) string {
	return mcp.RedactSecrets(s)
}

func profilesForServer(doc mcp.ManagedConfig, server string) []string {
	names := make([]string, 0, len(doc.Profiles))
	for profile, cfg := range doc.Profiles {
		if slices.Contains(cfg.Servers, server) {
			names = append(names, profile)
		}
	}
	sort.Strings(names)
	return names
}

// runtimeName derives the transport label for status display: an explicit
// runtime kind wins, otherwise the transport is resolved through the
// canonical mcp.Classify (MCPH-01) rather than a re-implemented inline check.
// A Classify error (mixed/inconsistent shape) falls back to "local" — status
// display is best-effort, the actual run-eligibility gate lives in
// manager.RunnableManagedServers.
func runtimeName(server mcp.ManagedServer) string {
	if strings.TrimSpace(server.Runtime.Kind) != "" {
		return strings.TrimSpace(server.Runtime.Kind)
	}
	if serverType, _, err := mcp.Classify(server); err == nil && serverType == mcp.ServerTypeStreamableHTTP {
		return mcp.TrustRemoteHTTP
	}
	return "local"
}

// authStatus reports how a server authenticates.
//
// OAuth is decided by the real classifier, NOT by sniffing env key names for "OAUTH".
// The sniff was wrong in the direction that matters most: a remote server with NO oauth
// configuration at all is exactly the Linear/Atlassian shape — dynamic client
// registration, zero operator config — and reporting it "unsupported" told the operator
// the one thing that would stop them trying to connect it.
func authStatus(server mcp.ManagedServer) string {
	if settings, err := mcp.OAuthSettingsFromEnv(server.Env); err == nil && mcp.UsesOAuth(server, settings) {
		return AuthOAuth
	}
	for _, entry := range server.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		// Aura's own MCP_OAUTH_* knobs configure the flow decided above; reaching here
		// means that flow is NOT active, so they describe nothing. Skipping them is also
		// load-bearing: MCP_OAUTH_TOKEN_URL contains "TOKEN" and would otherwise be
		// reported as a bearer credential the server does not have.
		if strings.HasPrefix(upper, "MCP_OAUTH_") {
			continue
		}
		// Any OTHER oauth-shaped key still means what it always did: a server that runs
		// its own OAuth internally, which a local stdio server legitimately can.
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
