// chat subcommand for `aura chat`: the multi-thread, PERSISTED conversation REPL
// (Phase 4 / Slice 1.8). It is a hand-rolled switch group
// {list|new|resume|archive|unarchive|delete|rename|search} mirroring runDB (NOT
// cobra — the OQ1 deviation recorded for identity/paused-states). Bare `aura chat`
// starts a NEW persisted conversation REPL; `aura chat resume` (no id) resumes the
// most-recent active conversation.
//
// The REPL drives runner.Runner (D-A3-06): the Phase-3 streaming + dim tool-activity
// + per-turn cost footer + two-stage Ctrl+C UX is preserved, now sourced from the
// Runner's Event stream. On an Actions.AwaitingInput Event it renders the ask_user
// prompt inline per kind (D-A3-02) and resumes via SubmitAnswer + Turn(convID,nil).
//
// The composition root (D-A2-05) lives in bootChat: config.Load -> db.Open ->
// identity/askuser/conversations Stores + ScanOrphans (boot reconciliation, Req#12)
// + tiktoken InitEncoder -> runner.Runner.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/web"
	"github.com/jackc/pgx/v5/pgxpool"
)

// exitCommand quits the REPL cleanly when typed as a whole line (D-10).
const exitCommand = "/exit"

const chatUsage = "usage: aura chat {list|new|resume [<id>]|archive <id>|unarchive <id>|delete <id> --confirm|rename <id> <title>|search <query>}"

// chatEnv is the booted composition root shared by every chat subcommand: the
// config, the open pool, the three Stores, and the Runner that drives the REPL.
type chatEnv struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	conv     *conversations.Store
	pause    *askuser.Store
	identity *identity.Store
	run      *runner.Runner
	client   llm.Client
	mcpClose func() error
}

// close shuts the mounted MCP server(s) down (goleak-clean: stops the code-sandbox-mcp
// subprocess) then releases the pool (the OTel TracerProvider is owned by the REPL path).
func (e *chatEnv) close() {
	if e.mcpClose != nil {
		_ = e.mcpClose()
	}
	if e.pool != nil {
		e.pool.Close()
	}
}

// runChat is the `aura chat` entry point. It dispatches the subcommand group; a
// bare `aura chat` (no args) starts a NEW persisted conversation REPL.
func runChat(args []string) {
	if len(args) == 0 {
		chatNew(nil)
		return
	}
	switch args[0] {
	case "list":
		chatList(args[1:])
	case "new":
		chatNew(args[1:])
	case "resume":
		chatResume(args[1:])
	case "archive":
		chatSetStatus(args[1:], conversations.StatusArchived, "archived")
	case "unarchive":
		chatSetStatus(args[1:], conversations.StatusActive, "unarchived")
	case "delete":
		chatDelete(args[1:])
	case "rename":
		chatRename(args[1:])
	case "search":
		chatSearch(args[1:])
	default:
		fmt.Fprintln(os.Stderr, chatUsage)
		os.Exit(1)
	}
}

// bootChat builds the composition root (D-A2-05). It loads config (fail-fast on a
// missing API key with a clear message, never a panic), opens the pool, constructs
// the three Stores, runs the boot orphan scan (Req#12) BEFORE serving, initializes
// the tiktoken encoder once, and constructs the Runner.
func bootChat(ctx context.Context) *chatEnv {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, llm.ErrMissingAPIKey) || isMissingAPIKey(err) {
			fmt.Fprintln(os.Stderr, "aura chat: "+llm.ErrMissingAPIKey.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	convStore := conversations.New(pool, conversations.Config{
		RunDir:       cfg.RunDir,
		TurnCapBytes: cfg.ConversationTurnCapBytes,
	})
	pauseStore := askuser.New(pool)
	idStore := identity.New(pool)
	cacheStore := cachemetrics.New(pool)

	// Boot reconciliation GC (D-A5-02 / Req#12): reconcile orphan sidecar dirs
	// BEFORE serving. A scan failure is a WARN-level degradation, not a boot-blocker.
	if err := conversations.ScanOrphans(ctx, pool, conversations.ScanParams{
		RunDir:             cfg.RunDir,
		WarnThresholdBytes: int64(cfg.RunDirWarnThresholdBytes),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warn: boot orphan scan:", err)
	}

	// Initialize the cl100k tiktoken encoder once (D-A2-06) so the first turn does
	// not pay the vocab-parse latency.
	if err := conversations.InitEncoder(); err != nil {
		fmt.Fprintln(os.Stderr, "warn: tiktoken init:", err)
	}

	// Sandbox via code-sandbox-mcp (D-15): auto-provision the binary (no operator
	// install), spawn it over stdio, and mount sandbox_* into the agent registry.
	// Degrade clean — if the MCP server cannot start (no docker / offline), chat still
	// serves non-code turns; code execution is simply unavailable this session.
	reg := buildChatRegistry(cfg)
	mcpClose, sbx, mErr := mcptools.MountCodeSandbox(ctx, reg, "")
	if mErr != nil {
		fmt.Fprintln(os.Stderr, "warn: sandbox unavailable (code execution disabled this session):", mErr)
		mcpClose = func() error { return nil }
	} else {
		fmt.Fprintf(os.Stderr, "sandbox ready: %d tools via code-sandbox-mcp\n", len(sbx))
	}

	client := openai_compat.New(cfg.LLM)
	run := runner.New(runner.Deps{
		Conv:         convStore,
		Pause:        pauseStore,
		Identity:     idStore,
		CacheMetrics: cacheStore,
		Client:       client,
		Registry:     reg,
		LLM:          cfg.LLM,
		RunDir:       cfg.RunDir,
		PreviewCap:   cfg.ToolPreviewCap,
		EvictAfter:   cfg.ContextToolEvictAfterTurns,
	})
	return &chatEnv{cfg: cfg, pool: pool, conv: convStore, pause: pauseStore, identity: idStore, run: run, client: client, mcpClose: mcpClose}
}

// buildChatRegistry builds the agent tool registry for the chat REPL: the built-in
// tools + web tools. Sandbox code-execution tools are NOT registered here — they are
// mounted at boot from code-sandbox-mcp via mcptools.MountCodeSandbox (D-15), so they
// exist only when the MCP server is available (degrade-clean otherwise).
func buildChatRegistry(cfg *config.Config) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{})
	reg.Register(tools.AskUser{})
	webEngine := web.NewClient(cfg)
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine})
	return reg
}

// chatList prints the persisted conversations with their title + aggregates.
func chatList(args []string) {
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()

	includeArchived := false
	for _, a := range args {
		if a == "--all" || a == "--archived" {
			includeArchived = true
		}
	}
	convs, err := env.conv.List(ctx, includeArchived)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printConversationList(convs)
}

// chatSearch runs the locked FTS query and prints conv_id|seq|similarity|excerpt
// with an app-side excerpt window (D-A5-03).
func chatSearch(args []string) {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: aura chat search <query> [--limit N]")
		os.Exit(1)
	}
	query := args[0]
	limit := 20
	if v, ok := flagValue(args[1:], "--limit"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()

	hits, err := env.conv.SearchConversationTurns(ctx, query, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printSearchResults(query, hits)
}

// chatSetStatus archives / unarchives a conversation.
func chatSetStatus(args []string, status, verb string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: aura chat %s <id>\n", verb)
		os.Exit(1)
	}
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()
	if err := env.conv.UpdateStatus(ctx, args[0], status); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s %s\n", verb, args[0])
}

// chatRename sets a conversation title.
func chatRename(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aura chat rename <id> <title>")
		os.Exit(1)
	}
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()
	if err := env.conv.Rename(ctx, args[0], args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: renamed %s\n", args[0])
}

// chatDelete removes a conversation + its turns + sidecar dir, gated by --confirm.
func chatDelete(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura chat delete <id> --confirm")
		os.Exit(1)
	}
	confirmed := false
	for _, a := range args[1:] {
		if a == "--confirm" {
			confirmed = true
		}
	}
	if !confirmed {
		fmt.Fprintln(os.Stderr, "refusing — pass --confirm to delete the conversation and its turns")
		os.Exit(1)
	}
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()
	if err := env.conv.Delete(ctx, args[0]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: deleted %s\n", args[0])
}

// chatNew starts a brand-new persisted conversation REPL.
func chatNew(_ []string) {
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()
	convID, err := env.run.NewConversation(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura chat:", err)
		os.Exit(1)
	}
	runReplOrExit(ctx, env, convID)
}

// chatResume resumes a specific conversation (resume <id>) or the most-recent
// active one (resume, no id), then enters the REPL.
func chatResume(args []string) {
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()

	var convID string
	if len(args) >= 1 && args[0] != "" {
		convID = args[0]
		if _, err := env.conv.Get(ctx, convID); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		id, err := mostRecentActive(ctx, env.conv)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		convID = id
	}
	fmt.Printf("\x1b[2m· resuming %s\x1b[0m\n", convID)
	runReplOrExit(ctx, env, convID)
}

// mostRecentActive returns the id of the most-recently-active conversation, or an
// error when there is none (List is ordered last_active_at DESC).
func mostRecentActive(ctx context.Context, conv *conversations.Store) (string, error) {
	convs, err := conv.List(ctx, false)
	if err != nil {
		return "", err
	}
	if len(convs) == 0 {
		return "", fmt.Errorf("no active conversation to resume — start one with `aura chat new`")
	}
	return convs[0].ID, nil
}

// runReplOrExit runs the Runner-driven REPL, wiring the OTel TracerProvider
// (flushed on exit) + the two-stage Ctrl+C, and exits non-zero on a fatal error.
func runReplOrExit(ctx context.Context, env *chatEnv, convID string) {
	tp, err := newTracer(ctx, env.cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "otel:", err)
		os.Exit(1)
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = tp.Shutdown(sctx)
	}()

	deps := replDeps{
		in:         os.Stdin,
		out:        os.Stdout,
		errOut:     os.Stderr,
		run:        env.run,
		cfg:        env.cfg,
		convID:     convID,
		newTurnCtx: signalTurnCtx,
	}
	// Session-end Runner.Stop (auto-resolve orphan pendings + join auto-title
	// workers) is owned by chatLoop's defer so the white-box tests drain workers too.
	if err := chatLoop(ctx, deps); err != nil {
		fmt.Fprintln(os.Stderr, "aura chat:", err)
		os.Exit(1)
	}
}

// replDeps are the REPL's injectable dependencies so the loop is testable with
// scripted stdin + a fake-client Runner (no live OpenRouter, D-31).
type replDeps struct {
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
	run        *runner.Runner
	cfg        *config.Config
	convID     string
	newTurnCtx func(parent context.Context) (context.Context, context.CancelFunc, func() bool)
}
