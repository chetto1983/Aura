package agent

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/tokenjuice"
)

// argvFromArgs extracts an argv-style slice from tool arguments.
// For execute_shell/execute_code the "command" field is split on whitespace.
// For other tools the "action" field becomes argv[0] when present.
func argvFromArgs(args map[string]any) []string {
	if cmd, ok := args["command"].(string); ok && cmd != "" {
		return strings.Fields(cmd)
	}
	if action, ok := args["action"].(string); ok && action != "" {
		return []string{action}
	}
	return nil
}

// commandFromArgs returns the raw command string for execute_shell/execute_code,
// or "" for tools that carry no command string.
func commandFromArgs(args map[string]any) string {
	if cmd, ok := args["command"].(string); ok {
		return cmd
	}
	return ""
}

// exitCodeFromToolOutput parses the "exit_code: N" prefix produced by
// execute_shell and execute_code. Returns nil for all other tool names.
func exitCodeFromToolOutput(toolName, raw string) *int {
	switch toolName {
	case "execute_shell", "execute_code":
	default:
		return nil
	}
	for _, line := range strings.SplitN(raw, "\n", 4) {
		if rest, ok := strings.CutPrefix(line, "exit_code: "); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
				return &n
			}
		}
	}
	return nil
}

// compactToolOutput runs TokenJuice on a tool result and returns the (possibly
// compacted) string. A debug log is emitted each call regardless of whether a
// rule fired so the operator can observe the pass-through rate too.
func compactToolOutput(logger *slog.Logger, toolName string, args map[string]any, raw string) string {
	r := tokenjuice.Compact(tokenjuice.Input{
		ToolName: toolName,
		Argv:     argvFromArgs(args),
		Command:  commandFromArgs(args),
		Stdout:   raw,
		ExitCode: exitCodeFromToolOutput(toolName, raw),
	}, tokenjuice.Options{})
	if logger != nil {
		logger.Debug("tokenjuice",
			"tool", toolName,
			"bytes_in", r.Stats.OriginalBytes,
			"bytes_out", r.Stats.CompactedBytes,
			"ratio", r.Stats.Ratio(),
			"rule", r.RuleID,
			"applied", r.Applied,
		)
	}
	if r.Applied {
		return r.InlineText
	}
	return raw
}
