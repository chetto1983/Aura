package display

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	maxTerminalPreviewBytes = 64 << 10
	maxTerminalCommandBytes = 4096
	maxTerminalLines        = 2000
)

// Terminal is a completed shell result. Output is the tool's combined stream in
// original order; it must never be labelled as separate stdout/stderr streams.
type Terminal struct {
	Command    string `json:"command"`
	Output     string `json:"output"`
	Cwd        string `json:"cwd,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

func decodeTerminalPreview(in PreviewInput) (Terminal, bool) {
	if len(in.Arguments) > maxTerminalPreviewBytes || len(in.ResultPreview) > maxTerminalPreviewBytes || strings.Count(in.ResultPreview, "\n") > maxTerminalLines {
		return Terminal{}, false
	}
	body, raw, foreground := splitShellFooter(in.ResultPreview)
	if raw == "" || len(body) > maxTerminalPreviewBytes {
		return Terminal{}, false
	}
	var terminal Terminal
	switch in.ToolName {
	case "shell_exec", "sandbox_exec":
		if !foreground {
			return Terminal{}, false
		}
		var args struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(in.Arguments), &args) != nil || strings.TrimSpace(args.Command) == "" || len(args.Command) > maxTerminalCommandBytes {
			return Terminal{}, false
		}
		var footer struct {
			ExitCode   *int    `json:"exit_code"`
			Cwd        *string `json:"cwd"`
			DurationMS *int64  `json:"duration_ms"`
			TimedOut   bool    `json:"timed_out"`
			Cancelled  bool    `json:"cancelled"`
		}
		if json.Unmarshal([]byte(raw), &footer) != nil || footer.ExitCode == nil || *footer.ExitCode < 0 || *footer.ExitCode > 255 || footer.Cwd == nil || footer.DurationMS == nil || *footer.DurationMS < 0 || len(*footer.Cwd) > 1024 || footer.TimedOut || footer.Cancelled {
			return Terminal{}, false
		}
		terminal.Command, terminal.ExitCode, terminal.Cwd, terminal.DurationMS = args.Command, *footer.ExitCode, *footer.Cwd, *footer.DurationMS
	case "shell_poll":
		if foreground {
			return Terminal{}, false
		}
		var args struct {
			ShellID string `json:"shell_id"`
		}
		if json.Unmarshal([]byte(in.Arguments), &args) != nil || strings.TrimSpace(args.ShellID) == "" || len(args.ShellID) > 256 {
			return Terminal{}, false
		}
		var footer struct {
			ShellID string `json:"shell_id"`
			Status  string `json:"status"`
		}
		if json.Unmarshal([]byte(raw), &footer) != nil || footer.ShellID != args.ShellID || !strings.HasPrefix(footer.Status, "exited:") {
			return Terminal{}, false
		}
		exit, err := strconv.Atoi(strings.TrimPrefix(footer.Status, "exited:"))
		if err != nil || exit < 0 || exit > 255 {
			return Terminal{}, false
		}
		terminal.Command, terminal.ExitCode = "shell_poll "+args.ShellID, exit
	default:
		return Terminal{}, false
	}
	terminal.Output = body
	terminal.Truncated = strings.HasPrefix(body, "[output truncated:") || strings.HasPrefix(body, "[background output truncated:")
	return terminal, true
}
