package mcptools

import (
	"encoding/json"
	"log/slog"
	"sort"
)

// This file is the D-21/D-33/D-34 multiplex surface a curated MCP tool needs to
// classify per action instead of flat. bridge_risk.go's mcpToolRisk/
// classifyToolRisk classify a *sdkmcp.Tool SPEC once at bridge time, with no call
// args at all — once a fork collapses many raw tools into one curated tool, that
// function has nothing to key its per-action decision on any more. The decision
// therefore moves entirely to the exported surface below, consumed by
// internal/gateway's classifyCalendarAction (classify.go, which DOES see the
// model-supplied rawArgs). isKnownMultiplexedMCPTool and reconcileCuratedActions
// are called from bridge.go's specFromToolDefWithPolicy — this file supplies the
// decision, bridge.go is where it is actually wired into a spec.

const (
	// calendarCuratedTool is the fork's own advertised tool name (measured live
	// 2026-08-22 against ghcr.io/chetto1983/aura-pim-mcp:38c94fd9d... — tools/list
	// returns exactly one tool named "calendar"), NOT yet namespaced.
	calendarCuratedTool = "calendar"
	// calendarNamespace MUST equal internal/mcp/manager.BuiltInCatalog's PIM
	// catalog entry Name ("calendar") — TestMultiplexCalendarNamespaceMatchesCatalog
	// asserts the two agree, so a catalog rename cannot silently orphan the
	// classifier key below.
	calendarNamespace = "calendar"
)

// CalendarMultiplexedToolName is the model-facing name of the curated calendar
// tool: <namespace>__<tool> via namespacedName (design doc §6: calendar__calendar).
// This is the exact multiplexedClassifiers key internal/gateway registers its
// classifier under, so the Multiplexed flag and the classifier can never drift
// apart under a namespace rename.
var CalendarMultiplexedToolName = namespacedName(calendarNamespace, calendarCuratedTool)

// multiplexedMCPTools maps each curated MCP tool's model-facing name to the
// trustedRecipeActions source its action table lives under. This is the ONLY
// place a curated MCP tool becomes "known multiplexed" to
// isKnownMultiplexedMCPTool/MCPActionClassFor — plan 46-08 adds the WhatsApp
// entry here and nowhere else.
var multiplexedMCPTools = map[string]string{
	CalendarMultiplexedToolName: calendarRecipeSource,
}

// MultiplexedMCPTools returns the model-facing names of every curated
// multiplexed MCP tool Aura knows, sorted. internal/gateway's cross-package
// invariant test walks this to assert every name has exactly one
// multiplexedClassifiers entry, failing if either side is extended alone.
func MultiplexedMCPTools() []string {
	out := make([]string, 0, len(multiplexedMCPTools))
	for name := range multiplexedMCPTools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// MCPActionClassFor resolves tool's recipe source through multiplexedMCPTools and
// looks action up in trustedRecipeActions[source]. A tool that is not a known
// curated multiplex, or an action absent from its table, returns
// MCPActionUnknown — internal/gateway's classifier saturates that to
// scoring.Risky, never Safe (the loud half of D-33's fail-closed pair; the WARN
// half is reconcileCuratedActions below).
func MCPActionClassFor(tool, action string) MCPActionClass {
	source, ok := multiplexedMCPTools[tool]
	if !ok {
		return MCPActionUnknown
	}
	actions, ok := trustedRecipeActions[source]
	if !ok {
		return MCPActionUnknown
	}
	if class, ok := actions[action]; ok {
		return class
	}
	return MCPActionUnknown
}

// isKnownMultiplexedMCPTool is the ONLY signal specFromToolDefWithPolicy may use
// to set spec.Multiplexed (D-34). Inferring Multiplexed from "does this tool's
// schema have an action property" would make gateway.ValidateClassifiable panic
// at boot for ANY stranger's server whose schema happens to use an `action`
// argument — the opposite of what a generic MCP host promises (SC#6). Gating on
// classifier existence instead means an unknown server lands on the generic
// fail-closed tier, never a boot panic.
func isKnownMultiplexedMCPTool(name string) bool {
	_, ok := multiplexedMCPTools[name]
	return ok
}

// curatedActionEnum parses properties.action.enum out of a curated tool's raw
// JSON-Schema params, tolerating an absent action property or unparseable input
// by returning nil — D-33's reconciliation is a WARN, never a panic, and there is
// nothing to warn about beyond what capSchemaDescriptions already logged.
func curatedActionEnum(params json.RawMessage) []string {
	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(params, &schema); err != nil {
		return nil
	}
	return schema.Properties.Action.Enum
}

// reconcileCuratedActions WARNs, by name, in both directions when a curated
// tool's LIVE schema enum and trustedRecipeActions[source] disagree (D-33): the
// action names live in another repository and can drift without Aura noticing. A
// boot panic was rejected — MCP mounts are fail-soft by design, and a rename in
// another repo is an outage, not a wiring bug. An unrecognised action still
// saturates to Risky at call time via MCPActionClassFor/the gateway classifier,
// so this WARN is the loud half of that fail-closed pair, not the only defense.
func reconcileCuratedActions(toolName, source string, params json.RawMessage) {
	enum := curatedActionEnum(params)
	if enum == nil {
		return
	}
	inEnum := make(map[string]struct{}, len(enum))
	for _, action := range enum {
		inEnum[action] = struct{}{}
		if _, ok := trustedRecipeActions[source][action]; !ok {
			slog.Warn("mcp curated tool advertises an action with no risk-table entry",
				"tool", toolName, "action", action)
		}
	}
	for action := range trustedRecipeActions[source] {
		if _, ok := inEnum[action]; !ok {
			slog.Warn("mcp risk table has an action the curated tool no longer advertises",
				"tool", toolName, "action", action)
		}
	}
}
