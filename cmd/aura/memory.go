package main

// `aura memory <verb>` is the operator path into the managed memory sidecar.
// It opens the sidecar over streamable-HTTP and calls the RAW `memory_*` MCP tools
// directly, bypassing the agent loop. This is the deliberate, explicit write +
// pull-on-demand recall surface (D-01/D-02/D-03): writes happen only when an operator
// invokes a write verb — never via passive every-turn extraction. The `memory__`
// namespace belongs to the agent registry/bridge, NOT this wire-level call, so every
// dispatched tool name is the RAW wire name (e.g. `memory_search`, not
// `memory__memory_search`).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

const memoryServerName = "memory"

// memoryUsage mirrors the ArcadeDB memory MCP surface, one verb per tool.
const memoryUsage = "usage: aura memory {search <query> [--as-of <RFC3339>]|" +
	"facts <entity> [predicate] [--as-of <RFC3339>]|entities|digest|" +
	"remember <subject> <predicate> <object> <statement> [--subject-kind <kind>] [--object-kind <kind>] [--supersedes]|" +
	"merge <duplicate> <survivor>|" +
	"forget [--entity <name>] [--subject <s> --predicate <p> --object <o>] [--run <id>] [--apply]|" +
	"reembed [--all]|schema}"

func runMemory(args []string) {
	ctx, err := withOperatorIdentity(cliInvocationContext)
	if err == nil {
		err = runMemoryCommand(ctx, args, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, memoryUsage)
		os.Exit(1)
	}
}

// withOperatorIdentity resolves the current operator from Postgres and places that
// identity on the context before any owner-scoped memory or document call.
func withOperatorIdentity(ctx context.Context) (context.Context, error) {
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("resolving the operator identity needs Postgres: %w", err)
	}
	defer pool.Close()
	identityID, err := identityctx.OperatorIdentity(ctx, pool)
	if err != nil {
		return nil, err
	}
	return identityctx.WithIdentityID(ctx, identityID), nil
}

func runMemoryCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", memoryUsage)
	}
	verb := args[0]
	rest := args[1:]
	tool, toolArgs, err := memoryVerbToTool(verb, rest)
	if err != nil {
		return err
	}
	return callMemoryTool(ctx, tool, toolArgs, out)
}

// memoryVerbToTool maps an `aura memory <verb>` to its RAW `memory_*` wire tool name
// plus the call arguments built from the CLI positional args. It is ONE VERB PER TOOL
// on purpose: every advertised verb must map to a live MCP tool.
//
// Arbitrary graph queries are deliberately absent; graph_schema provides bounded
// schema inspection without exposing an unscoped read surface.
func memoryVerbToTool(verb string, args []string) (string, map[string]any, error) {
	switch verb {
	case "search":
		q, err := arg(args, 0, "search", "<query>")
		if err != nil {
			return "", nil, err
		}
		call := map[string]any{"query": q}
		if asOf := takeFlag(&args, "--as-of"); asOf != "" {
			call["as_of"] = asOf
		}
		return "memory_search", call, nil
	case "facts":
		entity, err := arg(args, 0, "facts", "<entity> [predicate]")
		if err != nil {
			return "", nil, err
		}
		call := map[string]any{"entity": entity}
		if asOf := takeFlag(&args, "--as-of"); asOf != "" {
			call["as_of"] = asOf
		}
		if predicate := argOr(args, 1); predicate != "" {
			call["predicate"] = predicate
		}
		return "memory_facts_about", call, nil
	case "entities":
		return "memory_entities", map[string]any{}, nil
	case "digest":
		return "memory_digest", map[string]any{}, nil
	case "remember":
		return memoryRememberArgs(args)
	case "merge":
		return memoryMergeArgs(args)
	case "forget":
		return memoryForgetArgs(args)
	case "reembed":
		// Hidden from the model on purpose (mcptools.memoryHiddenFromModel): rewriting
		// every vector is an operator's answer to an embedder change, not a move an
		// agent makes mid-turn. The CLI calls the raw wire tool, so it reaches it anyway.
		return "memory_reembed", map[string]any{"all": takeBoolFlag(&args, "--all")}, nil
	case "schema":
		return "graph_schema", map[string]any{}, nil
	default:
		return "", nil, fmt.Errorf("unknown memory verb %q\n%s", verb, memoryUsage)
	}
}

// memoryRememberArgs builds the one write the surface has. The statement is a
// separate argument rather than derived from the triple because it is what gets
// indexed and searched, and a restated triple retrieves badly.
func memoryRememberArgs(args []string) (string, map[string]any, error) {
	supersedes := takeBoolFlag(&args, "--supersedes")
	run := takeFlag(&args, "--run")
	subjectKind := takeFlag(&args, "--subject-kind")
	objectKind := takeFlag(&args, "--object-kind")
	if len(args) < 4 {
		return "", nil, fmt.Errorf(
			"memory remember requires <subject> <predicate> <object> <statement> [--subject-kind <kind>] [--object-kind <kind>] [--supersedes] [--run <id>]")
	}
	if run == "" {
		run = cliRunID(time.Now().UTC())
	}
	call := map[string]any{
		"subject":    args[0],
		"predicate":  args[1],
		"object":     args[2],
		"statement":  strings.Join(args[3:], " "),
		"supersedes": supersedes,
		"source":     map[string]any{"run_id": run},
	}
	if subjectKind != "" {
		call["subject_kind"] = subjectKind
	}
	if objectKind != "" {
		call["object_kind"] = objectKind
	}
	return "memory_upsert_fact", call, nil
}

// cliRunID groups a day's hand-typed facts under one run.
//
// The source run lets an operator remove everything a manual entry run produced.
// A day is the granularity that makes the undo discoverable: `aura memory forget --run
// cli-2026-08-02` reverses a session of manual entry, and an operator can type that id
// from memory. Per-invocation would be more precise and completely undiscoverable.
// --run overrides it when a caller wants its own grouping (a rescue, an import).
func cliRunID(now time.Time) string { return "cli-" + now.Format("2006-01-02") }

func memoryMergeArgs(args []string) (string, map[string]any, error) {
	if len(args) < 2 {
		return "", nil, fmt.Errorf("memory merge requires <duplicate> <survivor>")
	}
	return "memory_merge_entities", map[string]any{
		"source": args[0],
		"target": args[1],
	}, nil
}

// memoryForgetArgs defaults to a DRY RUN. Forgetting is the one irreversible act
// on this surface — an upsert supersedes and a merge moves facts, but forget
// destroys — so the operator opts IN to it with --apply.
func memoryForgetArgs(args []string) (string, map[string]any, error) {
	apply := takeBoolFlag(&args, "--apply")
	call := map[string]any{"dry_run": !apply}
	for _, spec := range []struct{ flag, key string }{
		{"--entity", "entity"},
		{"--subject", "subject"},
		{"--predicate", "predicate"},
		{"--object", "object"},
	} {
		if v := takeFlag(&args, spec.flag); v != "" {
			call[spec.key] = v
		}
	}
	if run := takeFlag(&args, "--run"); run != "" {
		call["source"] = map[string]any{"run_id": run}
	}
	if len(call) == 1 {
		return "", nil, fmt.Errorf(
			"memory forget needs something to match: --entity, a --subject/--predicate/--object triple, or --run")
	}
	return "memory_forget", call, nil
}

// takeFlag removes "--name value" from args and returns the value.
func takeFlag(args *[]string, name string) string {
	for i, a := range *args {
		if a == name && i+1 < len(*args) {
			value := (*args)[i+1]
			*args = append((*args)[:i], (*args)[i+2:]...)
			return value
		}
	}
	return ""
}

func takeBoolFlag(args *[]string, name string) bool {
	for i, a := range *args {
		if a == name {
			*args = append((*args)[:i], (*args)[i+1:]...)
			return true
		}
	}
	return false
}

// callMemoryTool resolves the managed memory sidecar, opens it over streamable-HTTP,
// and calls the RAW tool name directly — the same shape as mcp_tools.go's managed
// open path. A 20s timeout fails fast on a dead sidecar (T-15-03-03) instead of hanging.
func callMemoryTool(ctx context.Context, tool string, args map[string]any, out io.Writer) error {
	text, err := callMemoryToolText(ctx, tool, args)
	if err != nil {
		return err
	}
	return writeln(out, text)
}

// openMemoryMCP resolves the managed memory sidecar and opens ONE connection to it. The
// returned Transport carries its MCP session across every CallTool until Close, so a
// caller with several writes to make (the onboarding seed) handshakes once instead of
// once per write (Amendment #95). The caller owns the Close.
func openMemoryMCP(ctx context.Context) (mcp.Transport, error) {
	server, ok, err := effectiveManagedMCPServer(memoryServerName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("memory MCP server is not configured or is disabled; the managed %q recipe must be mounted", memoryServerName)
	}
	return openManagedMCPTransport(ctx, memoryServerName, server)
}

// callMemoryToolText is the text-returning core of callMemoryTool (shared with the
// runner's L4 archival-recall seam, serve_adapters.go). It opens the managed memory
// sidecar for ONE call of the RAW tool name and returns the tool's text result. A 20s
// timeout fails fast on a dead sidecar (T-15-03-03).
func callMemoryToolText(ctx context.Context, tool string, args map[string]any) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cli, err := openMemoryMCP(callCtx)
	if err != nil {
		return "", err
	}
	defer func() { _ = cli.Close() }()
	scoped, err := scopeMemoryArgs(callCtx, args)
	if err != nil {
		return "", err
	}
	return cli.CallTool(callCtx, tool, scoped)
}

// scopeMemoryArgs stamps the caller's identity on every memory call. The ArcadeDB
// memory MCP uses user_identifier to resolve the caller's isolated database and
// credential, so an unstamped call must never reach the wire.
//
// It takes the identity off the context and does NOT invent one. runMemory resolves
// the real owner up front; an unscoped context here is a wiring bug that must fail
// rather than write memory under the wrong identity.
//
// A caller-supplied user_identifier is left alone: the paths that pass one explicitly
// (for example, a per-conversation owner or an operator inspecting another identity)
// must not be silently rescoped.
func scopeMemoryArgs(ctx context.Context, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if existing, ok := args["user_identifier"].(string); ok && existing != "" {
		return args, nil
	}
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		return nil, errors.New(
			"memory call has no identity to scope to: resolve the operator identity before calling")
	}
	args["user_identifier"] = identityID
	return args, nil
}

func arg(args []string, i int, verb, placeholder string) (string, error) {
	if i >= len(args) || strings.TrimSpace(args[i]) == "" {
		return "", fmt.Errorf("memory %s requires %s", verb, placeholder)
	}
	return args[i], nil
}

func argOr(args []string, i int) string {
	if i >= len(args) {
		return ""
	}
	return args[i]
}
