package manager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// Runtime kinds select how a managed MCP server is launched: directly on the
// host, in a per-server Docker container, or via the Docker MCP gateway.
const (
	RuntimeLocal         = mcp.RuntimeKindLocal
	RuntimeDocker        = mcp.RuntimeKindDocker
	RuntimeDockerGateway = mcp.RuntimeKindDockerGateway
)

var errMCPServerBlocked = errors.New("mcp server blocked")

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
	switch runtimeKind(server) {
	case RuntimeDocker:
		return dockerRuntimeConfig(server)
	case RuntimeDockerGateway:
		return dockerGatewayRuntimeConfig(name, server)
	default:
		if strings.TrimSpace(server.Command) == "" {
			return mcp.ServerConfig{}, fmt.Errorf("MCP server %q command cannot be empty", name)
		}
		return mcp.ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env}, nil
	}
}

func dockerRuntimeConfig(server mcp.ManagedServer) (mcp.ServerConfig, error) {
	image := strings.TrimSpace(server.Runtime.Image)
	if image == "" {
		return mcp.ServerConfig{}, fmt.Errorf("docker runtime image cannot be empty")
	}
	args := []string{"run", "-i", "--rm"}
	if len(server.Runtime.Network) == 0 {
		args = append(args, "--network", "none")
	} else {
		args = append(args, "--network", "bridge")
	}
	for _, mount := range server.Runtime.Mounts {
		if strings.TrimSpace(mount) != "" {
			args = append(args, "--mount", mount)
		}
	}
	if server.Runtime.CPUs != "" {
		args = append(args, "--cpus", server.Runtime.CPUs)
	}
	if server.Runtime.Memory != "" {
		args = append(args, "--memory", server.Runtime.Memory)
	}
	env := append([]string(nil), server.Env...)
	for _, entry := range server.Env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			args = append(args, "-e", key)
		}
	}
	if len(server.Runtime.Network) > 0 {
		env = append(env, "AURA_MCP_NETWORK_ALLOW="+strings.Join(server.Runtime.Network, ","))
	}
	args = append(args, image)
	args = append(args, server.Runtime.Command...)
	return mcp.ServerConfig{Command: "docker", Args: args, Env: env}, nil
}

func dockerGatewayRuntimeConfig(name string, server mcp.ManagedServer) (mcp.ServerConfig, error) {
	profile := strings.TrimSpace(server.Runtime.Profile)
	if profile == "" {
		return mcp.ServerConfig{}, fmt.Errorf("MCP server %q docker gateway profile cannot be empty", name)
	}
	return mcp.ServerConfig{Command: "docker", Args: []string{"mcp", "gateway", "run", "--profile", profile}, Env: server.Env}, nil
}

func runtimeKind(server mcp.ManagedServer) string {
	if server.Runtime.Kind != "" {
		return server.Runtime.Kind
	}
	return RuntimeLocal
}

func normalizedTrustForServer(server mcp.ManagedServer) string {
	if mcp.IsKnownTrust(server.Trust.Class) {
		return server.Trust.Class
	}
	if strings.HasPrefix(strings.TrimSpace(server.Source), "recipe:") {
		return mcp.TrustTrustedRecipe
	}
	if server.Type == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
		return mcp.TrustRemoteHTTP
	}
	return mcp.TrustBlocked
}

func isStreamableHTTPServer(server mcp.ManagedServer) bool {
	return strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != ""
}
