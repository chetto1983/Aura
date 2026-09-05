package manager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// RuntimeLocal is the one kind a managed stdio MCP server can be launched with:
// directly, as a child process. The `docker` and `docker_gateway` kinds were
// retired by amendment #209 — RuntimeLaunchConfig refused both whenever
// AURA_IN_CONTAINER=1, and the appliance image sets it unconditionally, so no
// shipped deployment could ever take those paths.
const RuntimeLocal = mcp.RuntimeKindLocal

var (
	errMCPServerBlocked = errors.New("mcp server blocked")
	// errRetiredRuntimeKind answers a registry row still declaring a retired kind.
	// Read paths deliberately do not validate (one planted entry must not make the
	// whole registry unreadable), so such a row reaches the launcher; without this it
	// would fall through to the stdio branch and fail on an empty Command, reporting
	// the wrong problem. The remedy is the one the container refusal used to give.
	errRetiredRuntimeKind = errors.New("runtime kind retired (amendment #209) - deploy the server as a compose sibling and mount it via URL")
)

// retiredRuntimeKinds are the launch kinds amendment #209 removed. They are matched
// by their on-disk strings, not by constants, precisely because the constants are gone.
var retiredRuntimeKinds = map[string]struct{}{"docker": {}, "docker_gateway": {}}

// RuntimeServers builds launchable ServerConfigs for the active profile's
// runnable stdio servers, excluding streamable-HTTP servers; it returns nil
// when none are launchable.
func RuntimeServers(doc mcp.ManagedConfig) (map[string]mcp.ServerConfig, error) {
	servers, err := RunnableManagedServers(doc)
	if err != nil {
		return nil, err
	}
	out := map[string]mcp.ServerConfig{}
	for name, server := range servers {
		if isStreamableHTTPServer(server) {
			continue
		}
		cfg, err := RuntimeLaunchConfig(name, server)
		if err != nil {
			return nil, err
		}
		out[name] = cfg
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// RunnableManagedServers returns the active profile's servers that are eligible
// to run, skipping disabled, trust-blocked, and unapprovable servers; it
// returns nil when none qualify.
func RunnableManagedServers(doc mcp.ManagedConfig) (map[string]mcp.ManagedServer, error) {
	out := map[string]mcp.ManagedServer{}
	for _, name := range doc.ProfileServerNames(doc.ActiveProfileName()) {
		server := doc.MCPServers[name]
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		if normalizedTrustForServer(server) == mcp.TrustBlocked {
			continue
		}
		if isStreamableHTTPServer(server) {
			if strings.TrimSpace(server.URL) == "" {
				return nil, fmt.Errorf("MCP server %q url cannot be empty", name)
			}
			out[name] = server
			continue
		}
		if _, err := RuntimeLaunchConfig(name, server); err != nil {
			if errors.Is(err, errMCPServerBlocked) {
				continue
			}
			return nil, err
		}
		out[name] = server
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// RuntimeLaunchConfig resolves the concrete command/args/env for launching a
// server based on its runtime kind, returning errMCPServerBlocked when trust
// approval is required.
func RuntimeLaunchConfig(name string, server mcp.ManagedServer) (mcp.ServerConfig, error) {
	trust := normalizedTrustForServer(server)
	if trust == mcp.TrustBlocked {
		return mcp.ServerConfig{}, fmt.Errorf("%w: %q trust approval required", errMCPServerBlocked, name)
	}
	kind := runtimeKind(server)
	if _, retired := retiredRuntimeKinds[strings.TrimSpace(kind)]; retired {
		return mcp.ServerConfig{}, fmt.Errorf("%w: server %q declares %q", errRetiredRuntimeKind, name, kind)
	}
	if strings.TrimSpace(server.Command) == "" {
		return mcp.ServerConfig{}, fmt.Errorf("MCP server %q command cannot be empty", name)
	}
	return mcp.ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env}, nil
}

func runtimeKind(server mcp.ManagedServer) string {
	if server.Runtime.Kind != "" {
		return server.Runtime.Kind
	}
	return RuntimeLocal
}

// normalizedTrustForServer resolves server's effective trust class through the
// canonical mcp.Classify (MCPH-01): the manager's eligibility gate must agree
// with the single source of truth, not carry its own copy of the trust
// decision. A Classify error (mixed transport, inconsistent explicit
// type/trust) is treated conservatively as TrustBlocked, since this path
// gates whether a server may run at all.
func normalizedTrustForServer(server mcp.ManagedServer) string {
	_, trust, err := mcp.Classify(server)
	if err != nil {
		return mcp.TrustBlocked
	}
	return trust
}

// isStreamableHTTPServer reports whether server classifies as the
// streamable_http transport via mcp.Classify. A Classify error is treated as
// "not streamable_http" — RuntimeLaunchConfig's trust gate (which also
// classifies via normalizedTrustForServer) is what ultimately blocks an
// unclassifiable server, regardless of which branch it falls into here.
func isStreamableHTTPServer(server mcp.ManagedServer) bool {
	serverType, _, err := mcp.Classify(server)
	if err != nil {
		return false
	}
	return serverType == mcp.ServerTypeStreamableHTTP
}
