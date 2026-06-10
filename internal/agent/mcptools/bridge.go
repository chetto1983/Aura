// Package mcptools bridges a generic MCP server's tools into Aura's agent tool
// registry: it lists the server's tools and adapts each to a tools.Tool whose
// Execute routes through the MCP client's tools/call.
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

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
// no inputSchema: a valid "any/no args" JSON-Schema object.
var emptyObjectSchema = json.RawMessage(`{"type":"object"}`)

// bridgedTool adapts one MCP tool to tools.Tool. The spec is atomically swapped
// when reconnectingServer refreshes tools/list after a transport reconnect.
type bridgedTool struct {
	srv  Server
	name string
	spec atomic.Value
}

func (b *bridgedTool) Spec() tools.Spec { return b.spec.Load().(tools.Spec) }

func (b *bridgedTool) storeSpec(spec tools.Spec) {
	b.spec.Store(spec)
}

func (b *bridgedTool) refreshSpec(d mcp.ToolDef) {
	params, summary, description := specFieldsFromToolDef(d)
	spec := b.Spec()
	spec.Summary = summary
	spec.Description = description
	spec.Parameters = params
	spec.Deferred = true
	b.spec.Store(spec)
}

// Execute unmarshals the model's args, calls the MCP tool, and threads the text
// content through tools.NewResult. A tool-level failure is returned to the model
// as inline `error: ...` content so it self-corrects, not as a loop-fatal Go
// error; only a missing tool-call context propagates as a Go error.
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
// model-facing name as <namespace>__<tool> so a mounted server can never silently
// shadow a built-in. The wire name used by CallTool stays raw.
//
// Bridged tools are Deferred: a real multi-tool MCP server would otherwise flood
// every per-turn manifest. tool_search indexes the deferred tool's name,
// description, and argument-field names, so deferred MCP tools stay discoverable.
func Bridge(ctx context.Context, namespace string, srv Server) ([]tools.Tool, error) {
	defs, err := srv.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	bridged := bridgeTools(namespace, srv, defs)
	if tracker, ok := srv.(interface{ trackBridgedTools([]tools.Tool) }); ok {
		tracker.trackBridgedTools(bridged)
	}
	return bridged, nil
}

func bridgeTools(namespace string, srv Server, defs []mcp.ToolDef) []tools.Tool {
	out := make([]tools.Tool, 0, len(defs))
	for _, d := range defs {
		bt := &bridgedTool{srv: srv, name: d.Name}
		bt.storeSpec(specFromToolDef(namespace, d))
		out = append(out, bt)
	}
	return out
}

func specFromToolDef(namespace string, d mcp.ToolDef) tools.Spec {
	params, summary, description := specFieldsFromToolDef(d)
	return tools.Spec{
		Name:        namespacedName(namespace, d.Name),
		Summary:     summary,
		Description: description,
		Parameters:  params,
		Deferred:    true,
	}
}

func specFieldsFromToolDef(d mcp.ToolDef) (json.RawMessage, string, string) {
	params := emptyObjectSchema
	if schema := strings.TrimSpace(string(d.InputSchema)); schema != "" && schema != "null" {
		params = d.InputSchema
	}
	description := strings.TrimSpace(d.Description)
	return params, firstLine(d.Description) + requiredArgsHint(params), description
}

// Mount bridges all of srv's advertised tools under namespace and registers them
// into reg, all-or-nothing. Two distinct raw tool names that sanitize to the same
// namespaced name are disambiguated with a deterministic hash suffix before
// registration. It still refuses to clobber an existing tool name.
func Mount(ctx context.Context, reg *tools.Registry, namespace string, srv Server) ([]string, error) {
	bridged, err := Bridge(ctx, namespace, srv)
	if err != nil {
		return nil, err
	}
	names, err := registerBridged(reg, bridged)
	if err != nil {
		return nil, err
	}
	if hook, ok := srv.(interface{ setRefreshHook(func()) }); ok {
		hook.setRefreshHook(func() { invalidateToolSearch(reg) })
	}
	return names, nil
}

func invalidateToolSearch(reg *tools.Registry) {
	if t, ok := reg.Get("tool_search"); ok {
		if ts, ok := t.(*tools.ToolSearch); ok {
			ts.InvalidateIndex()
		}
	}
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

		spec := bt.Spec()
		name := spec.Name
		if _, dup := chosen[name]; dup {
			suf := hashSuffix(spec.Name + "\x00" + bt.name)
			if len(name)+len(suf) > maxToolNameLen {
				name = name[:maxToolNameLen-len(suf)]
			}
			name += suf
			spec.Name = name
			bt.storeSpec(spec)
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

// firstLine returns the first non-empty trimmed line of s.
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
// schema. Returns "" when the schema has no required list.
func requiredArgsHint(schema json.RawMessage) string {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || len(s.Required) == 0 {
		return ""
	}
	return " Required args: " + strings.Join(s.Required, ", ") + "."
}
