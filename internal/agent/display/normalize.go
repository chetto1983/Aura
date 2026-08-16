package display

import "github.com/chetto1983/aura/internal/web"

// NormalizeWithRegistry is the dispatch (DISP-01 / D-FALLBACK): it maps a structured
// tool result to a typed Payload, switching on the tool name. An unrecognized tool --
// or a recognized tool handed a result of the wrong concrete type -- returns
// (Payload{}, false) so the caller renders the raw, escaped tool card (HARDEN-08).
//
// result must be the concrete decoded value the live path holds (Pitfall 4):
//   - web_search  -> []web.Result
//   - web_fetch   -> web.Page
//   - swarm_spawn -> []ChildReport
//   - shell_exec / sandbox_exec -> CodeInput
//   - any tool's error -> *web.WebError
//
// It takes an explicit tool-call id (the sseAdapter
// correlation key) and a caller-owned per-turn source Registry, so several
// web_search calls in one turn accumulate into one numbered source list (D-05).
func NormalizeWithRegistry(toolCallID, toolName string, result any, reg *Registry) (Payload, bool) {
	// An error result crosses into system_event regardless of which tool produced it
	// (any tool can fail), so it is checked before the per-tool result type-switch —
	// otherwise a web_fetch error would miss the per-tool web.Page assertion and fall
	// through as unrecognized instead of rendering its sanitized system_event.
	if we, ok := result.(*web.WebError); ok {
		return normalizeWebError(toolCallID, we)
	}
	switch toolName {
	case "web_search":
		results, ok := result.([]web.Result)
		if !ok {
			return Payload{}, false
		}
		return normalizeWebSearch(toolCallID, results, reg)
	case "web_fetch":
		page, ok := result.(web.Page)
		if !ok {
			return Payload{}, false
		}
		return normalizeWebFetch(toolCallID, page, reg)
	case "swarm_spawn":
		reports, ok := result.([]ChildReport)
		if !ok {
			return Payload{}, false
		}
		return normalizeSwarm(toolCallID, reports)
	case "shell_exec", "sandbox_exec":
		in, ok := result.(CodeInput)
		if !ok {
			return Payload{}, false
		}
		return normalizeCode(toolCallID, in)
	default:
		return Payload{}, false
	}
}
