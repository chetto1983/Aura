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
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

const memoryServerName = "memory"

const memoryUsage = "usage: aura memory {search <query>|context <query>|sessions|conversation <session-id>|" +
	"add-entity <name> [type] [description]|add-fact <subject> <predicate> <object>|" +
	"add-preference <category> <preference>|store-message <session-id> <role> <content>|" +
	"get-entity <name>|relationship <from> <type> <to>|export|" +
	"trace {start <session-id> <task>|step <trace-id> <observation>|complete <trace-id> [outcome]|observations <session-id>}|" +
	"query <cypher>}"

func runMemory(args []string) {
	if err := runMemoryCommand(context.Background(), args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, memoryUsage)
		os.Exit(1)
	}
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

// memoryVerbToTool maps an `aura memory <verb>` to its RAW `memory_*` (or read-only
// `graph_query`) wire tool name plus the call arguments built from the CLI positional
// args. There is NO standalone `memory_get_facts` on the live surface (Open Q4); fact
// reads go through `memory_search` or the read-only `query` (graph_query) verb.
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
	case "relationship":
		return memoryRelationshipArgs(args)
	case "export":
		return "memory_export_graph", map[string]any{}, nil
	case "trace":
		return memoryTraceArgs(args)
	case "query":
		cypher, err := arg(args, 0, "query", "<cypher>")
		if err != nil {
			return "", nil, err
		}
		return "graph_query", map[string]any{"query": cypher}, nil
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
	if len(args) < 2 {
		return "", nil, fmt.Errorf("memory add-preference requires <category> <preference>")
	}
	return "memory_add_preference", map[string]any{
		"category":   args[0],
		"preference": strings.Join(args[1:], " "),
	}, nil
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

// memoryTraceArgs maps the `trace` reasoning-memory subverbs to the live MCP tool
// contract (verified against the running sidecar, spike 035 / plan 15-05): a trace is
// keyed by session_id+task at start, steps and completion are keyed by trace_id, and
// observations are read back BY SESSION (memory_get_observations takes session_id, not
// trace_id). The step free-text lands in `observation` so it is recallable.
func memoryTraceArgs(args []string) (string, map[string]any, error) {
	sub, err := arg(args, 0, "trace", "{start <session-id> <task>|step <trace-id> <observation>|complete <trace-id> [outcome]|observations <session-id>}")
	if err != nil {
		return "", nil, err
	}
	rest := args[1:]
	switch sub {
	case "start":
		if len(rest) < 2 {
			return "", nil, fmt.Errorf("memory trace start requires <session-id> <task>")
		}
		return "memory_start_trace", map[string]any{
			"session_id": rest[0],
			"task":       strings.Join(rest[1:], " "),
		}, nil
	case "step":
		if len(rest) < 2 {
			return "", nil, fmt.Errorf("memory trace step requires <trace-id> <observation>")
		}
		return "memory_record_step", map[string]any{
			"trace_id":    rest[0],
			"observation": strings.Join(rest[1:], " "),
		}, nil
	case "complete":
		id, err := arg(rest, 0, "trace complete", "<trace-id> [outcome]")
		if err != nil {
			return "", nil, err
		}
		call := map[string]any{"trace_id": id}
		if outcome := strings.Join(rest[min(len(rest), 1):], " "); outcome != "" {
			call["outcome"] = outcome
		}
		return "memory_complete_trace", call, nil
	case "observations":
		sid, err := arg(rest, 0, "trace observations", "<session-id>")
		if err != nil {
			return "", nil, err
		}
		return "memory_get_observations", map[string]any{"session_id": sid}, nil
	default:
		return "", nil, fmt.Errorf("unknown memory trace subverb %q", sub)
	}
}

// callMemoryTool resolves the managed memory sidecar, opens it over streamable-HTTP,
// and calls the RAW tool name directly — the same shape as mcp_tools.go's managed
// open path. A 20s timeout fails fast on a dead sidecar (T-15-03-03) instead of hanging.
func callMemoryTool(ctx context.Context, tool string, args map[string]any, out io.Writer) error {
	server, ok, err := effectiveManagedMCPServer(memoryServerName)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("memory MCP server is not configured or is disabled; the managed %q recipe must be mounted", memoryServerName)
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cli, err := mcp.OpenServer(callCtx, memoryServerName, server)
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()
	text, err := cli.CallTool(callCtx, tool, args)
	if err != nil {
		return err
	}
	return writeln(out, text)
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
