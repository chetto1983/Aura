// Package mcptools bridges a generic MCP server's tools into Aura's agent tool
// registry: it lists the server's tools (tools/list) and adapts each to a
// tools.Tool whose Execute routes through the MCP client's tools/call. This is the
// platform seam: mount any stdio MCP server and its tools become first-class
// agent tools, cache-friendly (Deferred) and spill-aware
// (results flow through tools.NewResult).
//
// It depends on both internal/mcp (transport) and internal/agent/tools (the agent
// contract) via a narrow Server interface, so the generic mcp client stays
// agent-agnostic and the bridge stays unit-testable with a fake.
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// Server is the narrow MCP surface the bridge needs; *mcp.Client satisfies it.
// Declared consumer-side so the bridge is testable without spawning a process.
type Server interface {
	ListTools(ctx context.Context) ([]mcp.ToolDef, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}

// emptyObjectSchema is the Parameters fallback for a tool whose server advertised
// no inputSchema — a valid "any/no args" JSON-Schema object.
var emptyObjectSchema = json.RawMessage(`{"type":"object"}`)

// bridgedTool adapts one MCP tool to tools.Tool.
type bridgedTool struct {
	srv  Server
	name string
	spec tools.Spec
}

func (b *bridgedTool) Spec() tools.Spec { return b.spec }

// Execute unmarshals the model's args, calls the MCP tool, and threads the text
// content through tools.NewResult (spill/preview aware). A tool-level failure is
// returned to the model as inline `error: ...` content so it self-corrects
// (mirrors web_search's contract), NOT as a loop-fatal Go error; only a missing
// tool-call context (NewResult precondition) propagates as a Go error.
func (b *bridgedTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	var args map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return tools.ToolResult{}, fmt.Errorf("mcp tool %s args: %w", b.name, err)
		}
	}
	text, err := b.srv.CallTool(ctx, b.name, args)
	if err != nil {
		return tools.NewResult(ctx, "error: "+err.Error())
	}
	return tools.NewResult(ctx, text)
}

// Bridge lists srv's tools and adapts each to a tools.Tool, namespacing the
// model-facing name as <namespace>__<tool> (sanitized, 64-byte capped) so a mounted
// server can never silently shadow a built-in. The wire name (bridgedTool.name, used
// by CallTool) stays RAW — only spec.Name is namespaced. The server's inputSchema
// passes through as Parameters unchanged.
//
// Bridged tools are Deferred: a real multi-tool MCP server (spike 001: 16 mail
// tools; spike 002: 12 whatsapp tools) would otherwise flood every per-turn manifest
// straight into the 30-50-tool degradation zone the 8.1 BM25 tool_search was built to
// defend. The model sees only name+summary by default and loads the full spec on
// demand via tool_search, which indexes the deferred tool's name, description, and
// argument-field names — so deferred MCP tools stay fully discoverable.
func Bridge(ctx context.Context, namespace string, srv Server) ([]tools.Tool, error) {
	defs, err := srv.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	return bridgeTools(namespace, srv, defs), nil
}

func bridgeTools(namespace string, srv Server, defs []mcp.ToolDef) []tools.Tool {
	out := make([]tools.Tool, 0, len(defs))
	for _, d := range defs {
		params := emptyObjectSchema
		if schema := strings.TrimSpace(string(d.InputSchema)); schema != "" && schema != "null" {
			params = d.InputSchema
		}
		out = append(out, &bridgedTool{
			srv:  srv,
			name: d.Name,
			spec: tools.Spec{
				Name: namespacedName(namespace, d.Name),
				// The deferred stub is all the model sees by default — append the
				// required arg names so a first call doesn't have to guess the
				// shape, fail validation, tool_search the schema, and retry (live
				// swarm E2E 2026-06-04: that discovery round-trip cost each fresh
				// worker context ~2 extra LLM turns on whatsapp send_message).
				Summary:     firstLine(d.Description) + requiredArgsHint(params),
				Description: strings.TrimSpace(d.Description),
				Parameters:  params,
				Deferred:    true,
			},
		})
	}
	return out
}

// Mount bridges all of srv's advertised tools under namespace and registers them
// into reg, all-or-nothing. Two distinct raw tool names that sanitize to the same
// namespaced name are disambiguated with a deterministic hash suffix before
// registration. It still refuses to clobber an existing tool name (a collision with
// a built-in or another mounted server is a config error), so a mounted server can
// never silently shadow a built-in. On any residual collision NOTHING is registered
// and the colliding name is reported.
func Mount(ctx context.Context, reg *tools.Registry, namespace string, srv Server) ([]string, error) {
	bridged, err := Bridge(ctx, namespace, srv)
	if err != nil {
		return nil, err
	}
	return registerBridged(reg, bridged)
}

func registerBridged(reg *tools.Registry, bridged []tools.Tool) ([]string, error) {
	seenRaw := make(map[string]struct{}, len(bridged))
	chosen := make(map[string]struct{}, len(bridged))
	for _, t := range bridged {
		bt := t.(*bridgedTool)
		if _, dup := seenRaw[bt.name]; dup {
			return nil, fmt.Errorf("mcp bridge: server advertised duplicate tool %q", bt.name)
		}
		seenRaw[bt.name] = struct{}{}

		name := bt.spec.Name
		// Distinct raw names whose namespaced form collides get a deterministic hash
		// suffix (keyed on the RAW name, so each survivor is unique). Re-truncate first
		// so the 13-byte suffix never pushes an already-capped name past maxToolNameLen.
		if _, dup := chosen[name]; dup {
			suf := hashSuffix(bt.spec.Name + "\x00" + bt.name)
			if len(name)+len(suf) > maxToolNameLen {
				name = name[:maxToolNameLen-len(suf)]
			}
			name += suf
			bt.spec.Name = name
		}
		if _, exists := reg.Get(name); exists {
			return nil, fmt.Errorf("mcp bridge: tool %q already registered (collision)", name)
		}
		if _, dup := chosen[name]; dup {
			return nil, fmt.Errorf("mcp bridge: tool name %q still collides after disambiguation", name)
		}
		chosen[name] = struct{}{}
	}
	names := make([]string, 0, len(bridged))
	for _, t := range bridged {
		reg.Register(t)
		names = append(names, t.Spec().Name)
	}
	return names, nil
}

// firstLine returns the first non-empty trimmed line of s — the one-line manifest
// summary. MCP descriptions are "<summary>. \n<detail>", so the first line is the
// summary.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}

// requiredArgsHint renders " Required args: a, b." from a JSON-schema's required
// list so the deferred stub teaches the call shape without carrying the full
// schema. Returns "" when the schema has no required list (nothing to teach —
// the model can call with {}).
func requiredArgsHint(schema json.RawMessage) string {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || len(s.Required) == 0 {
		return ""
	}
	return " Required args: " + strings.Join(s.Required, ", ") + "."
}
