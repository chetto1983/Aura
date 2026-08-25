package manager

import (
	"maps"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// runtimeset.go answers "what does this host mount?" — the registry's servers plus the
// catalog recipes that are on by default.

// Default-on catalog keys. Memory is a core capability EVERYWHERE; the rest are
// default-on only inside the Aura appliance image, where each one's backing sidecar
// is a declared compose service of the same stack.
const (
	memoryRecipeName   = "memory"
	calendarRecipeName = "calendar"
	whatsappRecipeName = "whatsapp"
)

// containerDefaultOnRecipes mount out of the box inside the appliance image.
//
// calculator used to be here, on the premise that the image warm-caches its uvx package
// so the stdio server starts without egress. The premise was false: the aura service
// mounts a named volume over /root/.cache/uv, a named volume is seeded once and never
// refreshed, so the warmed cache was invisible to every later image. See
// manager.BuiltInCatalog for the full account of why the recipe is gone.
//
// calendar + whatsapp: their sidecars (aura-pim-mcp, aura-whatsapp) ship in the same
// compose file as the agent, so on the appliance they are always present. They were
// install-on-demand, and that made a trap the operator could not see: the cockpit's
// Connect panel pairs the device — scan the QR, WhatsApp says linked — but pairing a
// device is not the same as mounting its tools, and nothing in that flow ran
// `aura mcp install`. The result was a WhatsApp that showed as connected while the agent
// had zero WhatsApp tools, with no error anywhere to explain the gap. Same for Calendar.
// Being on the list does not force them: an explicit `aura mcp disable whatsapp` still
// wins, and a missing sidecar fail-softs to a WARN drop at mount like any other server.
var containerDefaultOnRecipes = []string{calendarRecipeName, whatsappRecipeName}

// recipeBearerEnv maps Aura-owned remote MCP recipes to the deployment secret
// their HTTP transport uses. The credential is overlaid only in the resolved
// runtime set: it never enters the catalog JSON or the persisted registry, and a
// rotated deployment secret takes effect on the next mount even when the recipe
// was installed before the rotation.
var recipeBearerEnv = map[string]string{
	"recipe:calendar": "AURA_PIM_MCP_ADMIN_TOKEN",
}

// RuntimeSet composes what this host will actually mount from `managed`: the runnable
// server configs plus the policy map (the managed recipes an operator may enable/disable).
//
// It takes the document rather than fetching one because there is exactly one place that
// knows where servers live — the registry — and this package is not it. It used to read the
// managed JSON file itself, from inside internal/config, which is how the board and the
// write path ended up answering from two different copies of the same registry.
//
// There was also a second declaration source here: AURA_MCP_SERVERS_JSON, an env override
// that could add servers and shadow managed ones. It is gone. A second way to declare the
// same thing is a second thing to keep in sync, and it bought nothing `aura mcp add` does
// not — while its rows arrived with a source no write path could edit, so a server declared
// that way appeared on the board and refused every mutation.
func RuntimeSet(managed mcp.ManagedConfig) (map[string]mcp.ServerConfig, map[string]mcp.ManagedServer, error) {
	managedServers, err := RuntimeServers(managed)
	if err != nil {
		return nil, nil, err
	}
	runnableManaged, err := RunnableManagedServers(managed)
	if err != nil {
		return nil, nil, err
	}
	policies := make(map[string]mcp.ManagedServer, len(runnableManaged))
	maps.Copy(policies, runnableManaged)
	out := make(map[string]mcp.ServerConfig, len(managedServers))
	maps.Copy(out, managedServers)
	// Default-on (D-08): memory is a core capability, so it mounts out of the box with no
	// `aura mcp install`; an explicit `aura mcp disable memory` still wins (D-09).
	injectDefaultOnMemory(policies, managed)
	injectDefaultOnContainerRecipes(policies, managed)
	injectRecipeBearerTokens(policies)
	if len(out) == 0 && len(policies) == 0 {
		return nil, nil, nil
	}
	return out, policies, nil
}

// injectRecipeBearerTokens uses the existing generic MCP_BEARER_TOKEN transport
// contract for Aura-owned HTTP recipes. Source, rather than registry key, is the
// discriminator so an operator-installed alias of a built-in recipe behaves the
// same as its default name. Third-party remote_http servers are never matched.
func injectRecipeBearerTokens(policies map[string]mcp.ManagedServer) {
	for name, server := range policies {
		envName, ok := recipeBearerEnv[server.Source]
		if !ok {
			continue
		}
		token := strings.TrimSpace(os.Getenv(envName))
		if token == "" {
			continue
		}
		server.Env = replaceRuntimeEnv(server.Env, "MCP_BEARER_TOKEN", token)
		policies[name] = server
	}
}

func replaceRuntimeEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

// injectDefaultOn adds a catalog recipe to policies unless the operator has ANY say of
// their own (D-08 default-on, D-09 respect disable), so a fresh machine mounts it with no
// `aura mcp install`.
//
// Precedence (highest first):
//  1. ANY explicit entry in the managed doc wins — enabled (already in policies),
//     disabled (`aura mcp disable <name>`), trust-blocked, or excluded by the active
//     profile. The check is against the UNFILTERED managed.MCPServers, not the
//     profile-filtered policies map: a customized URL excluded by the active profile must
//     NOT be silently replaced by the catalog recipe, and a profile that excludes the
//     server keeps it unmounted (CR-01).
//  2. Otherwise (no operator entry at all) inject the catalog recipe.
func injectDefaultOn(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig, name string) {
	if _, ok := policies[name]; ok {
		return
	}
	if _, ok := managed.MCPServers[name]; ok {
		// Explicit operator entry (disabled, blocked, or profile-excluded — the
		// enabled+active case already returned via policies above).
		return
	}
	recipe, ok := LookupCatalog(name)
	if !ok {
		return
	}
	policies[name] = recipe.Server
}

// injectDefaultOnMemory keeps memory on out of the box on EVERY host, container or not:
// it is a core capability, not an appliance convenience.
func injectDefaultOnMemory(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig) {
	injectDefaultOn(policies, managed, memoryRecipeName)
}

// injectDefaultOnContainerRecipes turns on the recipes whose backing sidecar ships in the
// appliance image/compose stack. Outside the container it is a no-op — a dev host may have
// neither uvx nor the sidecars.
func injectDefaultOnContainerRecipes(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig) {
	if os.Getenv("AURA_IN_CONTAINER") != "1" {
		return
	}
	for _, name := range containerDefaultOnRecipes {
		injectDefaultOn(policies, managed, name)
	}
}
