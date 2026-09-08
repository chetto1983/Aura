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
		// normalizer so their cockpit rows cannot drift. Decode only the member this
		// package consumes: queued is a producer-owned numeric count, not a second
		// contract for the display layer to redeclare.
		var queued struct {
			Workers *[]ChildReport `json:"workers"`
		}
		if err := json.Unmarshal([]byte(preview), &queued); err != nil || queued.Workers == nil {
			return nil, false
		}
		return *queued.Workers, true
	case "shell_exec", "sandbox_exec":
		return shellCodeInput(preview), true
	default:
		return nil, false
	}
}

// splitShellFooter extracts the trailing structured "[aura_shell {json}]" /
// "[aura_shell_bg {json}]" footer that tools/shell_exec.go appendShellFooter adds to the
// model preview, leaving the human-readable command output as the code body. The marker
// is matched conservatively (own-line, preview ends with the footer's closing "]"); an
// unrecognized shape returns the preview unchanged (graceful degradation).
func splitShellFooter(preview string) (body, footer string, foreground bool) {
	if !strings.HasSuffix(preview, "]") {
		return preview, "", false
	}
	last, selected := -1, ""
	for _, marker := range []string{"\n[aura_shell ", "\n[aura_shell_bg "} {
		if i := strings.LastIndex(preview, marker); i > last {
			last, selected = i, marker
		}
	}
	if last < 0 {
		return preview, "", false
	}
	return strings.TrimRight(preview[:last], "\n"), preview[last+len(selected) : len(preview)-1], selected == "\n[aura_shell "
}

func shellCodeInput(preview string) CodeInput {
	body, raw, foreground := splitShellFooter(preview)
	in := CodeInput{Body: body}
	var footer struct {
		Cancelled  bool    `json:"cancelled"`
		TimedOut   *bool   `json:"timed_out"`
		ExitCode   *int    `json:"exit_code"`
		Cwd        *string `json:"cwd"`
		DurationMS *int64  `json:"duration_ms"`
	}
	if !foreground || json.Unmarshal([]byte(raw), &footer) != nil {
		return in
	}
	// Older retained results omitted cancelled. Their foreground footer had no
	// exit code and the host appended this marker; ordinary output still has an exit code.
	legacyCancelled := footer.ExitCode == nil && footer.TimedOut != nil && !*footer.TimedOut && footer.Cwd != nil && footer.DurationMS != nil && *footer.DurationMS >= 0 && strings.HasSuffix(body, "[command cancelled]")
	in.Cancelled = footer.Cancelled || legacyCancelled
	return in
}
