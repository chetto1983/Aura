// Aura entry point. Sub-commands:
//
//	aura serve              — run the long-lived agent runtime (default in production)
//	aura shell              — interactive REPL against the agent loop
//	aura agent dry-run      — drive a mock LoopAgent through the Budget tree, one Event per JSON line (SC#4)
//	aura web <sub>          — web tooling: doctor (SearXNG reachability) | tool web_search/web_fetch '<json>'
//	aura tools              — print the tool manifest (active + deferred)
//	aura db <sub>           — Postgres lifecycle (migrate|ping|status|reset)
//	aura neo4j <sub>        — Neo4j lifecycle
//	aura identity <sub>     — identity + capability_grants (list|get|grant|revoke)
//	aura paused-states <sub>— HITL pause escape hatch (list|purge --before <ISO> --confirm)
//	aura chat <sub>         — multi-thread conversation REPL (list|new|resume|archive|unarchive|delete|rename|search)
//	aura version            — print build metadata (version, commit, build date)
//
// Tabula-rasa scaffold: `tools`, `agent`, `db`, `neo4j`, `chat`, `shell`, and
// `serve` are wired through the real runtime composition roots. The Phase-1
// `aura chat` stub + concrete Loop were removed in Slice 0.9 (Plan 02-07);
// `aura chat` returned in Phase 3 wired to a real LlmAgent.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/swarm"
	"github.com/chetto1983/aura/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "tools":
		printTools()
	case "mcp":
		runMCP(os.Args[2:])
	case "agent":
		runAgent(os.Args[2:])
	case "swarm-demo":
		runSwarmDemo(os.Args[2:])
	case "web":
		runWeb(os.Args[2:])
	case "db":
		runDB(os.Args[2:])
	case "neo4j":
		runNeo4j(os.Args[2:])
	case "identity":
		runIdentity(os.Args[2:])
	case "paused-states":
		runPausedStates(os.Args[2:])
	case "task":
		runTask(os.Args[2:])
	case "skills":
		runSkills(os.Args[2:])
	case "chat":
		runChat(os.Args[2:])
	case "cache-stats":
		runCacheStats(os.Args[2:])
	case "cache-audit": // hidden — runtime KV-prefix invariant gate (D-06); not in usage()
		runCacheAudit(os.Args[2:])
	case "config":
		runConfig(os.Args[2:])
	case "version", "--version", "-v":
		runVersion()
	case "serve":
		runServe(os.Args[2:])
	case "shell":
		runShell(os.Args[2:])
	case "toolpipe": // hidden — non-LLM tool-layer latency harness (NDJSON stdin); not in usage()
		runToolPipe(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: aura {serve|shell|chat <sub>|config <sub>|identity <sub>|paused-states <sub>|task <sub>|skills <sub>|agent <sub>|swarm-demo|web <doctor|tool ...>|tools|mcp <sub>|db <sub>|neo4j <sub>|version}")
}

func buildRegistry() *tools.Registry {
	cfg := config.LoadDB()
	return buildBaseRegistry(cfg, nil)
}

// buildBaseRegistry is the shared composition root for every boot path. ts is the
// live scheduler store the non-deferred `task` tool persists against (D-11): serve/
// chat inject the cronTaskStore over the open pool; the pool-free manifest paths
// (`aura tools`, buildRegistry) pass nil — the tool still registers (its Spec needs no
// store) so the manifest lists it, and an Execute without a store would error loudly.
func buildBaseRegistry(cfg *config.Config, ts *cronTaskStore) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{})
	reg.Register(tools.AskUser{}) // HITL pause primitive — the LLM must see ask_user in the live manifest
	reg.Register(newTaskTool(ts))
	// ONE non-deferred skill tool; manifest rides in its Description (D-05/D-06). When
	// a live pool is available (serve/chat boot, ts!=nil) the write actions are wired to
	// the durable, gated Writer (11-05); the pool-free manifest path (`aura tools`) gets
	// a read-only tool whose write actions error loudly.
	reg.Register(newSkillTool(cfg, taskStorePool(ts)))
	webEngine := web.NewClient(cfg)
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine}) // manifest auto-sorts (web_fetch < web_search); never hand-order
	// shell_exec is the full host terminal — THE execution surface (amendment #50 / D-15c).
	// Deferred so simple chat/web turns do not carry a giant shell schema in the hot manifest.
	reg.Register(&tools.ShellExec{})
	// Native in-process filesystem hands — Claude-Code-style file ergonomics, full
	// host access, no path fence (amendment #50 / D-15c) EXCEPT the surgical
	// skills-library fence (#54 / D-43): fs_write/fs_edit refuse to write inside
	// SkillsDir so the gated skill-authoring flow cannot be bypassed.
	reg.Register(&tools.FSRead{})
	reg.Register(&tools.FSWrite{SkillsDir: cfg.SkillsDir})
	reg.Register(&tools.FSEdit{SkillsDir: cfg.SkillsDir})
	reg.Register(&tools.FSGrep{})
	reg.Register(&tools.FSGlob{})
	// swarm_spawn registers into the PARENT registry ONLY (D-08/D-10): workers receive
	// the Without(parent, "swarm_spawn") clone the adapter derives per child, never the
	// tool itself, so a worker cannot recursively fan out. It is Deferred:true, so it
	// does NOT satisfy the >=1-non-deferred guard (Pitfall 6) — the non-deferred
	// built-ins above keep reg.Validate() green. The adapter resolves the live parent
	// budget/registry/client/llmCfg/convID off the tool-call ctx (agent.WithSwarmContext).
	reg.Register(&tools.SwarmSpawn{Runner: swarm.NewRunnerAdapter(*cfg), MaxGoals: cfg.MaxSwarmGoals})
	// D-10: fail closed at boot if no actionable tool exists (excluding tool_search).
	// This is the shared composition root — buildRegistry and buildRegistryWithMCP
	// both delegate here, so the guard covers every boot path.
	if err := reg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "registry:", err)
		os.Exit(1)
	}
	return reg
}

func buildRegistryWithMCP(ctx context.Context, cfg *config.Config, ts *cronTaskStore) (*tools.Registry, []func() error, error) {
	if cfg.MCPServersErr != nil {
		return nil, nil, cfg.MCPServersErr
	}
	reg := buildBaseRegistry(cfg, ts)
	if len(cfg.MCPServers) == 0 && len(cfg.MCPPolicies) == 0 {
		return reg, nil, nil
	}

	seen := map[string]struct{}{}
	serverNames := make([]string, 0, len(cfg.MCPServers)+len(cfg.MCPPolicies))
	for name := range cfg.MCPServers {
		seen[name] = struct{}{}
		serverNames = append(serverNames, name)
	}
	for name := range cfg.MCPPolicies {
		if _, ok := seen[name]; ok {
			continue
		}
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	closers := make([]func() error, 0, len(serverNames))
	for _, name := range serverNames {
		// D-21 fail-soft: a single dead/misconfigured server WARN-and-drops; boot
		// continues with the servers that did mount. Matches every surveyed MCP host
		// (Claude Code/Desktop, VS Code). Already-mounted servers stay registered —
		// do NOT closeMCPServers, do NOT abort. The non-deferred built-ins keep the
		// registry valid even when every MCP server is dropped (Pitfall 6).
		var closer func() error
		var err error
		if _, managed := cfg.MCPPolicies[name]; managed {
			closer, _, err = mcptools.MountManagedServer(ctx, reg, name, cfg.MCPPolicies[name])
		} else {
			closer, _, err = mcptools.MountServer(ctx, reg, name, cfg.MCPServers[name])
		}
		if err != nil {
			slog.Warn("mcp mount failed", "server", name, "err", err)
			continue
		}
		closers = append(closers, closer)
	}
	return reg, closers, nil
}

func closeMCPServers(closers []func() error) error {
	var first error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func printTools() {
	reg, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		os.Exit(1)
	}
	defer func() { _ = closeMCPServers(closers) }()
	fmt.Print(reg.RenderText())
}
