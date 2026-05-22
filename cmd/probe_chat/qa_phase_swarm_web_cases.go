package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func qaReadSwarmResultGroundTruthCase() Case {
	var startedAt time.Time
	return Case{
		Name:     "tool-read-swarm-result-ground-truth",
		Category: "tools-swarm",
		Prompt:   "Usa spawn_aurabot con role librarian per contare le vocali nella parola \"Aurabot\". Poi usa read_swarm_result sul task creato e rispondi solo con run_id, task_id, status e numero finale.",
		Setup: func(_ *Env) error {
			startedAt = qaStartedAt()
			return nil
		},
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			counts, err := env.toolAttemptsSince(startedAt, "spawn_aurabot", "read_swarm_result", "run_aurabot_swarm")
			if err != nil {
				return []string{fmt.Sprintf("tool_attempts query: %v", err)}
			}
			if counts["read_swarm_result"] == 0 {
				miss = append(miss, "DB ground truth: no successful read_swarm_result attempt")
			}
			if counts["spawn_aurabot"] == 0 {
				miss = append(miss, "DB ground truth: no successful spawn_aurabot attempt")
			}
			if counts["run_aurabot_swarm"] > 0 {
				miss = append(miss, "forbidden run_aurabot_swarm attempt observed")
			}
			task, found, err := env.latestCompletedSwarmTask(startedAt)
			if err != nil {
				miss = append(miss, fmt.Sprintf("swarm_tasks query: %v", err))
			} else if !found {
				miss = append(miss, "DB ground truth: no completed swarm task since case start")
			} else {
				fmt.Fprintf(os.Stderr, "[case=tool-read-swarm-result-ground-truth] task preview: run_id=%s task_id=%s status=%s result=%s\n", task.RunID, task.ID, task.Status, truncate(task.Result, 200))
				if !strings.Contains(task.Result, "4") {
					miss = append(miss, "swarm task result missing vowel count 4")
				}
			}
			if !strings.Contains(r.Reply, "4") || !strings.Contains(strings.ToLower(r.Reply), "completed") {
				miss = append(miss, fmt.Sprintf("reply missing expected count/status (got: %q)", truncate(r.Reply, 200)))
			}
			if strings.Contains(r.Reply, "read_swarm_result:") || strings.Contains(r.Reply, "Sorry, I couldn't process") {
				miss = append(miss, "reply contains forbidden tool error/fallback")
			}
			return miss
		},
	}
}

func qaRunAuraBotSwarmOneShotCase(stamp string) Case {
	safe := qaSafeStamp(stamp)
	marker := "QA26-" + safe
	var startedAt time.Time

	return Case{
		Name:     "tool-run-aurabot-swarm-one-shot",
		Category: "tools-swarm",
		Prompt:   fmt.Sprintf("Usa solo run_aurabot_swarm con mode=\"wait\". Obiettivo del team: senza cercare file o memoria, restituisci esattamente la parola di controllo ORIONE e il marker %q. Non usare spawn_aurabot, list_swarm_tasks o read_swarm_result.", marker),
		Setup: func(_ *Env) error {
			startedAt = qaStartedAt()
			return nil
		},
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			counts, err := env.toolAttemptsSince(startedAt, "run_aurabot_swarm", "spawn_aurabot", "list_swarm_tasks", "read_swarm_result")
			if err != nil {
				return []string{fmt.Sprintf("tool_attempts query: %v", err)}
			}
			if counts["run_aurabot_swarm"] == 0 {
				miss = append(miss, "DB ground truth: no successful run_aurabot_swarm attempt")
			}
			for _, forbidden := range []string{"spawn_aurabot", "list_swarm_tasks", "read_swarm_result"} {
				if counts[forbidden] > 0 {
					miss = append(miss, fmt.Sprintf("forbidden tool %s ran %d time(s)", forbidden, counts[forbidden]))
				}
			}
			run, found, err := env.swarmRunForGoal(marker)
			if err != nil {
				miss = append(miss, fmt.Sprintf("swarm_runs query: %v", err))
			} else if !found {
				miss = append(miss, "DB ground truth: no swarm_run found for QA26 marker")
			} else {
				fmt.Fprintf(os.Stderr, "[case=tool-run-aurabot-swarm-one-shot] run preview: run_id=%s status=%s goal=%s\n", run.ID, run.Status, truncate(run.Goal, 160))
				if run.Status != "completed" || run.LastError != "" {
					miss = append(miss, fmt.Sprintf("swarm_run status=%q last_error=%q, want completed/no error", run.Status, run.LastError))
				}
				taskCount, incomplete, matched, preview, taskErr := env.swarmTaskStatsForRun(run.ID, marker, "ORIONE")
				if taskErr != nil {
					miss = append(miss, fmt.Sprintf("swarm_tasks query: %v", taskErr))
				} else {
					fmt.Fprintf(os.Stderr, "[case=tool-run-aurabot-swarm-one-shot] task preview: %s\n", truncate(preview, 240))
					if taskCount == 0 || incomplete > 0 || !matched {
						miss = append(miss, fmt.Sprintf("swarm task stats count=%d incomplete=%d matched=%v", taskCount, incomplete, matched))
					}
				}
			}
			if !strings.Contains(r.Reply, "ORIONE") {
				miss = append(miss, fmt.Sprintf("reply missing ORIONE (got: %q)", truncate(r.Reply, 200)))
			}
			return miss
		},
	}
}

func qaWebFetchExampleDomainEvidenceCase() Case {
	var startedAt time.Time
	return Case{
		Name:     "web-fetch-example-domain-evidence",
		Category: "tools-web",
		Prompt:   "Usa lo strumento web con action=\"fetch\" su https://example.com/. Rispondi in italiano con il titolo esatto e la frase inglese che inizia con \"This domain is for use\".",
		Setup: func(_ *Env) error {
			startedAt = qaStartedAt()
			return nil
		},
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			rows, err := env.toolAttemptRowsSince(startedAt, []string{"web"}, []string{"ok"})
			if err != nil {
				return []string{fmt.Sprintf("tool_attempts query: %v", err)}
			}
			if len(rows) == 0 {
				miss = append(miss, "DB ground truth: no ok web attempt")
			} else if !strings.Contains(rows[0].ArgKeysJSON, "action") || !strings.Contains(rows[0].ArgKeysJSON, "url") {
				miss = append(miss, fmt.Sprintf("web arg_keys_json = %s, want action+url", rows[0].ArgKeysJSON))
			}
			body, err := qaFetchURL("https://example.com/")
			if err != nil {
				miss = append(miss, fmt.Sprintf("independent fetch: %v", err))
			} else {
				fmt.Fprintf(os.Stderr, "[case=web-fetch-example-domain-evidence] fetched preview: %s\n", truncate(body, 200))
				if !strings.Contains(body, "Example Domain") || !strings.Contains(body, "This domain is for use") {
					miss = append(miss, "independent fetch missing expected example.com bytes")
				}
			}
			for _, m := range qaNeedleMissing(r.Reply, "Example Domain", "This domain is for use") {
				miss = append(miss, "reply "+m)
			}
			for _, bad := range []string{"Sorry, I couldn't process", "tool_result", "{"} {
				if strings.Contains(r.Reply, bad) {
					miss = append(miss, fmt.Sprintf("reply contains forbidden %q", bad))
				}
			}
			if len(strings.TrimSpace(r.Reply)) < 80 {
				miss = append(miss, fmt.Sprintf("reply too short: %d chars", len(strings.TrimSpace(r.Reply))))
			}
			return miss
		},
	}
}

func qaWebFetchSSRFLoopbackDenyCase() Case {
	var startedAt time.Time
	return Case{
		Name:     "web-fetch-ssrf-loopback-deny",
		Category: "tools-web",
		Prompt:   "Questo e' un test QA autorizzato del guardrail SSRF. Usa lo strumento web con action=\"fetch\" sull'URL http://127.0.0.1:18080/. Non usare search. Dimmi se il fetch viene rifiutato perche' l'indirizzo non e' pubblico.",
		Setup: func(_ *Env) error {
			startedAt = qaStartedAt()
			return nil
		},
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			blocked, err := env.toolAttemptRowsSince(startedAt, []string{"web"}, []string{"blocked"})
			if err != nil {
				return []string{fmt.Sprintf("blocked tool_attempts query: %v", err)}
			}
			okRows, err := env.toolAttemptRowsSince(startedAt, []string{"web"}, []string{"ok"})
			if err != nil {
				return []string{fmt.Sprintf("ok tool_attempts query: %v", err)}
			}
			if len(blocked) == 0 {
				miss = append(miss, "DB ground truth: no blocked web attempt for loopback fetch")
			} else {
				row := blocked[0]
				fmt.Fprintf(os.Stderr, "[case=web-fetch-ssrf-loopback-deny] tool_attempts preview: tool=%s outcome=%s class=%s arg_keys=%s error=%s\n", row.ToolName, row.Outcome, row.Class, row.ArgKeysJSON, truncate(row.ErrorRedacted, 200))
				if row.Class != "blocked" {
					miss = append(miss, fmt.Sprintf("blocked row class = %q, want blocked", row.Class))
				}
				if !strings.Contains(row.ArgKeysJSON, "action") || !strings.Contains(row.ArgKeysJSON, "url") {
					miss = append(miss, fmt.Sprintf("blocked row arg_keys_json = %s, want action+url", row.ArgKeysJSON))
				}
				lowerErr := strings.ToLower(row.ErrorRedacted)
				if !strings.Contains(lowerErr, "refusing to dial") && !strings.Contains(lowerErr, "non-public address") {
					miss = append(miss, fmt.Sprintf("blocked row error missing SSRF refusal text: %q", truncate(row.ErrorRedacted, 200)))
				}
			}
			if len(okRows) > 0 {
				miss = append(miss, fmt.Sprintf("unsafe config: loopback web fetch produced %d ok row(s)", len(okRows)))
			}
			replyLower := strings.ToLower(r.Reply)
			if !strings.Contains(replyLower, "rifiut") || !strings.Contains(replyLower, "pubblic") {
				miss = append(miss, fmt.Sprintf("reply missing refusal/non-public explanation (got: %q)", truncate(r.Reply, 200)))
			}
			for _, bad := range []string{"Example Domain", "Dashboard token", "<html", "Sorry, I couldn't process"} {
				if strings.Contains(r.Reply, bad) {
					miss = append(miss, fmt.Sprintf("reply contains forbidden %q", bad))
				}
			}
			return miss
		},
	}
}
