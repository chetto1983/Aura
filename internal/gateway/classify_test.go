package gateway

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/scoring"
)

// skillSpec/taskSpec/swarmSpec return the REAL tool descriptors so the tests drive
// off the same schema the model sees (exhaustiveness derives its action list from
// spec.Parameters, not a hand-copied enum that could drift).
func skillSpec() tools.Spec { return (&tools.SkillTool{}).Spec() }
func taskSpec() tools.Spec  { return (&tools.TaskTool{}).Spec() }
func swarmSpec() tools.Spec { return (&tools.SwarmSpawn{}).Spec() }

func mustArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}

// actionEnum extracts the property-level `action` enum from a tool's JSON schema,
// so a future action added to the schema is visible to the exhaustiveness test.
func actionEnum(t *testing.T, spec tools.Spec) map[string]bool {
	t.Helper()
	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("parse %s schema: %v", spec.Name, err)
	}
	set := make(map[string]bool, len(schema.Properties.Action.Enum))
	for _, a := range schema.Properties.Action.Enum {
		set[a] = true
	}
	if len(set) == 0 {
		t.Fatalf("%s: no action enum in schema", spec.Name)
	}
	return set
}

// TestClassifyTable is the behavior table (D-02b): every live skill/task action, the
// two multiplexed tools' fixed outcomes, the parse-fail/unknown saturate-upward
// default, and the non-multiplexed Mutating-bit fallback.
func TestClassifyTable(t *testing.T) {
	skill := skillSpec()
	task := taskSpec()
	swarm := swarmSpec()

	cases := []struct {
		name string
		spec tools.Spec
		args json.RawMessage
		want scoring.RiskTier
	}{
		// skill reads → Safe (allow-listed, never scored)
		{"skill/list", skill, mustArgs(t, map[string]string{"action": "list"}), scoring.Safe},
		{"skill/info", skill, mustArgs(t, map[string]string{"action": "info", "name": "x"}), scoring.Safe},
		{"skill/use", skill, mustArgs(t, map[string]string{"action": "use", "name": "x"}), scoring.Safe},
		// skill snippet-lifecycle → Normal
		{"skill/restore", skill, mustArgs(t, map[string]string{"action": "restore", "name": "x"}), scoring.Normal},
		{"skill/archive", skill, mustArgs(t, map[string]string{"action": "archive", "name": "x"}), scoring.Normal},
		{"skill/save_snippet", skill, mustArgs(t, map[string]string{"action": "save_snippet", "name": "x"}), scoring.Normal},
		// skill authoring → scored via ComputeSkillTier
		{"skill/create", skill, mustArgs(t, map[string]string{"action": "create", "name": "x"}), scoring.Risky},
		{"skill/update", skill, mustArgs(t, map[string]string{"action": "update", "name": "x"}), scoring.Risky},
		{"skill/delete", skill, mustArgs(t, map[string]string{"action": "delete", "name": "x"}), scoring.Destructive},
		// skill empty/unknown/parse-fail → Risky (saturate upward)
		{"skill/empty", skill, mustArgs(t, map[string]string{"action": ""}), scoring.Risky},
		{"skill/unknown", skill, mustArgs(t, map[string]string{"action": "install"}), scoring.Risky},
		{"skill/parsefail", skill, json.RawMessage(`{not json`), scoring.Risky},

		// task fixed tiers
		{"task/list", task, mustArgs(t, map[string]string{"action": "list"}), scoring.Safe},
		{"task/cancel", task, mustArgs(t, map[string]string{"action": "cancel", "task_id": "1"}), scoring.Normal},
		{"task/run_now", task, mustArgs(t, map[string]string{"action": "run_now", "task_id": "1"}), scoring.Risky},
		// task schedule scored via ComputeTaskTier
		{"task/schedule/reminder", task, mustArgs(t, map[string]string{"action": "schedule", "kind": "reminder"}), scoring.Safe},
		{"task/schedule/nokind", task, mustArgs(t, map[string]string{"action": "schedule"}), scoring.Normal},
		// agent_job schedule force-bumped to at least Risky (AG-016)
		{"task/schedule/agent_job", task, mustArgs(t, map[string]string{"action": "schedule", "kind": "agent_job"}), scoring.Risky},
		// task empty/unknown/parse-fail → Risky
		{"task/empty", task, mustArgs(t, map[string]string{"action": ""}), scoring.Risky},
		{"task/unknown", task, mustArgs(t, map[string]string{"action": "purge"}), scoring.Risky},
		{"task/parsefail", task, json.RawMessage(`{`), scoring.Risky},

		// swarm_spawn → Normal on every input, args included. A worker's tools are a SUBSET
		// of the parent's (runChild removes swarm_spawn itself), so spawning grants no
		// authority the caller did not already hold and use ungated; the fan-out is bounded
		// by the goals/concurrency/timeout caps. It is reserved and audited, not gated.
		{"swarm/goals", swarm, mustArgs(t, map[string][]string{"goals": {"a", "b"}}), scoring.Normal},
		{"swarm/empty", swarm, json.RawMessage(`{}`), scoring.Normal},
		{"swarm/parsefail", swarm, json.RawMessage(`nope`), scoring.Normal},

		// Non-multiplexed fallback: Mutating→Normal, read→Safe.
		//
		// Normal, not Risky, and the difference is the whole point: GateRecommended fires
		// from Risky up, so a Risky floor made every ordinary write — storing a memory,
		// writing a file — stop the turn for operator approval. Normal still reserves and
		// audits; it just does not interrupt. shell_exec keeps its own destructive-pattern
		// approval in internal/agent/tools, which this line never touched.
		{"shell_exec", tools.Spec{Name: "shell_exec", Mutating: true}, json.RawMessage(`{}`), scoring.Normal},
		{"mcp_memory_write", tools.Spec{Name: "memory__memory_add_fact", Mutating: true}, json.RawMessage(`{}`), scoring.Normal},
		{"fs_read", tools.Spec{Name: "fs_read", Mutating: false}, json.RawMessage(`{}`), scoring.Safe},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.spec, tc.args); got != tc.want {
				t.Fatalf("classify(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestClassifyAgentJobDestructivePayload proves a destructive agent_job payload rides
// ComputeTaskTier all the way to Destructive (it is already ≥ Risky, so the AG-016
// force-bump leaves it untouched — never LOWERS it).
func TestClassifyAgentJobDestructivePayload(t *testing.T) {
	args := mustArgs(t, map[string]any{
		"action":  "schedule",
		"kind":    "agent_job",
		"payload": map[string]string{"goal": "rm -rf /"},
	})
	if got := classify(taskSpec(), args); got != scoring.Destructive {
		t.Fatalf("destructive agent_job = %q, want %q", got, scoring.Destructive)
	}
}

// TestClassifyExhaustive drives the covered-action set off the live tool schemas: if a
// new action is added to skill/task without a mapping in classify (its fixed-tier /
// scored tables), this test fails (RESEARCH Pitfall 2 — a new multiplexed action must
// force an explicit classifier decision, never silently ride the unknown→Risky default).
func TestClassifyExhaustive(t *testing.T) {
	t.Run("skill", func(t *testing.T) {
		schema := actionEnum(t, skillSpec())
		covered := map[string]bool{}
		for a := range skillFixedTiers {
			covered[a] = true
		}
		for a := range skillScoredActions {
			covered[a] = true
		}
		assertSameActions(t, "skill", schema, covered)
	})
	t.Run("task", func(t *testing.T) {
		schema := actionEnum(t, taskSpec())
		covered := map[string]bool{}
		for a := range taskFixedTiers {
			covered[a] = true
		}
		for a := range taskScoredActions {
			covered[a] = true
		}
		assertSameActions(t, "task", schema, covered)
	})
}

func assertSameActions(t *testing.T, tool string, schema, covered map[string]bool) {
	t.Helper()
	for a := range schema {
		if !covered[a] {
			t.Errorf("%s: schema action %q has NO classifier mapping (add it to the fixed/scored tables)", tool, a)
		}
	}
	for a := range covered {
		if !schema[a] {
			t.Errorf("%s: classifier maps %q which is NOT in the live schema (dead action?)", tool, a)
		}
	}
	if t.Failed() {
		t.Logf("%s schema actions: %v; covered: %v", tool, sortedKeys(schema), sortedKeys(covered))
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestClassifyReadsNotScored is the anti-landmine proof (D-02c): a skill read reaches
// scoring.Safe only because it is allow-listed. It would gate to Risky if it were ever
// routed through scoring.ComputeSkillTier (whose default branch returns Risky) — this
// test pins that separation by asserting BOTH the ComputeSkillTier landmine value AND
// the correct classify outcome.
func TestClassifyReadsNotScored(t *testing.T) {
	skill := skillSpec()
	for _, action := range []string{"list", "info", "use"} {
		if tier := scoring.ComputeSkillTier(scoring.SkillAction(action), ""); tier != scoring.Risky {
			t.Fatalf("precondition: ComputeSkillTier(%q) = %q, expected the Risky landmine", action, tier)
		}
		args := mustArgs(t, map[string]string{"action": action, "name": "x"})
		if got := classify(skill, args); got != scoring.Safe {
			t.Fatalf("classify(skill %s) = %q, want Safe (read must be allow-listed, not scored)", action, got)
		}
	}
}
