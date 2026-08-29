package display

import (
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/web"
)

// NormalizeToolPreview is the SINGLE decode+normalize site shared by the live agent
// loop and the replay snapshot projection (D-06, "one normalizer for live + replay").
// It reverses a typed tool's model-visible result preview — the exact string the runner
// persists as the RoleTool turn Content AND streams as the live TOOL_CALL_RESULT — back
// into the concrete value Normalize switches on, then runs the shared normalizer with
// the caller-owned per-turn registry. Because live and replay both consume the SAME
// preview shape, the re-derived Payload is identical by construction (Pitfall 4: no
// preview-vs-full-bytes drift).
//
// The tools this protocol wires are recognized:
//   - web_search → []web.Result (the adapter marshals {"results":[…]}, web_search.go)
//   - web_fetch  → web.Page     (the adapter marshals the Page directly)
//   - swarm_spawn → []ChildReport (the adapter marshals the ordered reports, swarm/report.go)
//   - shell_exec / sandbox_exec → CodeInput (the command output text, structured footer stripped)
//
// Every other tool, an empty preview, an inline {error,…} preview, or malformed JSON
// returns (Payload{}, false) so the caller keeps the raw escaped card (D-FALLBACK) and
// the registry is untouched — replay D-FALLBACK == live D-FALLBACK.
func NormalizeToolPreview(toolCallID, toolName, preview string, reg *Registry) (Payload, bool) {
	if preview == "" || reg == nil {
		return Payload{}, false
	}
	result, ok := decodeToolPreview(toolName, preview)
	if !ok {
		return Payload{}, false
	}
	return NormalizeWithRegistry(toolCallID, toolName, result, reg)
}

// decodeToolPreview decodes a typed tool's result preview into the concrete value
// NormalizeWithRegistry expects. An unrecognized tool, an error-shaped preview, or
// malformed JSON returns ok=false so the caller keeps the raw card (D-FALLBACK).
func decodeToolPreview(toolName, preview string) (any, bool) {
	switch toolName {
	case "web_search":
		var wrap struct {
			Results []web.Result `json:"results"`
		}
		if err := json.Unmarshal([]byte(preview), &wrap); err != nil || wrap.Results == nil {
			return nil, false
		}
		return wrap.Results, true
	case "web_fetch":
		var page web.Page
		if err := json.Unmarshal([]byte(preview), &page); err != nil || page.URL == "" {
			return nil, false
		}
		return page, true
	case "swarm_spawn":
		// The swarm result preview is the compact JSON of the ordered []ChildReport
		// (swarm/report.go marshalReports → tools.NewResult). display.ChildReport's tags
		// match swarm.ChildReport byte-for-byte (payload.go), so it decodes straight in.
		// A non-array preview (the over-cap / context-unavailable inline error strings)
		// fails to unmarshal → raw card, exactly as live.
		var reports []ChildReport
		if err := json.Unmarshal([]byte(preview), &reports); err == nil {
			return reports, true
		}
		// Background dispatch and the synchronous result deliberately share this
		// normalizer so their cockpit rows cannot drift.
		var queued struct {
			Queued  bool           `json:"queued"`
			Note    string         `json:"note"`
			Workers *[]ChildReport `json:"workers"`
		}
		if err := json.Unmarshal([]byte(preview), &queued); err != nil || queued.Workers == nil {
			return nil, false
		}
		return *queued.Workers, true
	case "shell_exec", "sandbox_exec":
		// A code-producing tool's preview is plain combined output text, not JSON; wrap it
		// as the code body. Lang is empty (shell output has no single language) and the
		// structured "[aura_shell {…}]" footer is stripped — its exit code already shows on
		// the tool card. (Local-file artifacts ride the separate aura.artifact seam.)
		return CodeInput{Body: stripShellFooter(preview)}, true
	default:
		return nil, false
	}
}

// stripShellFooter drops the trailing structured "[aura_shell {json}]" /
// "[aura_shell_bg {json}]" footer that tools/shell_exec.go appendShellFooter adds to the
// model preview, leaving the human-readable command output as the code body. The marker
// is matched conservatively (own-line, preview ends with the footer's closing "]"); an
// unrecognized shape returns the preview unchanged (graceful degradation).
func stripShellFooter(preview string) string {
	if !strings.HasSuffix(preview, "]") {
		return preview
	}
	for _, marker := range []string{"\n[aura_shell ", "\n[aura_shell_bg "} {
		if i := strings.LastIndex(preview, marker); i >= 0 {
			return strings.TrimRight(preview[:i], "\n")
		}
	}
	return preview
}
