package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// swarmRunner is the engine seam swarm_spawn delegates to. It is defined HERE (in
// the tools package, NOT internal/swarm or internal/agent) so swarm_spawn.go
// imports neither — breaking the cycle tools→swarm→agent→tools. The concrete
// implementation lives in internal/swarm (RunnerAdapter), reads the parent deps
// off the ctx (agent.WithSwarmContext), and calls the 09-02 engine. This mirrors
// the sandbox_exec.go sandboxRunner pattern EXACTLY (interface in the tool
// package, concrete impl injected at construction).
type swarmRunner interface {
	Run(ctx context.Context, goals []string) (ToolResult, error)
}

// swarmSpawnDescription is the load-bearing D-24 anti-over-spawn literal. It is the
// BM25-discoverable document for this Deferred tool (the manifest shows only the
// Summary until tool_search loads this). It MUST carry the four anti-over-spawn
// phrases a test asserts via strings.Contains:
//   - use ONLY for 2 or more independent, self-contained subtasks
//   - a simple single task = answer directly (do not spawn)
//   - each goal is a complete brief (objective + output format + boundaries)
//   - the worker cannot see the conversation
const swarmSpawnDescription = "Run several independent subtasks in parallel as headless worker agents and " +
	"get back one report per subtask. " +
	"Use this ONLY when the work splits into 2 or more independent, self-contained subtasks that can run at the same time. " +
	"A simple single task is NOT a reason to spawn — answer it directly yourself instead of spawning a swarm. " +
	"Each goal must be a complete, standalone brief: state its objective, the output format you expect, and its boundaries, " +
	"because the worker cannot see the conversation, the user, the other workers, or anything outside the goal text you give it. " +
	"Each worker returns a single final report; you read those reports and synthesize the answer. " +
	"Example: {\"goals\":[\"Summarize the Q1 revenue figures from the attached notes\",\"List the top 3 competitor product launches this quarter\"]}."

// SwarmSpawn is the Deferred:true fan-out tool (D-01). It validates the goals cap
// (D-13) and delegates to the injected swarmRunner; it constructs no worker and
// imports no engine package itself, so the tools package stays cycle-free. Runner
// is wired at the composition root (cmd/aura) to the internal/swarm adapter;
// MaxGoals comes from config (AURA_SWARM_MAX_GOALS).
type SwarmSpawn struct {
	Runner   swarmRunner
	MaxGoals int
}

// swarmSpawnArgs is the D-03 schema: goals ONLY, no tier param.
type swarmSpawnArgs struct {
	Goals []string `json:"goals"`
}

func (e *SwarmSpawn) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "goals": {
      "type": "array",
      "items": {"type": "string"},
      "minItems": 1,
      "description": "Two or more independent, self-contained subtask briefs. Each entry is one worker's complete instructions (objective + output format + boundaries); the worker sees only this text."
    }
  },
  "required": ["goals"]
}`)
	return Spec{
		Name: "swarm_spawn",
		// The Summary is ALL the model sees in the default manifest (deferred stub)
		// — it must carry the WHEN, not just the what, or the model never reaches
		// for the tool (live E2E 2026-06-04: sequential execution on a clearly
		// 2-task prompt, workers=0). The anti-over-spawn counterweight (D-24)
		// stays in the Description and in the trailing "2 or more independent"
		// qualifier here.
		Summary: "Run 2 or more independent subtasks in parallel as worker agents and collect their reports. " +
			"Whenever the user asks for multiple unrelated things in one request, call this instead of doing them one by one. " +
			`Call shape: {"goals":["<complete brief for subtask 1>","<complete brief for subtask 2>"]}.`,
		Description: swarmSpawnDescription,
		Parameters:  params,
		Deferred:    true,
		// D-02/D-02d: a swarm worker turn wields the full tool set, so swarm_spawn is
		// the fail-closed Mutating floor. It has no `action` field, but it is treated
		// as Multiplexed so the boot-guard asserts the classifier tiers it (flat Risky).
		Mutating:    true,
		Multiplexed: true,
	}
}

// Execute unmarshals {goals}, rejects an over-cap call with a model-readable inline
// error (D-13, no runner call), and otherwise delegates to the injected runner. A
// missing runner is a real Go error (composition-root wiring bug); domain
// rejections ride in the NewResult string so the model self-corrects.
func (e *SwarmSpawn) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a swarmSpawnArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("swarm_spawn args: %w", err)
	}
	if e.MaxGoals > 0 && len(a.Goals) > e.MaxGoals {
		return NewResult(ctx, fmt.Sprintf(
			"error: too many goals — %d exceeds the AURA_SWARM_MAX_GOALS cap of %d; split into fewer parallel subtasks or answer directly",
			len(a.Goals), e.MaxGoals))
	}
	if e.Runner == nil {
		return ToolResult{}, fmt.Errorf("swarm_spawn: runner is not configured")
	}
	return e.Runner.Run(ctx, a.Goals)
}
