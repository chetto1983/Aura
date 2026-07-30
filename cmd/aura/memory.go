package main

// `aura memory <verb>` is the operator path into the managed agent-memory sidecar.
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
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

const memoryServerName = "memory"

const memoryUsage = "usage: aura memory {search <query>|context <query>|sessions|conversation <session-id>|" +
	"add-entity <name> [type] [description]|add-fact <subject> <predicate> <object>|" +
	"add-preference <category> <preference> [--about <entity>[,<entity>]]|" +
	"store-message <session-id> <role> <content>|" +
	"get-entity <name>|facts <subject>|facts --like <text>|relationship <from> <type> <to>|" +
	"update <preference|fact|entity> <node-id> <field>=<value>...|" +
	"forget <preference|fact|entity> <node-id>}"

func runMemory(args []string) {
	ctx, err := withOperatorIdentity(context.Background())
	if err == nil {
		err = runMemoryCommand(ctx, args, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, memoryUsage)
		os.Exit(1)
	}
}

// withOperatorIdentity resolves who this CLI invocation writes and reads as, and seeds it
// on the context so every downstream call is scoped to one answer. `aura memory` and
// `aura docs` both take it: memory to own what it writes, docs because with
// AURA_MUSR_ISOLATION on an empty principal owns nothing and retrieval fails closed.
//
// It costs a Postgres round-trip on a command that otherwise only needs the sidecar. That
// is the honest price: the identity lives in Postgres, and the alternative — the hardcoded
// seed this used to fall back on — was silently addressing a tenant that first login had
// already deleted, so `aura memory` wrote a memory graph the cockpit could not see and
// read one it had not written.
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
// plus the call arguments built from the CLI positional args. Fact reads go through
// `memory_search` / `memory_get_facts`; arbitrary Cypher is NOT exposed here — the
// unscoped `graph_query` tool was removed (data-exfiltration surface), and the operator
// runs raw Cypher through `aura neo4j cypher read/write` instead.
func memoryVerbToTool(verb string, args []string) (string, map[string]any, error) {
	switch verb {
	case "search":
		q, err := arg(args, 0, "search", "<query>")
		if err != nil {
			return "", nil, err
		}
		return "memory_search", map[string]any{"query": q}, nil
	case "context":
		q, err := arg(args, 0, "context", "<query>")
		if err != nil {
			return "", nil, err
		}
		return "memory_get_context", map[string]any{"query": q}, nil
	case "sessions":
		return "memory_list_sessions", map[string]any{}, nil
	case "conversation":
		sid, err := arg(args, 0, "conversation", "<session-id>")
		if err != nil {
			return "", nil, err
		}
		return "memory_get_conversation", map[string]any{"session_id": sid}, nil
	case "add-entity":
		return memoryAddEntityArgs(args)
	case "add-fact":
		return memoryAddFactArgs(args)
	case "add-preference":
		return memoryAddPreferenceArgs(args)
	case "store-message":
		return memoryStoreMessageArgs(args)
	case "get-entity":
		name, err := arg(args, 0, "get-entity", "<name>")
		if err != nil {
			return "", nil, err
		}
		return "memory_get_entity", map[string]any{"name": name}, nil
	case "facts":
		return memoryFactsArgs(args)
	case "relationship":
		return memoryRelationshipArgs(args)
	case "update":
		return memoryUpdateArgs(args)
	case "forget":
		return memoryForgetArgs(args)
	default:
		return "", nil, fmt.Errorf("unknown memory verb %q\n%s", verb, memoryUsage)
	}
}

func memoryAddEntityArgs(args []string) (string, map[string]any, error) {
	name, err := arg(args, 0, "add-entity", "<name> [type] [description]")
	if err != nil {
		return "", nil, err
	}
	call := map[string]any{"name": name}
	if t := argOr(args, 1); t != "" {
		call["entity_type"] = t
	}
	if d := strings.Join(args[min(len(args), 2):], " "); d != "" {
		call["description"] = d
	}
	return "memory_add_entity", call, nil
}

func memoryAddFactArgs(args []string) (string, map[string]any, error) {
	if len(args) < 3 {
		return "", nil, fmt.Errorf("memory add-fact requires <subject> <predicate> <object>")
	}
	return "memory_add_fact", map[string]any{
		"subject":      args[0],
		"predicate":    args[1],
		"object_value": strings.Join(args[2:], " "),
	}, nil
}

func memoryAddPreferenceArgs(args []string) (string, map[string]any, error) {
	positional, about, err := takeAboutFlag(args)
	if err != nil {
		return "", nil, err
	}
	if len(positional) < 2 {
		return "", nil, fmt.Errorf(
			"memory add-preference requires <category> <preference> [--about <entity>[,<entity>]]")
	}
	call := map[string]any{
		"category":   positional[0],
		"preference": strings.Join(positional[1:], " "),
	}
	if len(about) > 0 {
		call["applies_to"] = about
	}
	return "memory_add_preference", call, nil
}

// takeAboutFlag pulls `--about a,b` out of the positional args. applies_to is what links
// a preference to the entities it is ABOUT; without it the preference dangles off the
// user node and recall can only reach it by wording, never by structure. Every other verb
// here is positional-only, so this stays a single recognised flag rather than a parser.
func takeAboutFlag(args []string) (positional []string, about []string, err error) {
	for i, a := range args {
		if a != "--about" {
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("memory add-preference: --about requires <entity>[,<entity>]")
		}
		names := splitCommaList(args[i+1])
		if len(names) == 0 {
			return nil, nil, fmt.Errorf("memory add-preference: --about requires at least one entity name")
		}
		return append(append([]string{}, args[:i]...), args[i+2:]...), names, nil
	}
	return args, nil, nil
}

// memoryFactsArgs maps `facts` to memory_get_facts. Facts are the one memory kind
// `memory_search` does not cover — its default memory_types are messages, entities and
// preferences — so without both forms here a stored fact is only reachable by guessing
// its subject exactly.
func memoryFactsArgs(args []string) (string, map[string]any, error) {
	if len(args) > 0 && args[0] == "--like" {
		text := strings.Join(args[1:], " ")
		if strings.TrimSpace(text) == "" {
			return "", nil, fmt.Errorf("memory facts --like requires <text>")
		}
		return "memory_get_facts", map[string]any{"query": text}, nil
	}
	subject := strings.Join(args, " ")
	if strings.TrimSpace(subject) == "" {
		return "", nil, fmt.Errorf("memory facts requires <subject> (or --like <text>)")
	}
	return "memory_get_facts", map[string]any{"subject": subject}, nil
}

func memoryStoreMessageArgs(args []string) (string, map[string]any, error) {
	if len(args) < 3 {
		return "", nil, fmt.Errorf("memory store-message requires <session-id> <role> <content>")
	}
	return "memory_store_message", map[string]any{
		"session_id": args[0],
		"role":       args[1],
		"content":    strings.Join(args[2:], " "),
	}, nil
}

func memoryRelationshipArgs(args []string) (string, map[string]any, error) {
	if len(args) < 3 {
		return "", nil, fmt.Errorf("memory relationship requires <from> <type> <to>")
	}
	return "memory_create_relationship", map[string]any{
		"from_entity":       args[0],
		"relationship_type": args[1],
		"to_entity":         args[2],
	}, nil
}

// memoryUpdateFields lists, per node type, the fields memory_update accepts and how to
// parse them. The whitelist is enforced here because the tool uses only the fields that
// apply to the node type and silently drops the rest: `update fact <id> name=x` would
// otherwise report a successful update that changed nothing.
var memoryUpdateFields = map[string]map[string]string{
	"entity":     {"name": "text", "description": "text", "subtype": "text", "aliases": "list"},
	"preference": {"preference": "text", "category": "text", "context": "text", "confidence": "number"},
	"fact":       {"subject": "text", "predicate": "text", "object_value": "text", "confidence": "number"},
}

// memoryUpdateArgs maps `update` to memory_update — the correction verb. The add_* tools
// resolve and deduplicate onto the closest existing node WITHOUT rewriting its text, so
// re-adding can never fix a wrong name or wording; forget-and-re-add loses the node's id
// and its relationships. This edits by id instead, and refreshes the node's embedding so
// the vector index stops answering under the old wording.
func memoryUpdateArgs(args []string) (string, map[string]any, error) {
	if len(args) < 3 {
		return "", nil, fmt.Errorf(
			"memory update requires <preference|fact|entity> <node-id> <field>=<value> [<field>=<value>...]")
	}
	nodeType := strings.ToLower(args[0])
	fields, ok := memoryUpdateFields[nodeType]
	if !ok {
		return "", nil, fmt.Errorf(
			"memory update: unknown node type %q; use preference, fact or entity", args[0])
	}
	call := map[string]any{"node_type": nodeType, "node_id": args[1]}
	for _, assignment := range args[2:] {
		name, value, found := strings.Cut(assignment, "=")
		if !found {
			return "", nil, fmt.Errorf("memory update: %q is not <field>=<value>", assignment)
		}
		kind, ok := fields[name]
		if !ok {
			return "", nil, fmt.Errorf("memory update: a %s has no field %q; it accepts %s",
				nodeType, name, strings.Join(slices.Sorted(maps.Keys(fields)), ", "))
		}
		parsed, err := parseMemoryField(kind, name, value)
		if err != nil {
			return "", nil, err
		}
		call[name] = parsed
	}
	return "memory_update", call, nil
}

func parseMemoryField(kind, name, value string) (any, error) {
	switch kind {
	case "list":
		return splitCommaList(value), nil
	case "number":
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("memory update: %s must be a number, got %q", name, value)
		}
		return number, nil
	default:
		return value, nil
	}
}

func splitCommaList(value string) []string {
	var items []string
	for item := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

// memoryForgetArgs maps the `forget` subverb to the memory_forget MCP tool: delete a
// preference or fact the caller owns, by id. Ownership is enforced server-side (a node
// the caller does not own is refused, not silently ignored).
func memoryForgetArgs(args []string) (string, map[string]any, error) {
	if len(args) < 2 {
		return "", nil, fmt.Errorf("memory forget requires <preference|fact|entity> <node-id>")
	}
	return "memory_forget", map[string]any{
		"node_type": args[0],
		"node_id":   args[1],
	}, nil
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

// scopeMemoryArgs stamps the caller's identity on every memory call. The memory server
// treats a missing user_identifier as "no scope", so an unstamped call wrote a
// :Conversation with a NULL owner and zero HAS_CONVERSATION edges — data owned by nobody,
// invisible to every scoped read meant to return it, with anything extracted from it
// landing in the "global" deduplication scope where it can never merge with the
// owner-scoped entities the agent records.
//
// It takes the identity off the context and does NOT invent one. The previous fallback to
// identityctx.LocalOperatorIdentity looked fail-closed and was not: first login retires
// that seed, so the CLI addressed a deleted tenant while the cockpit used the enrolled
// one, and the two never saw each other's memory. runMemory resolves the real owner up
// front (withOperatorIdentity); an unscoped context here means that resolution was
// skipped, which is a wiring bug worth failing on rather than papering over — the failure
// mode it would paper over is writing a user's memory under the wrong identity.
//
// A caller-supplied user_identifier is left alone: the paths that pass one explicitly
// (serve_recall.go's per-conversation owner, an operator inspecting another identity)
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
