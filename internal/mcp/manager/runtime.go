package manager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

const (
	RuntimeLocal         = mcp.RuntimeKindLocal
	RuntimeDocker        = mcp.RuntimeKindDocker
	RuntimeDockerGateway = mcp.RuntimeKindDockerGateway
)

var errMCPServerBlocked = errors.New("mcp server blocked")

func RuntimeServers(doc mcp.ManagedConfig) (map[string]mcp.ServerConfig, error) {
	out := map[string]mcp.ServerConfig{}
	for _, name := range doc.ProfileServerNames(doc.ActiveProfileName()) {
		server := doc.MCPServers[name]
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		cfg, err := RuntimeLaunchConfig(name, server)
		if err != nil {
			if errors.Is(err, errMCPServerBlocked) {
				continue
			}
			return nil, err
		}
		out[name] = cfg
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

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
	if server.Trust.Class != "" {
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
