// Package mcptools bridges a generic MCP server's tools into Aura's agent tool
// registry: it lists the server's tools and adapts each to a tools.Tool whose
// Execute routes through the mounted MountedServer's tools/call.
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// emptyObjectSchema is the Parameters fallback for a tool whose server advertised
// no inputSchema: a valid "any/no args" JSON-Schema object.
var emptyObjectSchema = json.RawMessage(`{"type":"object"}`)

const maxMCPDescriptionBytes = 4096
const maxMCPSummaryBytes = 768
const maxMCPArgDescBytes = 512
const maxMCPSchemaBytes = 16 * 1024
const maxMCPSchemaProperties = 128
const maxMCPErrorPreviewBytes = 2048
const mcpArgDescTruncated = " [truncated]"
const mcpErrorTruncated = " [error truncated]"

// bridgedTool adapts one MCP tool to tools.Tool. The spec is atomically swapped
// when MountedServer's onToolListChanged refreshes an in-place spec change.
type bridgedTool struct {
	srv         *MountedServer
	name        string
	callTimeout time.Duration
	policy      bridgePolicy
	// view is the MCP Apps document this tool's result renders in, zero when the
	// server declared none or the mount may not render (bridge_views.go). Read at
	// mount and never refreshed: onToolListChanged accepts a same-name-set change,
	// and a server that repoints a tool at a different document mid-run would move
	// the operator's rendering surface underneath them.
	view mcp.ViewRef
	spec atomic.Value
}

func (b *bridgedTool) Spec() tools.Spec { return b.spec.Load().(tools.Spec) }

func (b *bridgedTool) storeSpec(spec tools.Spec) {
	b.spec.Store(spec)
}

// schemaJSON converts an SDK Tool's InputSchema (an `any` holding the client
// side's default JSON marshaling of the server's schema — a map[string]any, not
// json.RawMessage) to bytes, falling back to emptyObjectSchema on a nil schema or
// a marshal error rather than forwarding something unparseable.
func schemaJSON(t *sdkmcp.Tool) json.RawMessage {
	if t == nil || t.InputSchema == nil {
		return emptyObjectSchema
	}
	raw, err := json.Marshal(t.InputSchema)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return emptyObjectSchema
	}
	return raw
}

func (b *bridgedTool) refreshSpec(t *sdkmcp.Tool) {
	params, summary, description := specFieldsFromToolDef(t)
	spec := b.Spec()
	oldMutating := spec.Mutating
	oldDestructive := spec.Destructive
	oldRequired := requiredArgNames(spec.Parameters)
	spec.Summary = summary
	spec.Description = description
	spec.Parameters = params
	spec.Deferred = b.policy.defaultDeferred()
	spec.Mutating, spec.Destructive = mcpToolRisk(b.policy, t)
	applyMCPOperationMetadata(&spec)
	if oldMutating != spec.Mutating {
		slog.Warn("mcp tool mutating flag changed on reconnect",
			"tool", spec.Name,
			"old_mutating", oldMutating,
			"new_mutating", spec.Mutating,
		)
	}
	if oldDestructive != spec.Destructive {
		slog.Warn("mcp tool destructive flag changed on reconnect",
			"tool", spec.Name,
			"old_destructive", oldDestructive,
			"new_destructive", spec.Destructive,
		)
	}
	if newRequired := requiredArgNames(params); !sameStrings(oldRequired, newRequired) {
		slog.Warn("mcp tool required args changed on reconnect",
			"tool", spec.Name,
			"old_required", strings.Join(oldRequired, ","),
			"new_required", strings.Join(newRequired, ","),
		)
	}
	b.spec.Store(spec)
}

// Bridge lists srv's tools and adapts each to a tools.Tool, namespacing the
// model-facing name as <namespace>__<tool> so a mounted server can never silently
// shadow a built-in. The wire name used by CallToolText stays raw.
//
// Bridged tools are Deferred by default: a real multi-tool MCP server would
// otherwise flood every per-turn manifest. tool_search indexes the deferred
// tool's name, description, and argument-field names, so deferred MCP tools stay
// discoverable. The memory MCP schemas are therefore reached through tool_search
// instead of being carried on every turn.
func Bridge(ctx context.Context, namespace string, srv *MountedServer) ([]tools.Tool, error) {
	advertised, err := srv.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	return bridgeFromAdvertised(namespace, srv, advertised)
}

// bridgeFromAdvertised bridges PRE-LISTED advertised tools, skipping the
// srv.ListTools round-trip Bridge itself makes. mountWithAdvertisedPolicy (the
// initial-mount path) uses this so the FIRST discovery listing goes through the
// raw session's own ctx bound instead of through MountedServer, which treats any
// transport error (including a caller's ctx deadline expiring) as a cue to
// transparently redial using ITS OWN redial-timeout budget
// (context.WithoutCancel-severed from the caller's ctx) — layering that
// independent, much longer budget on top of the initial mount's OWN bounded
// handshake ctx would silently blow through AURA_MCP_MOUNT_TIMEOUT, defeating the
// very bound mount.go installs. The raw session's own tools/list (no redial
// layer) is called BEFORE MountedServer even wraps it, so this failure mode
// cannot occur for the initial mount; bridged tools still reference srv (the
// mounted supervisor) for every CALL after mount, so runtime redial-on-transport-
// error is unaffected.
func bridgeFromAdvertised(namespace string, srv *MountedServer, advertised []*sdkmcp.Tool) ([]tools.Tool, error) {
	return bridgeFromAdvertisedWithPolicy(namespace, srv, advertised, defaultBridgePolicy(namespace))
}

func bridgeFromAdvertisedWithPolicy(namespace string, srv *MountedServer, advertised []*sdkmcp.Tool, policy bridgePolicy) ([]tools.Tool, error) {
	callTimeout, err := configuredMCPCallTimeout()
	if err != nil {
		return nil, err
	}
	bridged := bridgeToolsWithPolicy(namespace, srv, advertised, callTimeout, policy)
	srv.trackBridgedTools(bridged)
	return bridged, nil
}

func bridgeTools(namespace string, srv *MountedServer, advertised []*sdkmcp.Tool, callTimeout time.Duration) []tools.Tool {
	return bridgeToolsWithPolicy(namespace, srv, advertised, callTimeout, defaultBridgePolicy(namespace))
}

// bridgeToolsWithPolicy is the ONLY place bridging scores the always-loaded
// slot decision (D-27, bridge_deferral.go): it runs at MOUNT only (reached from
// Mount/mountWithAdvertisedPolicy), never on reconnect, so policy.alwaysLoaded
// is computed once here and frozen into every bridgedTool this call builds.
// policy is a value parameter, so this mutation is local to this call and each
// bridgedTool below receives its own enriched copy — that copy IS the freeze
// refreshSpec later relies on never being recomputed.
func bridgeToolsWithPolicy(namespace string, srv *MountedServer, advertised []*sdkmcp.Tool, callTimeout time.Duration, policy bridgePolicy) []tools.Tool {
	policy.modelFacingCount = countModelFacing(policy, advertised)
	policy.alwaysLoaded = grantLoadedSlot(namespace, policy.modelFacingCount)
	out := make([]tools.Tool, 0, len(advertised))
	for _, t := range advertised {
		if !policy.modelFacing(t.Name) {
			continue
		}
		bt := &bridgedTool{srv: srv, name: t.Name, callTimeout: callTimeout, policy: policy, view: viewRefFor(policy, t)}
		bt.storeSpec(specFromToolDefWithPolicy(namespace, t, policy))
		out = append(out, bt)
	}
	return out
}

func specFromToolDef(namespace string, t *sdkmcp.Tool) tools.Spec {
	return specFromToolDefWithPolicy(namespace, t, defaultBridgePolicy(namespace))
}

func specFromToolDefWithPolicy(namespace string, t *sdkmcp.Tool, policy bridgePolicy) tools.Spec {
	params, summary, description := specFieldsFromToolDef(t)
	mutating, destructive := mcpToolRisk(policy, t)
	spec := tools.Spec{
		Name:        namespacedName(namespace, t.Name),
		Summary:     summary,
		Description: description,
		Parameters:  params,
		Deferred:    policy.defaultDeferred(),
		Mutating:    mutating,
		Destructive: destructive,
	}
	applyMCPOperationMetadata(&spec)
	return spec
}

func applyMCPOperationMetadata(spec *tools.Spec) {
	if spec.Mutating {
		spec.OperationScope = tools.OperationScopeMCP
		spec.OperationNormalizer = tools.OperationNormalizerCanonical
		spec.ReplayPolicy = tools.ReplayToolResult
		return
	}
	spec.OperationScope = ""
	spec.OperationNormalizer = ""
	spec.ReplayPolicy = ""
}

func specFieldsFromToolDef(t *sdkmcp.Tool) (json.RawMessage, string, string) {
	params := capSchemaDescriptions(schemaJSON(t))
	summary := frameMCPSummary(t.Description, params)
	description := frameMCPDescription(t.Description)
	return params, summary, description
}

// capSchemaDescriptions truncates every "description" string anywhere in an
// MCP-provided JSON Schema to maxMCPArgDescBytes (B-15): a server-controlled
// argument description is an injection/flood surface that otherwise reaches the
// model and tool_search uncapped. On any parse/marshal failure it falls back to
// the empty-object schema rather than forwarding the raw bytes.
func capSchemaDescriptions(raw json.RawMessage) json.RawMessage {
	if len(raw) > maxMCPSchemaBytes {
		return emptyObjectSchema
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return emptyObjectSchema
	}
	if countSchemaProperties(v, 0) > maxMCPSchemaProperties {
		return emptyObjectSchema
	}
	capDescriptions(v, 0)
	out, err := json.Marshal(v)
	if err != nil {
		return emptyObjectSchema
	}
	if len(out) > maxMCPSchemaBytes {
		return emptyObjectSchema
	}
	return out
}

// capDescriptions walks a decoded JSON value and truncates every "description"
// string (at any nesting — property descriptions, items, $defs) to the cap. The
// depth bound stops a pathological deeply-nested schema from exhausting the stack.
func capDescriptions(v any, depth int) {
	if depth > 16 {
		return
	}
	switch n := v.(type) {
	case map[string]any:
		for k, val := range n {
			if k == "description" {
				if s, ok := val.(string); ok && len(s) > maxMCPArgDescBytes {
					n[k] = truncateUTF8Bytes(s, maxMCPArgDescBytes) + mcpArgDescTruncated
				}
				continue
			}
			capDescriptions(val, depth+1)
		}
	case []any:
		for _, item := range n {
			capDescriptions(item, depth+1)
		}
	}
}

func countSchemaProperties(v any, depth int) int {
	if depth > 16 {
		return 0
	}
	switch n := v.(type) {
	case map[string]any:
		total := 0
		for k, val := range n {
			if k == "properties" {
				if props, ok := val.(map[string]any); ok {
					total += len(props)
				}
			}
			total += countSchemaProperties(val, depth+1)
		}
		return total
	case []any:
		total := 0
		for _, item := range n {
			total += countSchemaProperties(item, depth+1)
		}
		return total
	default:
		return 0
	}
}

func capMCPErrorContent(err error) string {
	text := "error: " + err.Error()
	if len(text) <= maxMCPErrorPreviewBytes {
		return text
	}
	limit := max(maxMCPErrorPreviewBytes-len(mcpErrorTruncated), 0)
	return truncateUTF8Bytes(text, limit) + mcpErrorTruncated
}

type boundedBridgeError struct {
	cause   error
	message string
}

func (e *boundedBridgeError) Error() string { return e.message }
func (e *boundedBridgeError) Unwrap() error { return e.cause }

func boundedMCPError(err error) error {
	if err == nil {
		return nil
	}
	text := capMCPErrorContent(err)
	return &boundedBridgeError{
		cause: err, message: strings.TrimPrefix(text, "error: "),
	}
}

// frameMCPSummary renders a bounded, model-facing summary from the server's raw
// description plus a required-args hint. The server text is trusted content, so
// no distrust prefix is added; the byte cap (B-15) still bounds the flood surface.
func frameMCPSummary(raw string, params json.RawMessage) string {
	summary := firstLine(raw)
	if summary == "" {
		summary = "none provided"
	}
	hint := requiredArgsHint(params)
	const marker = " [summary truncated]"
	if len(summary)+len(hint) <= maxMCPSummaryBytes {
		return summary + hint
	}
	budget := maxMCPSummaryBytes - len(marker) - len(hint)
	if budget >= 0 {
		return truncateUTF8Bytes(summary, budget) + marker + hint
	}
	hintBudget := maxMCPSummaryBytes - len(marker)
	if hintBudget <= 0 {
		return truncateUTF8Bytes(marker, maxMCPSummaryBytes)
	}
	return marker + truncateUTF8Bytes(hint, hintBudget)
}

// frameMCPDescription returns the server's description bounded to the byte cap
// (B-15). The text is trusted content; only its length is defended, not its intent.
func frameMCPDescription(raw string) string {
	desc := strings.TrimSpace(raw)
	if desc == "" {
		return "none provided."
	}
	const marker = "\n[description truncated]"
	if len(desc) <= maxMCPDescriptionBytes {
		return desc
	}
	limit := max(maxMCPDescriptionBytes-len(marker), 0)
	return truncateUTF8Bytes(desc, limit) + marker
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	out := s[:maxBytes]
	for len(out) > 0 && !utf8.ValidString(out) {
		out = out[:len(out)-1]
	}
	return out
}

// Mount bridges all of srv's advertised tools under namespace and registers them
// into reg, all-or-nothing. Two distinct raw tool names that sanitize to the same
// namespaced name are disambiguated with a deterministic hash suffix before
// registration. It still refuses to clobber an existing tool name.
func Mount(ctx context.Context, reg *tools.Registry, namespace string, srv *MountedServer) ([]string, error) {
	bridged, err := Bridge(ctx, namespace, srv)
	if err != nil {
		return nil, err
	}
	return finishMount(reg, srv, bridged)
}

func mountWithAdvertisedPolicy(reg *tools.Registry, namespace string, srv *MountedServer, advertised []*sdkmcp.Tool, policy bridgePolicy) ([]string, error) {
	bridged, err := bridgeFromAdvertisedWithPolicy(namespace, srv, advertised, policy)
	if err != nil {
		return nil, err
	}
	return finishMount(reg, srv, bridged)
}

func finishMount(reg *tools.Registry, srv *MountedServer, bridged []tools.Tool) ([]string, error) {
	names, err := registerBridged(reg, bridged)
	if err != nil {
		return nil, err
	}
	srv.setRefreshHook(func() { invalidateToolSearch(reg) })
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
	for ln := range strings.SplitSeq(s, "\n") {
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

func requiredArgNames(schema json.RawMessage) []string {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || len(s.Required) == 0 {
		return nil
	}
	required := append([]string(nil), s.Required...)
	sort.Strings(required)
	return required
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
