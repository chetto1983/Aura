// Aura entry point. Sub-commands:
//
//	aura serve              — run the long-lived agent runtime (default in production)
//	aura shell              — interactive REPL against the agent loop
//	aura agent dry-run      — drive a mock LoopAgent through the Budget tree, one Event per JSON line (SC#4)
//	aura web <sub>          — web tooling: doctor (SearXNG reachability) | tool web_search/web_fetch '<json>'
//	aura doctor             - aggregate runtime dependency health check
//	aura tools              — print the tool manifest (active + deferred)
//	aura db <sub>           — Postgres lifecycle (migrate|ping|status|reset)
//	aura neo4j <sub>        — Neo4j lifecycle
//	aura identity <sub>     — identity + capability_grants (list|get|grant|revoke)
//	aura profile <sub>      — filesystem Agent.md profile (show|add-fact)
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
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/adaptive/orderingcontrol"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/swarm"
	"github.com/chetto1983/aura/internal/web"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
)

func main() {
	_ = godotenv.Load()
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	preparedCtx, preparedArgs, err := prepareCLIIdempotency(context.Background(), os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cliInvocationContext = preparedCtx
	os.Args = append([]string{os.Args[0]}, preparedArgs...)
	if os.Getenv(cliIdempotencyChildEnv) != "1" {
		if handled, exitCode := executeCLIIdempotentParent(preparedCtx, preparedArgs, os.Stdout, os.Stderr); handled {
			os.Exit(exitCode)
		}
	}
	switch os.Args[1] {
	case "tools":
		printTools()
	case "mcp":
		runMCPDispatch(os.Args[2:])
	case "memory":
		runMemory(os.Args[2:])
	case "agent":
		runAgent(os.Args[2:])
	case "swarm-demo":
		runSwarmDemo(os.Args[2:])
	case "web":
		runWeb(os.Args[2:])
	case "doctor":
		runDoctor(os.Args[2:])
	case "db":
		runDB(os.Args[2:])
	case "neo4j":
		runNeo4j(os.Args[2:])
	case "objectstore":
		runObjectStore(os.Args[2:])
	case "docs":
		runDocs(os.Args[2:])
	case "documents":
		runDocuments(os.Args[2:])
	case "identity":
		runIdentity(os.Args[2:])
	case "paused-states":
		runPausedStates(os.Args[2:])
	case "task":
		runTask(os.Args[2:])
	case "retention":
		runRetention(os.Args[2:])
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

// runMCPDispatch is the `aura mcp` composition root: it opens a *pgxpool.Pool ONLY when
// the subcommand mutates the managed config (mirrors identityRecover/
// identityRecoverOperator's open-close pool lifecycle, D-12) — read-only subcommands
// (recipes/status/list/logs/doctor/tools/console) never touch the DB. Under
// server_production a pool-open failure is fatal (MCPH-07's literal "audited OR
// disallowed" requirement — a production deploy may never fall back to an unaudited
// write); under every other profile a pool-open failure degrades to a nil pool with a
// stderr warning, and mcpWriteManagedConfig's own mcpPoolRequiredErr backstop applies the
// same profile gate a second time (defense in depth) before ever reaching an unaudited
// write.
func runMCPDispatch(args []string) {
	ctx := cliInvocationContext
	var pool *pgxpool.Pool
	if mcpCommandNeedsPool(args) {
		cfg := config.LoadDB()
		p, err := db.Open(ctx, &cfg.DB)
		if err != nil {
			if cfg.Profile == config.ProfileServerProduction {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "warning: mcp: could not open an audited database pool (%v); proceeding unaudited\n", err)
		} else {
			defer p.Close()
			pool = p
		}
	}
	runMCP(ctx, pool, args)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: aura {serve|shell|doctor|chat <sub>|config <sub>|identity <sub>|paused-states <sub>|task <sub>|retention <plan|apply>|skills <sub>|agent <sub>|swarm-demo|web <doctor|tool ...>|tools|mcp <sub>|memory <sub>|db <sub>|neo4j <sub>|objectstore <sub>|docs <sub>|documents backfill|version}")
}

func buildRegistry() *tools.Registry {
	cfg := config.LoadDB()
	return buildBaseRegistry(cfg, nil)
}

type runtimeToolHandles struct {
	BackgroundShells *tools.BackgroundShells
	ShellApprovals   *tools.ShellApprovals
	// ShellPoll / ShellKill are retained so serve boot can wire their .Caps to the live
	// capability store (VERIF-7 / D-18): the pool-free manifest paths construct them with a
	// nil Caps (owner-only fail-closed), and serve.go sets Caps = the identity store once it
	// exists, making the admin cross-session poll/kill recovery exemption reachable.
	ShellPoll *tools.ShellPoll
	ShellKill *tools.ShellKill
	// SendFile is retained so serve boot can wire its .Assets to the live *assets.Service
	// (VERIF-7 / WEBART-01): the pool-free manifest / CLI paths construct it with a nil Assets
	// (path-only degrade, D-02), and serve.go sets Assets = the asset service once buildAssetService
	// has run, so an authenticated channel-driven delivery becomes an owned Garage asset.
	SendFile *tools.SendFile
}

// buildBaseRegistry is the shared composition root for every boot path. ts is the
// live scheduler store the non-deferred `task` tool persists against (D-11): serve/
// chat inject the cronTaskStore over the open pool; the pool-free manifest paths
// (`aura tools`, buildRegistry) pass nil — the tool still registers (its Spec needs no
// store) so the manifest lists it, and an Execute without a store would error loudly.
func buildBaseRegistry(cfg *config.Config, ts *cronTaskStore) *tools.Registry {
	reg, _ := buildBaseRegistryWithHandles(cfg, ts, nil)
	return reg
}

// buildBaseRegistryWithHandles threads sandboxRouter onto the box-capable tools (shell_exec /
// fs_read / fs_write / send_file / skill) so under a strict profile they execute INSIDE the
// per-identity box (plan 37-07); a nil router (the pool-free `aura tools`/manifest paths, and
// dev/local_trusted) keeps every tool host-direct byte-for-byte. web_fetch / web_search are
// deliberately NEVER routed (D-11 — they stay host-side, already SSRF-guarded).
func buildBaseRegistryWithHandles(cfg *config.Config, ts *cronTaskStore, sandboxRouter *usersandbox.SandboxRouter) (*tools.Registry, runtimeToolHandles) {
	handles := runtimeToolHandles{
		// The background registry carries the SAME sandboxRouter the synchronous tools do (37-09):
		// under a strict profile shell_exec routes background jobs into the box via startBox; a nil
		// router (pool-free manifest paths, dev/local_trusted) keeps every job host-direct.
		BackgroundShells: tools.NewBackgroundShells(sandboxRouter),
		ShellApprovals:   tools.NewShellApprovals(),
	}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{
		Registry: reg,
		Adaptive: orderingcontrol.NewToolDiscovery(
			taskStorePool(ts),
			cfg.LLM.Provider,
			cfg.LLM.Model,
		),
	})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{})
	reg.Register(tools.AskUser{}) // HITL pause primitive — the LLM must see ask_user in the live manifest
	reg.Register(newTaskTool(ts))
	reg.Register(&tools.TodoTool{}) // working-memory scratchpad (Claude Code's TodoWrite), session-scoped

	// ONE non-deferred skill tool; manifest rides in its Description (D-05/D-06). When
	// a live pool is available (serve/chat boot, ts!=nil) the write actions are wired to
	// the durable, gated Writer (11-05); the pool-free manifest path (`aura tools`) gets
	// a read-only tool whose write actions error loudly.
	reg.Register(newSkillTool(cfg, taskStorePool(ts), sandboxRouter))
	webEngine := web.NewClient(cfg)
	// web_fetch / web_search are DELIBERATELY NOT routed into the box (D-11): they stay host-side,
	// already SSRF-guarded — passing sandboxRouter here would be a scope error.
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine}) // manifest auto-sorts (web_fetch < web_search); never hand-order
	reg.Register(&tools.DocumentSearch{
		Searcher: docsToolSearcher{cfg: cfg},
		Adaptive: orderingcontrol.NewDocumentRetrieval(
			taskStorePool(ts),
			cfg.LLM.Provider,
			cfg.LLM.Model,
		),
	})
	// shell_exec is the full host terminal — THE execution surface (amendment #50 / D-15c).
	// Deferred so simple chat/web turns do not carry a giant shell schema in the hot manifest.
	// Amendment #88: workspace is the fixed /workspace working root (cfg.WorkspaceDir),
	// same value on every host-direct tool below — replacing the previous
	// process-cwd-derived default (and the fs_* tools' outright lack of a WorkspaceRoot).
	workspace := cfg.WorkspaceDir
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		}
	}
	reg.Register(&tools.ShellExec{WorkspaceRoot: workspace, Background: handles.BackgroundShells, Approvals: handles.ShellApprovals, Router: sandboxRouter})
	// shell_poll / shell_kill mirror Claude Code's BashOutput / KillBash: read new
	// output from, and terminate, a background shell_exec job. Deferred — the model
	// tool_searches for them once it holds a background shell_id to follow. The pointers
	// are retained on handles (VERIF-7) so serve boot can wire .Caps to the live capability
	// store (D-18 admin cross-session recovery); .Caps stays nil here so the pool-free
	// manifest paths keep the owner-only fail-closed default.
	sp := &tools.ShellPoll{Shells: handles.BackgroundShells}
	sk := &tools.ShellKill{Shells: handles.BackgroundShells}
	handles.ShellPoll = sp
	handles.ShellKill = sk
	reg.Register(sp)
	reg.Register(sk)
	// Native in-process filesystem hands — Claude-Code-style file ergonomics, full
	// host access, no path fence (amendment #50 / D-15c) EXCEPT the surgical
	// skills-library fence (#54 / D-43): fs_write/fs_edit refuse to write inside
	// SkillsDir so the gated skill-authoring flow cannot be bypassed.
	reg.Register(&tools.FSRead{WorkspaceRoot: workspace, Router: sandboxRouter})
	reg.Register(&tools.FSWrite{WorkspaceRoot: workspace, SkillsDir: cfg.SkillsDir, Router: sandboxRouter})
	reg.Register(&tools.FSEdit{WorkspaceRoot: workspace, SkillsDir: cfg.SkillsDir})
	reg.Register(&tools.FSGrep{WorkspaceRoot: workspace})
	reg.Register(&tools.FSGlob{WorkspaceRoot: workspace})
	// send_file hands a host file to the user as an attachment (D-05/D-06). Deferred:
	// the model tool_searches for it when it has a produced/found file to deliver; the
	// agent loop lifts its artifact Meta onto the AG-UI ArtifactDelta the channel renders.
	sf := &tools.SendFile{WorkspaceRoot: workspace, Router: sandboxRouter}
	handles.SendFile = sf
	reg.Register(sf)
	// document_index is the explicit bridge from a workspace file to document_search
	// (D-03: a produced/delivered file is NOT silently searchable). It needs a live
	// Postgres pool for the ingestion job store, so — unlike the lazy, pool-free
	// docsToolSearcher above — it only registers when the task-store pool exists
	// (serve/chat boot); the pool-free manifest path (`aura tools`) registers
	// nothing rather than a nil-backed tool, mirroring the skill-tool write-action
	// guard just above. newRuntimeDocumentIngestor is the SAME lazy per-call
	// documents.Service builder serve_channels.go and document_processor_wiring.go
	// already use for send_file/channel ingestion — reused here rather than
	// duplicated, since its IngestPath signature already satisfies
	// tools.DocumentIndexBackend byte-for-byte.
	if pool := taskStorePool(ts); pool != nil {
		reg.Register(&tools.DocumentIndex{
			Indexer:       newRuntimeDocumentIngestor(cfg, pool),
			WorkspaceRoot: workspace,
		})
	}
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
	return reg, handles
}

func buildRegistryWithMCP(ctx context.Context, cfg *config.Config, ts *cronTaskStore, sandboxRouter *usersandbox.SandboxRouter) (*tools.Registry, runtimeToolHandles, []func() error, error) {
	if cfg.MCPServersErr != nil {
		return nil, runtimeToolHandles{}, nil, cfg.MCPServersErr
	}
	reg, handles := buildBaseRegistryWithHandles(cfg, ts, sandboxRouter)
	if len(cfg.MCPServers) == 0 && len(cfg.MCPPolicies) == 0 {
		return reg, handles, nil, nil
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

	mountTimeout := mcpMountTimeout()
	closers := make([]func() error, 0, len(serverNames))
	for _, name := range serverNames {
		// D-21 fail-soft: a single dead/misconfigured server WARN-and-drops; boot
		// continues with the servers that did mount. Matches every surveyed MCP host
		// (Claude Code/Desktop, VS Code). Already-mounted servers stay registered —
		// do NOT closeMCPServers, do NOT abort. The non-deferred built-ins keep the
		// registry valid even when every MCP server is dropped (Pitfall 6).
		// A transient transport failure (the sidecar is still starting / mid-restart) is
		// retried with bounded backoff so a boot-race no longer leaves the agent with zero
		// of a server's tools until the next restart; a permanent error fails immediately.
		//
		// handshakeCtx is a SEPARATE, narrower context from ctx (the daemon-lifetime
		// process context): it bounds ONLY this server's mount attempt (all
		// MountWithRetry attempts + backoff share this one deadline, per AURA_MCP_MOUNT_
		// TIMEOUT), and is passed to mountOnce as the handshake ctx while ctx itself is
		// passed unchanged as the process ctx. A hung handshake is dropped and its
		// subprocess reaped without ctx ever being touched — so an already-mounted,
		// healthy server (whose subprocess lives on ctx) is never killed once this
		// server's mount deadline elapses (Pitfall #2).
		handshakeCtx, cancel := context.WithTimeout(ctx, mountTimeout)
		mountOnce := func(c context.Context) (func() error, []string, error) {
			if _, managed := cfg.MCPPolicies[name]; managed {
				return mcptools.MountManagedServer(ctx, c, reg, name, cfg.MCPPolicies[name])
			}
			return mcptools.MountServer(ctx, c, reg, name, cfg.MCPServers[name])
		}
		closer, mounted, err := mcptools.MountWithRetry(handshakeCtx, name, mcpMountRetryPolicy(), mountOnce)
		cancel()
		if err != nil {
			slog.Warn("mcp mount failed", "server", name, "err", err, "mount_timeout", mountTimeout.String())
			continue
		}
		// Log the mounted tool count so a server that mounts zero tools (a degraded
		// sidecar) is visible in the boot log rather than indistinguishable from a
		// silent success — the signal that was missing when memory mounted 0 tools.
		slog.Info("mcp mounted", "server", name, "tools", len(mounted))
		closers = append(closers, closer)
	}
	return reg, handles, closers, nil
}

const (
	defaultMCPMountAttempts  = 6
	defaultMCPMountBaseDelay = time.Second
	defaultMCPMountMaxDelay  = 5 * time.Second
	defaultMCPMountTimeout   = 10 // seconds (D-06/D-07): AURA_MCP_MOUNT_TIMEOUT override
	defaultMCPShutdownSecs   = 5  // seconds (D-11): AURA_MCP_SHUTDOWN_TIMEOUT override
)

// mcpMountTimeout resolves the per-server bounded handshake deadline (D-06):
// AURA_MCP_MOUNT_TIMEOUT overrides the default 10s. This bounds ONE server's whole
// MountWithRetry budget (every attempt + backoff), not one attempt individually —
// mirrors mcpMountRetryPolicy's env-override style / envutil.IntDefault.
func mcpMountTimeout() time.Duration {
	return time.Duration(envutil.IntDefault("AURA_MCP_MOUNT_TIMEOUT", defaultMCPMountTimeout)) * time.Second
}

// mcpShutdownTimeout resolves the aggregate shutdown deadline (D-11):
// AURA_MCP_SHUTDOWN_TIMEOUT overrides the default 5s. This bounds the WHOLE
// closeMCPServers fan-out, not any one closer — the existing per-transport
// closeWaitTimeout/httpCloseTimeout 5s constants are unrelated and unchanged.
func mcpShutdownTimeout() time.Duration {
	return time.Duration(envutil.IntDefault("AURA_MCP_SHUTDOWN_TIMEOUT", defaultMCPShutdownSecs)) * time.Second
}

// mcpMountRetryPolicy resolves the boot-time MCP mount retry budget. AURA_MCP_MOUNT_RETRY_ATTEMPTS
// overrides the attempt count (1 disables retry, restoring the single-shot fail-soft behavior); the
// backoff bounds use fixed defaults (1s base, 5s cap → ~17s worst-case budget for the default 6
// attempts), spent only when a server is transiently unreachable at boot.
func mcpMountRetryPolicy() mcptools.MountRetryPolicy {
	policy := mcptools.MountRetryPolicy{
		Attempts:  defaultMCPMountAttempts,
		BaseDelay: defaultMCPMountBaseDelay,
		MaxDelay:  defaultMCPMountMaxDelay,
	}
	if v := strings.TrimSpace(os.Getenv("AURA_MCP_MOUNT_RETRY_ATTEMPTS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			policy.Attempts = n
		}
	}
	return policy
}

// closeMCPServers fans out every closer concurrently under ONE aggregate
// AURA_MCP_SHUTDOWN_TIMEOUT deadline (D-11), so total shutdown wall-clock is
// bounded regardless of server count — NOT sequential N×5s. A closer that is still
// running when the aggregate deadline fires is abandoned (logged, not waited on);
// it finishes on its own already-bounded per-transport closeWaitTimeout/
// httpCloseTimeout (5s, unchanged by this function). Zero closers returns
// immediately with no goroutine spun up.
func closeMCPServers(closers []func() error) error {
	if len(closers) == 0 {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), mcpShutdownTimeout())
	defer cancel()

	var g errgroup.Group
	for i := len(closers) - 1; i >= 0; i-- {
		g.Go(closers[i])
	}
	done := make(chan error, 1)
	go func() { done <- g.Wait() }()

	select {
	case err := <-done:
		return err
	case <-shutdownCtx.Done():
		slog.Warn("mcp shutdown aggregate deadline elapsed; abandoning stragglers",
			"timeout", mcpShutdownTimeout().String(), "servers", len(closers))
		return fmt.Errorf("mcp shutdown: %w", shutdownCtx.Err())
	}
}

func printTools() {
	// Pool-free manifest path: a nil router keeps every tool host-direct (the manifest never routes).
	reg, _, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		os.Exit(1)
	}
	defer func() { _ = closeMCPServers(closers) }()
	fmt.Print(reg.RenderText())
}
