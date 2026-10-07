package display

import (
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/web"
)

// PreviewInput is the completed call data shared by live and replay projection.
// Arguments are model-supplied and must be validated by each tool-specific parser.
type PreviewInput struct {
	ToolCallID    string
	ToolName      string
	Arguments     string
	ResultPreview string
	TrustedMCP    *TrustedMCP
}

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
//   - shell_exec / shell_poll → Terminal for verified completed output
//   - legacy shell_exec / sandbox_exec → CodeInput
//
// Every other tool, an empty preview, an inline {error,…} preview, or malformed JSON
// returns (Payload{}, false) so the caller keeps the raw escaped card (D-FALLBACK).
// The registry is untouched by all of them but one: a failed web_fetch marks the
// source it was reading as not read. Replay passes the same Arguments and preview, so
// replay D-FALLBACK == live D-FALLBACK still holds.
func NormalizeToolPreview(in PreviewInput, reg *Registry) (Payload, bool) {
	if in.ResultPreview == "" || reg == nil {
		return Payload{}, false
	}
	if in.ToolName == "web_fetch" && markFailedFetch(in, reg) {
		return Payload{}, false
	}
	if in.TrustedMCP != nil && in.ToolName != "" {
		switch in.TrustedMCP.Recipe {
		case "recipe:memory":
			return normalizeMemoryPreview(in)
		case "recipe:calendar", "recipe:whatsapp":
			return normalizeSidecarPreview(in)
		}
	}
	if in.ToolName == "task" || in.ToolName == "skill" || in.ToolName == "plugin_pack" {
		return normalizeNativeList(in)
	}
	result, ok := decodeToolPreview(in)
	if !ok {
		return Payload{}, false
	}
	return NormalizeWithRegistry(in.ToolCallID, in.ToolName, result, reg)
}

// decodeToolPreview decodes a typed tool's result preview into the concrete value
// NormalizeWithRegistry expects. An unrecognized tool, an error-shaped preview, or
// malformed JSON returns ok=false so the caller keeps the raw card (D-FALLBACK).
func decodeToolPreview(in PreviewInput) (any, bool) {
	switch in.ToolName {
	case "web_search":
		var wrap struct {
			Results []web.Result `json:"results"`
		}
		if err := json.Unmarshal([]byte(in.ResultPreview), &wrap); err != nil || wrap.Results == nil {
			return nil, false
		}
		return wrap.Results, true
	case "web_fetch":
		var page web.Page
		if err := json.Unmarshal([]byte(in.ResultPreview), &page); err != nil || page.URL == "" {
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
		if err := json.Unmarshal([]byte(in.ResultPreview), &reports); err == nil {
			return reports, true
		}
		// Background dispatch and the synchronous result deliberately share this
		// normalizer so their cockpit rows cannot drift. Decode only the member this
		// package consumes: queued is a producer-owned numeric count, not a second
		// contract for the display layer to redeclare.
		var queued struct {
			Workers *[]ChildReport `json:"workers"`
		}
		if err := json.Unmarshal([]byte(in.ResultPreview), &queued); err != nil || queued.Workers == nil {
			return nil, false
		}
		return *queued.Workers, true
	case "shell_exec":
		if in.Arguments != "" {
			return decodeTerminalPreview(in)
		}
		return shellCodeInput(in.ResultPreview), true
	case "shell_poll":
		return decodeTerminalPreview(in)
	case "sandbox_exec":
		if in.Arguments != "" {
			return decodeTerminalPreview(in)
		}
		return shellCodeInput(in.ResultPreview), true
	case "todo_write":
		return decodeTodoPreview(in)
	case "patch":
		return decodePatchDiff(in)
	case "read_file":
		return decodeReadFilePreview(in)
	case "write_file":
		return decodeWriteFilePreview(in)
	case "search_files":
		return decodeSearchFilesPreview(in)
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
