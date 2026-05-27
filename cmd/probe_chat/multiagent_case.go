package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var multiagentProbeTools = []string{
	"agent_note",
	"delegate_summarizer",
	"list_swarm_tasks",
	"read_swarm_result",
	"search",
	"web",
	"file",
	"source",
	"wiki_page",
	"mcp_calculator_calculate",
	"tool_search",
}

func naturalMultiagentOrchestratorCase(stamp string) Case {
	marker := "MA-" + stamp
	threadID := "probe-multiagent-orchestrator-" + stamp
	var startedAt time.Time

	return Case{
		Name:     "natural-multiagent-orchestrator-scratchpad",
		Category: "tools-swarm",
		ThreadID: threadID,
		Setup: func(_ *Env) error {
			startedAt = time.Now().UTC().Add(-2 * time.Second)
			return nil
		},
		PromptFn: func() string {
			return multiagentOrchestratorPrompt(marker)
		},
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			replyLower := strings.ToLower(r.Reply)
			if !strings.Contains(r.Reply, "MULTIAGENT_OK") || !strings.Contains(r.Reply, marker) {
				miss = append(miss, fmt.Sprintf("reply missing MULTIAGENT_OK marker %s (got: %q)", marker, truncate(r.Reply, 240)))
			}
			for _, required := range []string{
				"oh3-run-" + marker,
				"/workspace/sources/oh3/" + marker + "/trace.json",
				"CTX_BLOAT_4187",
				"https://example.local/oh3/" + marker + "/artifact",
				"compare-critical-path",
			} {
				if !strings.Contains(r.Reply, required) {
					miss = append(miss, fmt.Sprintf("reply did not preserve required fact %q", required))
				}
			}
			if strings.Contains(replyLower, "delegate_summarizer") || strings.Contains(replyLower, "agent_note") {
				miss = append(miss, "reply leaked internal tool/delegate names")
			}

			counts, err := env.toolAttemptsSince(startedAt, multiagentProbeTools...)
			if err != nil {
				miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
				return miss
			}
			if counts["delegate_summarizer"] == 0 {
				miss = append(miss, "DB ground truth: delegate_summarizer did not run")
			}
			if counts["agent_note"] == 0 {
				miss = append(miss, "DB ground truth: agent_note did not run for requested private checklist")
			}
			for _, forbidden := range []string{"search", "web", "file", "source", "wiki_page", "mcp_calculator_calculate"} {
				if counts[forbidden] > 0 {
					miss = append(miss, fmt.Sprintf("unexpected tool %s ran %d time(s)", forbidden, counts[forbidden]))
				}
			}
			if r.ToolCalls < 2 {
				miss = append(miss, fmt.Sprintf("expected parent to use scratchpad + delegation, got tool_calls=%d", r.ToolCalls))
			}
			if r.ToolCalls > 8 {
				miss = append(miss, fmt.Sprintf("parent tool loop too large: tool_calls=%d", r.ToolCalls))
			}

			rows, err := env.swarmTaskRowsSince(startedAt, marker)
			if err != nil {
				miss = append(miss, fmt.Sprintf("swarm DB query: %v", err))
				return miss
			}
			if len(rows) == 0 {
				miss = append(miss, "DB ground truth: no swarm task/run row matched the marker")
			}
			completedSummarizer := false
			for _, row := range rows {
				if row.RunStatus == "failed" || row.TaskStatus == "failed" {
					miss = append(miss, fmt.Sprintf("swarm failed: run=%s task=%s run_error=%q task_error=%q", row.RunID, row.TaskID, row.RunLastError, row.TaskLastError))
				}
				if row.Role == "summarizer" && row.TaskStatus == "completed" {
					completedSummarizer = true
					if row.LLMCalls == 0 {
						miss = append(miss, fmt.Sprintf("summarizer task %s completed with llm_calls=0", row.TaskID))
					}
					if row.ToolCalls != 0 {
						miss = append(miss, fmt.Sprintf("summarizer task %s should be tool-free, got tool_calls=%d", row.TaskID, row.ToolCalls))
					}
					for _, required := range []string{
						"oh3-run-" + marker,
						"/workspace/sources/oh3/" + marker + "/trace.json",
						"CTX_BLOAT_4187",
					} {
						if !strings.Contains(row.Result, required) {
							miss = append(miss, fmt.Sprintf("summarizer result did not preserve %q", required))
						}
					}
				}
			}
			if !completedSummarizer {
				miss = append(miss, "DB ground truth: no completed summarizer child task")
			}

			userID, err := env.fetchUserID()
			if err != nil {
				miss = append(miss, fmt.Sprintf("whoami: %v", err))
			} else {
				convID := "web:" + userID + ":" + threadID
				if note, exists, err := env.agentNoteContent(convID); err != nil {
					miss = append(miss, fmt.Sprintf("agent_note ground truth: %v", err))
				} else if !exists || !strings.Contains(note, marker) {
					miss = append(miss, fmt.Sprintf("agent_note missing marker %s", marker))
				}
			}
			if stats, err := env.contextStatsSince(startedAt, threadID); err != nil {
				miss = append(miss, fmt.Sprintf("context stats query: %v", err))
			} else {
				if stats.MaxTokensIn > 85000 {
					miss = append(miss, fmt.Sprintf("context bloat too high: max_tokens_in=%d rows=%d tool_rows=%d compactions=%d", stats.MaxTokensIn, stats.ConversationRows, stats.ToolRows, stats.Compactions))
				}
				if stats.ToolRows > 10 {
					miss = append(miss, fmt.Sprintf("too many tool rows in parent conversation: %d", stats.ToolRows))
				}
			}
			return miss
		},
		Metrics: func(r ChatReply, env *Env) map[string]any {
			return env.multiagentMetrics(startedAt, threadID, marker, r)
		},
		Cleanup: func(env *Env) {
			if userID, err := env.fetchUserID(); err == nil && userID != "" {
				_ = env.deleteAgentNote("web:" + userID + ":" + threadID)
			}
		},
	}
}

func naturalMultiagentNoOverdelegationCase(stamp string) Case {
	marker := "MA-SIMPLE-" + stamp
	threadID := "probe-multiagent-simple-" + stamp
	var startedAt time.Time

	return Case{
		Name:     "natural-multiagent-no-overdelegation",
		Category: "tools-swarm",
		ThreadID: threadID,
		Setup: func(_ *Env) error {
			startedAt = time.Now().UTC().Add(-2 * time.Second)
			return nil
		},
		Prompt: "Riassumi in una frase questa nota semplice senza fare ricerche e mantenendo il marker: " + marker + " significa che il caso banale deve restare banale.",
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			if strings.TrimSpace(r.Reply) == "" || !strings.Contains(r.Reply, marker) {
				miss = append(miss, fmt.Sprintf("reply missing simple marker %s", marker))
			}
			counts, err := env.toolAttemptsSince(startedAt, multiagentProbeTools...)
			if err != nil {
				miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
				return miss
			}
			if counts["delegate_summarizer"] > 0 {
				miss = append(miss, fmt.Sprintf("overdelegated simple request: delegate_summarizer ran %d time(s)", counts["delegate_summarizer"]))
			}
			if counts["agent_note"] > 0 {
				miss = append(miss, fmt.Sprintf("scratchpad used for simple one-shot request: agent_note ran %d time(s)", counts["agent_note"]))
			}
			rows, err := env.swarmTaskRowsSince(startedAt, marker)
			if err != nil {
				miss = append(miss, fmt.Sprintf("swarm DB query: %v", err))
				return miss
			}
			if len(rows) != 0 {
				miss = append(miss, fmt.Sprintf("simple request created %d swarm row(s)", len(rows)))
			}
			return miss
		},
		Metrics: func(r ChatReply, env *Env) map[string]any {
			return env.multiagentMetrics(startedAt, threadID, marker, r)
		},
	}
}

func multiagentOrchestratorPrompt(marker string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Sto facendo QA multiagente su %s. Organizza il lavoro come orchestratore: tieni una mini checklist privata con obiettivo, fatti da preservare e controlli finali; tratta la compressione del blocco rumoroso come sottolavoro separato prima del riepilogo finale, senza comprimerlo direttamente nel messaggio principale. Non cercare nel web, nei file o in memoria: lavora solo sul testo che segue. Conserva identici RUN_ID, PATH_A, URL_REPORT, ERROR_CODE e NEXT_ACTION. Alla fine rispondi con MULTIAGENT_OK %s, una sintesi naturale e una sezione \"Riferimenti preservati\" con quei cinque valori esatti, senza menzionare infrastruttura interna.\n\n", marker, marker)
	fmt.Fprintf(&b, "BLOCCO_RUMOROSO_START %s\n", marker)
	fmt.Fprintf(&b, "RUN_ID=oh3-run-%s\n", marker)
	fmt.Fprintf(&b, "PATH_A=/workspace/sources/oh3/%s/trace.json\n", marker)
	fmt.Fprintf(&b, "URL_REPORT=https://example.local/oh3/%s/artifact\n", marker)
	fmt.Fprintf(&b, "ERROR_CODE=CTX_BLOAT_4187\n")
	fmt.Fprintf(&b, "NEXT_ACTION=compare-critical-path\n")
	for i := 0; i < 18; i++ {
		fmt.Fprintf(&b, "noise_line_%02d: wrapper=%s duplicate navigation chrome lorem ipsum; keep only if it helps explain the failure. RUN_ID=oh3-run-%s PATH_A=/workspace/sources/oh3/%s/trace.json.\n", i, marker, marker, marker)
		if i%6 == 0 {
			fmt.Fprintf(&b, "checkpoint_%02d: useful fact: child output must stay compact, parent should preserve user tone, and metric critical_path_estimate must be derived from parent and child llm calls.\n", i)
		}
	}
	fmt.Fprintf(&b, "BLOCCO_RUMOROSO_END %s\n", marker)
	return b.String()
}

type swarmProbeTaskRow struct {
	RunID            string `json:"run_id"`
	RunStatus        string `json:"run_status"`
	RunGoal          string `json:"run_goal"`
	RunLastError     string `json:"run_last_error"`
	TaskID           string `json:"task_id"`
	TaskStatus       string `json:"task_status"`
	Role             string `json:"role"`
	Subject          string `json:"subject"`
	Result           string `json:"result"`
	TaskLastError    string `json:"task_last_error"`
	LLMCalls         int    `json:"llm_calls"`
	ToolCalls        int    `json:"tool_calls"`
	TokensPrompt     int    `json:"tokens_prompt"`
	TokensCompletion int    `json:"tokens_completion"`
	TokensTotal      int    `json:"tokens_total"`
	ElapsedMS        int64  `json:"elapsed_ms"`
}

func (e *Env) swarmTaskRowsSince(since time.Time, marker string) ([]swarmProbeTaskRow, error) {
	sinceText := since.UTC().Format(time.RFC3339Nano)
	like := "%" + marker + "%"
	query := `SELECT
  r.id AS run_id,
  r.status AS run_status,
  r.goal AS run_goal,
  r.last_error AS run_last_error,
  t.id AS task_id,
  t.status AS task_status,
  t.role AS role,
  t.subject AS subject,
  t.result AS result,
  t.last_error AS task_last_error,
  t.llm_calls AS llm_calls,
  t.tool_calls AS tool_calls,
  t.tokens_prompt AS tokens_prompt,
  t.tokens_completion AS tokens_completion,
  t.tokens_total AS tokens_total,
  t.elapsed_ms AS elapsed_ms
FROM swarm_runs r
JOIN swarm_tasks t ON t.run_id = r.id
WHERE r.created_at >= ` + sqliteQuote(sinceText) + `
  AND (
    r.goal LIKE ` + sqliteQuote(like) + `
    OR t.subject LIKE ` + sqliteQuote(like) + `
    OR t.prompt LIKE ` + sqliteQuote(like) + `
    OR t.result LIKE ` + sqliteQuote(like) + `
    OR r.last_error LIKE ` + sqliteQuote(like) + `
    OR t.last_error LIKE ` + sqliteQuote(like) + `
  )
ORDER BY r.created_at DESC, t.created_at DESC
LIMIT 12;`
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		var rows []swarmProbeTaskRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("decode docker swarm rows: %w", err)
		}
		return rows, nil
	}
	rows, err := e.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	var out []swarmProbeTaskRow
	for rows.Next() {
		var row swarmProbeTaskRow
		if err := rows.Scan(
			&row.RunID,
			&row.RunStatus,
			&row.RunGoal,
			&row.RunLastError,
			&row.TaskID,
			&row.TaskStatus,
			&row.Role,
			&row.Subject,
			&row.Result,
			&row.TaskLastError,
			&row.LLMCalls,
			&row.ToolCalls,
			&row.TokensPrompt,
			&row.TokensCompletion,
			&row.TokensTotal,
			&row.ElapsedMS,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return out, nil
}

func (e *Env) multiagentMetrics(startedAt time.Time, threadID, marker string, r ChatReply) map[string]any {
	metrics := map[string]any{
		"parent_llm_calls":  r.LLMCalls,
		"parent_tool_calls": r.ToolCalls,
		"parent_tokens":     r.Tokens,
		"parent_elapsed_ms": r.ElapsedMs,
	}
	if counts, err := e.toolAttemptsSince(startedAt, multiagentProbeTools...); err == nil {
		for _, name := range multiagentProbeTools {
			if counts[name] > 0 {
				metrics["tool_attempts."+name] = counts[name]
			}
		}
	} else {
		metrics["tool_attempts.error"] = err.Error()
	}
	rows, err := e.swarmTaskRowsSince(startedAt, marker)
	if err != nil {
		metrics["swarm.error"] = err.Error()
	} else {
		metrics["swarm_rows"] = len(rows)
		summarizerRows := 0
		reflectorRows := 0
		childLLM := 0
		childTools := 0
		childTokens := 0
		childElapsed := int64(0)
		completed := 0
		failed := 0
		for _, row := range rows {
			if row.Role == "reflector" {
				reflectorRows++
			}
			if row.Role != "summarizer" {
				continue
			}
			summarizerRows++
			childLLM += row.LLMCalls
			childTools += row.ToolCalls
			childTokens += row.TokensTotal
			if row.ElapsedMS > childElapsed {
				childElapsed = row.ElapsedMS
			}
			if row.TaskStatus == "completed" {
				completed++
			}
			if row.TaskStatus == "failed" || row.RunStatus == "failed" {
				failed++
			}
		}
		metrics["swarm_summarizer_rows"] = summarizerRows
		metrics["swarm_reflector_rows"] = reflectorRows
		metrics["child_llm_calls"] = childLLM
		metrics["child_tool_calls"] = childTools
		metrics["child_tokens_total"] = childTokens
		metrics["child_max_elapsed_ms"] = childElapsed
		metrics["child_completed_tasks"] = completed
		metrics["child_failed_tasks"] = failed
		metrics["critical_steps_estimate"] = r.LLMCalls + childLLM
	}
	if stats, err := e.contextStatsSince(startedAt, threadID); err == nil {
		metrics["context_rows"] = stats.ConversationRows
		metrics["context_tool_rows"] = stats.ToolRows
		metrics["context_max_tokens_in"] = stats.MaxTokensIn
		metrics["context_compactions"] = stats.Compactions
	} else {
		metrics["context.error"] = err.Error()
	}
	return metrics
}
