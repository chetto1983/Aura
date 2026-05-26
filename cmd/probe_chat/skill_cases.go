package main

import (
	"fmt"
	"strings"
	"time"
)

func naturalSkillAuthoringCase(stamp string) Case {
	name := "aura-natural-procedure-" + strings.ToLower(stamp)
	threadID := "probe-natural-skill-" + stamp
	var startedAt time.Time

	return Case{
		Name:     "natural-skill-authoring",
		Category: "tools-skills",
		ThreadID: threadID,
		Prompt: fmt.Sprintf(
			"Voglio insegnarti una procedura riutilizzabile chiamata %s: quando la richiamo, aiutami a controllare una nuova procedura prima di salvarla. Deve fare tre cose: controllare che il nome sia chiaro, verificare che la descrizione sia breve, e proporre una prova minima. Preparala adesso e poi dimmi solo che e' pronta.",
			name,
		),
		Setup: func(env *Env) error {
			startedAt = time.Now()
			_ = env.deleteSkillDir(name)
			return nil
		},
		Verify: func(r ChatReply, env *Env) []string {
			var miss []string
			desc, content, missing, err := env.fetchSkill(name)
			if err != nil {
				miss = append(miss, fmt.Sprintf("skill API ground truth: %v", err))
			} else if missing {
				miss = append(miss, fmt.Sprintf("skill API ground truth: %q missing; reusable procedure was not saved as a skill", name))
			} else {
				combined := strings.ToLower(desc + "\n" + content)
				for _, group := range [][]string{
					{"name", "nome", "naming"},
					{"description", "descrizione", "descr"},
					{"test", "proof", "prova", "minimal"},
				} {
					if !containsAny(combined, group) {
						miss = append(miss, fmt.Sprintf("skill %q missing expected concept from %v", name, group))
					}
				}
			}

			counts, err := env.toolAttemptsSince(startedAt, "file", "wiki_page", "skill", "tool_search")
			if err != nil {
				miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
				return miss
			}
			if counts["file"] == 0 {
				miss = append(miss, "expected local skill authoring through file operations, got no successful file attempts")
			}
			if counts["wiki_page"] > 0 {
				miss = append(miss, fmt.Sprintf("wrong owner path: wiki_page ran %d time(s) for reusable procedure authoring", counts["wiki_page"]))
			}
			if r.ToolCalls > 5 {
				miss = append(miss, fmt.Sprintf("too many tool calls for one small skill: got %d, want <= 5", r.ToolCalls))
			}
			if r.Tokens > 40000 {
				miss = append(miss, fmt.Sprintf("context bloat: got %d tokens, want <= 40000", r.Tokens))
			}
			return miss
		},
		Cleanup: func(env *Env) {
			_ = env.deleteSkillDir(name)
		},
	}
}
