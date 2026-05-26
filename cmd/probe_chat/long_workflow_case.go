package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func naturalLongWorkflowScratchpadCase(stamp string) Case {
	safeStamp := strings.NewReplacer("-", "_", ":", "_").Replace(stamp)
	marker := "LONGCTX_" + safeStamp
	threadID := "probe-long-workflow-" + stamp
	dir := filepath.Join("runtime-workspace", "notes", "long-context-"+stamp)
	filePath := filepath.Join(dir, "brief.md")
	toolPath := "notes/long-context-" + stamp + "/brief.md"
	var startedAt time.Time

	return Case{
		Name:     "natural-long-workflow-scratchpad",
		Category: "tools-agent-note",
		ThreadID: threadID,
		Setup: func(_ *Env) error {
			startedAt = time.Now().UTC().Add(-2 * time.Second)
			_ = os.Remove(filePath)
			_ = os.Remove(dir)
			return nil
		},
		Prompt: fmt.Sprintf(
			"Sto preparando un micro-briefing operativo per domani. Recupera quello che sai di me, controlla le notizie locali recenti di Caraglio (CN), e tieniti una checklist interna con il marker %s. Non creare ancora file: rispondimi solo con un breve stato di avanzamento in tre punti.",
			marker,
		),
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			userID, err := env.fetchUserID()
			if err != nil {
				return []string{fmt.Sprintf("whoami: %v", err)}
			}
			convID := "web:" + userID + ":" + threadID

			if r.ToolCalls < 3 {
				miss = append(miss, fmt.Sprintf("turn1: expected multiple tools, got %d", r.ToolCalls))
			}
			counts, err := env.toolAttemptsSince(startedAt, longWorkflowTools...)
			if err != nil {
				return append(miss, fmt.Sprintf("tool_attempts query after turn1: %v", err))
			}
			if counts["agent_note"] == 0 {
				miss = append(miss, "turn1: no agent_note attempt for internal checklist")
			}
			if counts["search"] == 0 {
				miss = append(miss, "turn1: no search attempt for user/work profile")
			}
			if counts["web"] == 0 {
				miss = append(miss, "turn1: no web attempt for recent Caraglio news")
			}
			for _, forbidden := range []string{"tool_search", "execute_code", "execute_shell"} {
				if counts[forbidden] > 0 {
					miss = append(miss, fmt.Sprintf("forbidden tool %s ran %d time(s)", forbidden, counts[forbidden]))
				}
			}
			note, exists, err := env.agentNoteContent(convID)
			if err != nil {
				miss = append(miss, fmt.Sprintf("turn1: agent_notes query: %v", err))
			} else if !exists || !strings.Contains(note, marker) {
				miss = append(miss, fmt.Sprintf("turn1: scratchpad missing marker %s (exists=%v content=%q)", marker, exists, truncate(note, 240)))
			}

			r2, err := env.sendChatThread(threadID, fmt.Sprintf(
				"Ora continua da dove eri rimasta: crea un file Markdown nel workspace in %s con una sezione profilo, una sezione notizie Caraglio e una sezione prossime azioni. Deve contenere il marker %s. Poi dimmi solo il percorso salvato e due rischi.",
				toolPath,
				marker,
			))
			if err != nil {
				return append(miss, fmt.Sprintf("turn2 sendChat: %v", err))
			}
			if r2.ToolCalls == 0 {
				miss = append(miss, fmt.Sprintf("turn2: expected file/scratchpad tool calls, got 0 (reply=%q)", truncate(r2.Reply, 240)))
			}
			counts, err = env.toolAttemptsSince(startedAt, longWorkflowTools...)
			if err != nil {
				return append(miss, fmt.Sprintf("tool_attempts query after turn2: %v", err))
			}
			if counts["file"] == 0 {
				miss = append(miss, "turn2: no file attempt for Markdown artifact")
			}
			body, err := os.ReadFile(filePath)
			if err != nil {
				miss = append(miss, fmt.Sprintf("turn2: markdown file missing on disk: %v", err))
			} else {
				text := string(body)
				for _, want := range []string{marker, "Caraglio", "Profilo"} {
					if !strings.Contains(text, want) {
						miss = append(miss, fmt.Sprintf("turn2: markdown file missing %q (preview=%q)", want, truncate(text, 300)))
					}
				}
			}

			r3, err := env.sendChatThread(threadID, fmt.Sprintf(
				"Per chiudere, rileggi la tua checklist interna, confrontala con il file salvato e poi svuota la checklist. Rispondimi con CHECKLIST_CHIUSA e il marker %s.",
				marker,
			))
			if err != nil {
				return append(miss, fmt.Sprintf("turn3 sendChat: %v", err))
			}
			if !strings.Contains(r3.Reply, "CHECKLIST_CHIUSA") || !strings.Contains(r3.Reply, marker) {
				miss = append(miss, fmt.Sprintf("turn3: reply missing close marker (reply=%q)", truncate(r3.Reply, 240)))
			}
			content, exists, err := env.agentNoteContent(convID)
			if err != nil {
				miss = append(miss, fmt.Sprintf("turn3: agent_notes post-clear query: %v", err))
			} else if exists && strings.TrimSpace(content) != "" {
				miss = append(miss, fmt.Sprintf("turn3: scratchpad not cleared (content=%q)", truncate(content, 240)))
			}
			counts, err = env.toolAttemptsSince(startedAt, longWorkflowTools...)
			if err != nil {
				return append(miss, fmt.Sprintf("tool_attempts query after turn3: %v", err))
			}
			if counts["agent_note"] < 2 {
				miss = append(miss, fmt.Sprintf("expected repeated scratchpad use across turns, got %d agent_note ok attempts", counts["agent_note"]))
			}
			if counts["search"] == 0 || counts["web"] == 0 || counts["file"] == 0 {
				miss = append(miss, fmt.Sprintf("missing multi-tool coverage: search=%d web=%d file=%d", counts["search"], counts["web"], counts["file"]))
			}
			for _, forbidden := range []string{"tool_search", "execute_code", "execute_shell"} {
				if counts[forbidden] > 0 {
					miss = append(miss, fmt.Sprintf("forbidden tool %s ran %d time(s)", forbidden, counts[forbidden]))
				}
			}
			miss = append(miss, verifyLongWorkflowToolHealth(env, startedAt)...)
			stats, err := env.contextStatsSince(startedAt, threadID)
			if err != nil {
				miss = append(miss, fmt.Sprintf("context stats query: %v", err))
			} else {
				if stats.ConversationRows < 6 {
					miss = append(miss, fmt.Sprintf("expected archived long conversation rows, got %d", stats.ConversationRows))
				}
				if stats.MaxTokensIn > 80000 {
					miss = append(miss, fmt.Sprintf("conversation context bloat too high: max_tokens_in=%d rows=%d tool_rows=%d compactions=%d", stats.MaxTokensIn, stats.ConversationRows, stats.ToolRows, stats.Compactions))
				}
				if stats.ToolRows > 9 {
					miss = append(miss, fmt.Sprintf("too many archived tool rows for bounded workflow: got %d, want <= 9", stats.ToolRows))
				}
			}
			return miss
		},
		Cleanup: func(env *Env) {
			_ = os.Remove(filePath)
			_ = os.Remove(dir)
			if userID, err := env.fetchUserID(); err == nil && userID != "" {
				_ = env.deleteAgentNote("web:" + userID + ":" + threadID)
			}
		},
	}
}

var longWorkflowTools = []string{
	"agent_note",
	"search",
	"web",
	"file",
	"tool_search",
	"execute_code",
	"execute_shell",
}

func verifyLongWorkflowToolHealth(env *Env, startedAt time.Time) []string {
	rows, err := env.toolAttemptRowsSince(
		startedAt,
		longWorkflowTools,
		[]string{"recoverable", "blocked", "error"},
	)
	if err != nil {
		return []string{fmt.Sprintf("tool health query: %v", err)}
	}
	var miss []string
	for _, row := range rows {
		miss = append(miss, fmt.Sprintf(
			"unexpected %s tool attempt: tool=%s class=%s reason=%s error=%s arg_keys=%s",
			row.Outcome,
			row.ToolName,
			row.Class,
			truncate(row.Reason, 120),
			truncate(row.ErrorRedacted, 120),
			truncate(row.ArgKeysJSON, 160),
		))
	}
	return miss
}
