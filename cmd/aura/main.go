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
// Tabula-rasa scaffold: `tools`, `agent`, `db`, and `neo4j` are wired; `shell`
// and `serve` print a TODO marker so the entry stays build-clean while the agent
// runtime fills in. The Phase-1 `aura chat` stub + concrete Loop were removed in
// Slice 0.9 (Plan 02-07); `aura chat` returns in Phase 3 wired to a real LlmAgent.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandboxagent"
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
	case "shell", "serve":
		fmt.Println("TODO: implemented by the agent-loop and CLI slices")
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: aura {serve|shell|chat <sub>|config <sub>|identity <sub>|paused-states <sub>|agent <sub>|web <doctor|tool ...>|tools|mcp <sub>|db <sub>|neo4j <sub>|version}")
}

func buildRegistry() *tools.Registry {
	cfg := config.LoadDB()
	return buildBaseRegistry(cfg)
}

func buildBaseRegistry(cfg *config.Config) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{})
	reg.Register(tools.AskUser{}) // HITL pause primitive — the LLM must see ask_user in the live manifest
	webEngine := web.NewClient(cfg)
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine}) // manifest auto-sorts (web_fetch < web_search); never hand-order
	reg.Register(&tools.SandboxExec{Runner: sandboxagent.New(cfg.SandboxAgent)})
	return reg
}

func buildRegistryWithMCP(ctx context.Context, cfg *config.Config) (*tools.Registry, []func() error, error) {
	if cfg.MCPServersErr != nil {
		return nil, nil, cfg.MCPServersErr
	}
	reg := buildBaseRegistry(cfg)
	if len(cfg.MCPServers) == 0 {
		return reg, nil, nil
	}

	serverNames := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	closers := make([]func() error, 0, len(serverNames))
	for _, name := range serverNames {
		closer, _, err := mcptools.MountServer(ctx, reg, name, cfg.MCPServers[name])
		if err != nil {
			_ = closeMCPServers(closers)
			return nil, nil, err
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
	reg, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB())
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		os.Exit(1)
	}
	defer func() { _ = closeMCPServers(closers) }()
	fmt.Print(reg.RenderText())
}
