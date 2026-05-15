package attempts

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

const (
	brieferMaxPerTool = 200  // max chars per tool line
	brieferMaxTools   = 8    // max tools surfaced in one capsule
	brieferMaxTotal   = 1500 // max total capsule length in chars
)

// Briefer produces a one-turn tool-experience capsule injected before each LLM
// round. It summarises recent per-tool failures so the model can adapt its
// strategy without accessing the full tool_attempts history itself (US-J04).
type Briefer struct {
	repo   Repo
	logger *slog.Logger
}

// NewBriefing creates a Briefer backed by the given Repo.
func NewBriefing(repo Repo, logger *slog.Logger) *Briefer {
	return &Briefer{repo: repo, logger: logger}
}

type briefEntry struct {
	line     string
	lastSeen time.Time
}

// Brief queries recent tool failures for threadID and returns a formatted
// capsule, or an empty string when there is nothing to report. The capsule is
// ephemeral — callers must not persist it; it lives for one LLM turn only.
//
// MCP tools absent from availableSet (offline servers) are annotated with
// "(currently unavailable)".
func (b *Briefer) Brief(ctx context.Context, threadID string, toolNames []string, availableSet map[string]struct{}) string {
	if b.repo == nil || len(toolNames) == 0 {
		return ""
	}

	var entries []briefEntry
	for _, name := range toolNames {
		rows, err := b.repo.Recent(ctx, threadID, name, 3)
		if err != nil {
			if b.logger != nil {
				b.logger.Error("briefer: repo.Recent failed", "tool", name, "err", err)
			}
			continue
		}

		var failures []ToolAttempt
		for _, row := range rows {
			switch row.Outcome {
			case "recoverable", "blocked", "fatal":
				failures = append(failures, row)
			}
		}
		if len(failures) == 0 {
			continue
		}

		last := failures[0] // newest first per repo.Recent contract
		detail := ""
		if last.ErrorRedacted != "" {
			detail = fmt.Sprintf(" (last: %s)", last.ErrorRedacted)
		} else if last.Reason != "" {
			detail = fmt.Sprintf(" (last: %s)", last.Reason)
		}

		unavail := ""
		if strings.HasPrefix(name, "mcp_") {
			if _, ok := availableSet[name]; !ok {
				unavail = " (currently unavailable)"
			}
		}

		line := fmt.Sprintf("- %s: %dx %s%s%s", name, len(failures), last.Class, detail, unavail)
		if len(line) > brieferMaxPerTool {
			line = line[:brieferMaxPerTool]
		}

		entries = append(entries, briefEntry{line: line, lastSeen: last.EndedAt})
	}

	if len(entries) == 0 {
		return ""
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastSeen.After(entries[j].lastSeen)
	})
	if len(entries) > brieferMaxTools {
		entries = entries[:brieferMaxTools]
	}

	var sb strings.Builder
	sb.WriteString("Recent tool experience for this thread:\n")
	for _, e := range entries {
		sb.WriteString(e.line)
		sb.WriteByte('\n')
	}
	result := strings.TrimRight(sb.String(), "\n")
	if len(result) > brieferMaxTotal {
		result = result[:brieferMaxTotal]
	}
	return result
}
