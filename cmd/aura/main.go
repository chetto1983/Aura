// Aura entry point. Sub-commands:
//
//	aura serve              — run the long-lived agent runtime (default in production)
//	aura shell              — interactive REPL against the agent loop
//	aura agent dry-run      — drive a mock LoopAgent through the Budget tree, one Event per JSON line (SC#4)
//	aura web <sub>          — web tooling: doctor (SearXNG reachability) | tool web_search/web_fetch '<json>'
//	aura doctor             - aggregate runtime dependency health check
//	aura tools              — print the tool manifest (active + deferred)
//	aura db <sub>           — Postgres lifecycle (migrate|ping|status|reset)
//	aura identity <sub>     — identity + capability_grants (list|get|grant|revoke)
//	aura gateway grants     — durable "always approve" gateway grants (list|revoke)
//	aura profile <sub>      — filesystem Agent.md profile (show|add-fact)
//	aura paused-states <sub>— HITL pause escape hatch (list|purge --before <ISO> --confirm)
//	aura chat <sub>         — multi-thread conversation REPL (list|new|resume|archive|unarchive|delete|rename|search)
//	aura version            — print build metadata (version, commit, build date)
//
// Tabula-rasa scaffold: `tools`, `agent`, `db`, `chat`, `shell`, and
// `serve` are wired through the real runtime composition roots. The Phase-1
// `aura chat` stub + concrete Loop were removed in Slice 0.9 (Plan 02-07);
// `aura chat` returned in Phase 3 wired to a real LlmAgent.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/mcp"
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
		printTools(os.Args[2:])
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
	case "objectstore":
		runObjectStore(os.Args[2:])
	case "docs":
		runDocs(os.Args[2:])
	case "identity":
		runIdentity(os.Args[2:])
	case "gateway":
		runGateway(os.Args[2:])
	case "paused-states":
		runPausedStates(os.Args[2:])
	case "task":
		runTask(os.Args[2:])
	case "retention":
		runRetention(os.Args[2:])
	case "skills":
		runSkills(os.Args[2:])
	case "pack":
		runPack(os.Args[2:])
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
	fmt.Fprintln(os.Stderr, "usage: aura {serve|shell|doctor|chat <sub>|config <sub>|identity <sub>|gateway grants <sub>|paused-states <sub>|task <sub>|retention <plan|apply>|skills <sub>|pack <list|show|install|trust>|agent <sub>|swarm-demo|web <doctor|tool ...>|tools|mcp <sub>|memory <sub>|db <sub>|objectstore <sub>|docs <sub>|version}")
}

// buildRegistry is the pool-free, box-free registry the manifest/fixture verbs use. The nil
// router is deliberate: these paths have no identity to key a box on, so a routed tool DENIES
// rather than reaching a filesystem that is nobody's.
func buildRegistry() *tools.Registry {
	cfg := config.LoadDB()
	return buildBaseRegistry(cfg, nil)
}

type runtimeToolHandles struct {
	BackgroundShells *tools.BackgroundShells
	ShellApprovals   *tools.ShellApprovals
	Memory           *mcptools.MountedServer
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
	// MCPViews is the process-wide MCP Apps document catalog the mounts fill, and
	// ViewCallers maps ONLY the servers that actually catalogued a document to their
	// mounted supervisor — so a view's callback can never name a server that never
	// served it one. Both are nil on the pool-free manifest paths, which render
	// nothing; every *ViewCatalog method tolerates that.
	MCPViews    *mcp.ViewCatalog
	ViewCallers mcptools.ViewCallers
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

// buildBaseRegistryWithHandles threads sandboxRouter onto every box-capable tool (shell_exec, the
// five fs_* tools, send_file, document_open) so their work happens INSIDE the caller's per-identity
// box on EVERY profile (plan 37-07). A nil router — the pool-free `aura tools`/manifest paths, which
// build the tools to read their Specs and never Execute — is not a host-direct mode: Route denies,
// and so does every tool holding it. web_fetch / web_search are deliberately NEVER routed (D-11 —
// they stay host-side, already SSRF-guarded).
func buildBaseRegistryWithHandles(
	cfg *config.Config,
	ts *cronTaskStore,
	sandboxRouter *usersandbox.SandboxRouter,
) (*tools.Registry, runtimeToolHandles) {
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
	})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{Location: tools.LocationOrUTC(cfg.Timezone)})
	reg.Register(tools.AskUser{}) // HITL pause primitive — the LLM must see ask_user in the live manifest
	reg.Register(newTaskTool(ts))
	reg.Register(&tools.TodoTool{}) // working-memory scratchpad (Claude Code's TodoWrite), session-scoped

	// ONE non-deferred skill tool; manifest rides in its Description (D-05/D-06). When
	// a live pool is available (serve/chat boot, ts!=nil) the write actions are wired to
	// the durable, gated Writer (11-05); the pool-free manifest path (`aura tools`) gets
	// a read-only tool whose write actions error loudly.
	registerSkillTools(reg, cfg, taskStorePool(ts))
	webEngine := web.NewClient(cfg)
	// web_fetch / web_search are DELIBERATELY NOT routed into the box (D-11): they stay host-side,
	// already SSRF-guarded — passing sandboxRouter here would be a scope error.
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine}) // manifest auto-sorts (web_fetch < web_search); never hand-order
	// document_search is the library index: it returns DOCUMENTS, and the agent
	// opens the one it wants with document_open. Registered unconditionally so the
	// manifest always lists it (Spec reads no dependency); with no pool it fails
	// loudly at call time rather than being silently absent, which is how the
	// upload->chat regression happened once already.
	library := newDocumentLibrary(taskStorePool(ts), cfg)
	reg.Register(&tools.DocumentSearch{Library: library})
	// shell_exec is the full terminal — THE execution surface — and it runs inside the caller's
	// per-identity box, never on the host. Deferred so simple chat/web turns do not carry a giant
	// shell schema in the hot manifest. No tool below is given the HOST workspace root: every one
	// of them resolves against the box's own /workspace, which is where the agent's files live.
	reg.Register(&tools.ShellExec{Background: handles.BackgroundShells, Approvals: handles.ShellApprovals, Router: sandboxRouter})
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
	// Claude-Code-style file ergonomics over the box's filesystem — a Go port of hermes-agent's
	// four file_tools.py tools (read_file/write_file/patch/search_files, replacing the prior
	// fs_read/fs_write/fs_edit/fs_grep/fs_glob five). The skills-library fence (#54 / D-43)
	// travels with them as a box-relative rule over the materialized /skills mount, so it needs
	// no configured directory: write_file/patch refuse to write there and the gated
	// skill-authoring flow cannot be bypassed.
	reg.Register(&tools.ReadFile{Router: sandboxRouter})
	reg.Register(&tools.WriteFile{Router: sandboxRouter})
	reg.Register(&tools.Patch{Router: sandboxRouter})
	reg.Register(&tools.SearchFiles{Router: sandboxRouter})
	// send_file hands a file from the box's /workspace to the user as an attachment (D-05/D-06).
	// Deferred: the model tool_searches for it when it has a produced/found file to deliver; the
	// agent loop lifts its artifact Meta onto the AG-UI ArtifactDelta the channel renders.
	sf := &tools.SendFile{Router: sandboxRouter}
	handles.SendFile = sf
	reg.Register(sf)
	// document_index / document_describe were DELETED (2026-08-07): with the ingest bucket as the
	// source of truth, "indexing" a workspace file is not an agent action any more — putting the
	// file in the bucket is, and that is a pipeline concern, not a tool call. document_open is
	// unchanged and needs a live Postgres pool: it walks a retrieval hit's document id back
	// through the catalog to the asset and streams the ORIGINAL file into the BOX's
	// /workspace/documents/ — hence the router rather than the host workspace root, so the path it
	// reports is the one the agent's own shell_exec and read_file can then open. Retrieval answers
	// "what does it say"; a spreadsheet question is usually "how many", which no chunk contains at
	// any relevance — so the agent gets the file and computes.
	if pool := taskStorePool(ts); pool != nil {
		reg.Register(&tools.DocumentOpen{
			Documents: newRuntimeDocumentOpener(cfg, pool),
			Router:    sandboxRouter,
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

func buildRegistryWithMCP(
	ctx context.Context,
	cfg *config.Config,
	ts *cronTaskStore,
	sandboxRouter *usersandbox.SandboxRouter,
	consent mcptools.ElicitationConsent,
) (*tools.Registry, runtimeToolHandles, []func() error, error) {
	if cfg.MCPServersErr != nil {
		return nil, runtimeToolHandles{}, nil, cfg.MCPServersErr
	}
	reg, handles := buildBaseRegistryWithHandles(cfg, ts, sandboxRouter)
	handles.MCPViews = mcp.NewViewCatalog()
	handles.ViewCallers = mcptools.ViewCallers{}
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
		var mountedHost *mcptools.MountedServer
		sharedAdmin := false
		mountOnce := func(c context.Context) (func() error, []string, error) {
			server, managed := cfg.MCPPolicies[name]
			if !managed {
				return mcptools.MountServer(ctx, c, reg, name, cfg.MCPServers[name])
			}
			closer, names, host, mountErr := mcptools.MountManagedServerWithOptions(
				ctx,
				c,
				reg,
				name,
				server,
				mcptools.MountOptions{
					Egress: mcp.RuntimeEgressPolicy(cfg.Profile.Strict(), server),
					Views:  handles.MCPViews,
				},
			)
			if mountErr == nil {
				mountedHost = host
				sharedAdmin = mcp.IsSharedAdminGoverned(server)
			}
			return closer, names, mountErr
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
		if mountedHost != nil {
			if sharedAdmin {
				handles.Memory = mountedHost
			}
			// A server earns a callback entry by having served a document, not by
			// being mounted: the catalog is the record, so the two cannot drift.
			if handles.MCPViews.HasServer(name) {
				handles.ViewCallers[name] = mountedHost
			}
		}
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
	for _, v := range slices.Backward(closers) {
		g.Go(v)
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

// printTools renders the live manifest. With --json it dumps the full specs instead
// of the human summary: the adaptive tool-discovery dataset has to derive its expected
// labels from the running registry rather than have them hand-typed, which is exactly
// how the frozen benchmark ended up naming tools that do not exist.
func printTools(args []string) {
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		fmt.Fprintln(os.Stderr, "usage: aura tools [--json]")
		os.Exit(1)
	}
	// Pool-free manifest path: printing Specs never calls Execute, so a nil router costs nothing
	// and saves this verb a Docker dial. Were a tool ever executed from here it would DENY, which
	// is the right answer for a path with no identity and no box.
	reg, _, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		os.Exit(1)
	}
	defer func() { _ = closeMCPServers(closers) }()
	if !asJSON {
		fmt.Print(reg.RenderText())
		return
	}
	payload, err := reg.RenderJSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tools --json:", err)
		os.Exit(1)
	}
	fmt.Println(string(payload))
}
