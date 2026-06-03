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
// by CallTool) stays RAW — only spec.Name is namespaced. Bridged tools are NOT
// Deferred: an MCP tool typically has a short description + a small argument schema,
// and the model MUST see that schema to call the tool correctly. The server's
// inputSchema passes through as Parameters unchanged.
func Bridge(ctx context.Context, namespace string, srv Server) ([]tools.Tool, error) {
	defs, err := srv.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tools.Tool, 0, len(defs))
	for _, d := range defs {
		params := emptyObjectSchema
		if len(strings.TrimSpace(string(d.InputSchema))) > 0 {
			params = d.InputSchema
		}
		out = append(out, &bridgedTool{
			srv:  srv,
			name: d.Name,
			spec: tools.Spec{
				Name:        namespacedName(namespace, d.Name),
				Summary:     firstLine(d.Description),
				Description: strings.TrimSpace(d.Description),
				Parameters:  params,
				Deferred:    false,
			},
		})
	}
	return out, nil
}

// Mount bridges srv's tools under namespace and registers them into reg,
// all-or-nothing. Two distinct raw tool names that sanitize to the same namespaced
// name are disambiguated with a deterministic hash suffix before registration. It
// still refuses to clobber an existing tool name (a collision with a built-in or
// another mounted server is a config error), so a mounted server can never silently
// shadow a built-in. On any residual collision NOTHING is registered and the
// colliding name is reported.
func Mount(ctx context.Context, reg *tools.Registry, namespace string, srv Server) ([]string, error) {
	bridged, err := Bridge(ctx, namespace, srv)
	if err != nil {
		return nil, err
	}
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
			suf := hashSuffix(namespace + "\x00" + bt.name)
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
