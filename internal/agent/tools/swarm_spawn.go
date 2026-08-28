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
//
// SWARM-01 (plan 51-03) widened the signature with a context string — the same
// interface, extended in place, never a second one (51-PATTERNS.md's explicit
// instruction).
type swarmRunner interface {
	Run(ctx context.Context, goals []string, context string) (ToolResult, error)
}

// SwarmCaps is the SWARM-02 live-cap struct swarm_spawn's Spec() renders into
// its own JSON schema at call time, so the model reads the operator's REAL
// configured limits instead of discovering them by failing. Injected at
// construction from the composition root (cmd/aura), which already reads
// config.Config — this package never imports internal/config or internal/swarm
// itself. MaxDepth is AURA_SWARM_MAX_DEPTH — rendered into THIS tool's own
// schema/description, deliberately NOT added to the Tier A/B operator knob
// registry (internal/config/config_knobs_test.go bans it there on purpose; a
// model-facing tool schema is a different catalog, per 51-RESEARCH.md Open
// Question 1).
type SwarmCaps struct {
	MaxGoals      int
	MaxConcurrent int
	// ChildIdleSec is AURA_SWARM_CHILD_IDLE_SEC (D-03, plan 51-09): the per-worker
	// INACTIVITY deadline a worker's own event loop resets on every progress event
	// -- not a wall-clock age bound. Renamed from the retired
	// AURA_SWARM_CHILD_TIMEOUT_SEC (Amendment #154 measured the wall clock as the
	// wrong instrument: four workers, a 23x margin, the cap firing once on an
	// upstream stall while the worker worth interrupting sailed under it).
	ChildIdleSec int
	MaxDepth     int
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
// is wired at the composition root (cmd/aura) to the internal/swarm adapter; Caps
// carries the live goal/concurrency/timeout/depth ceilings rendered into Spec()'s
// schema (SWARM-02).
type SwarmSpawn struct {
	Runner swarmRunner
	Caps   SwarmCaps
}

// swarmSpawnArgs is the D-03/SWARM-01 schema: goals plus a Context string
// carrying the file paths, error messages and constraints kept separate from
// the objective. Context is optional — an absent/empty value means "no
// context", not a Go zero-value error.
type swarmSpawnArgs struct {
	Goals   []string `json:"goals"`
	Context string   `json:"context,omitempty"`
}

func (e *SwarmSpawn) Spec() Spec {
	params := renderSwarmSpawnParams(e.Caps)
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
		// The reference coding agent keeps its delegation tool always-active, and fanning work
		// out IS decided while reading the request — but its description is a few lines, while
		// swarmSpawnDescription is 4,364 chars (~1,100 tokens) paid on every turn. Promote this
		// only after the description is cut to that size; the set is not the whole story, the
		// bytes are.
		Deferred: true,
		// D-02/D-02d: a swarm worker turn wields the full tool set, so swarm_spawn is
		// the fail-closed Mutating floor. It has no `action` field, but it is treated
		// as Multiplexed so the boot-guard asserts the classifier tiers it (flat Risky).
		Mutating:       true,
		Multiplexed:    true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
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
	if e.Caps.MaxGoals > 0 && len(a.Goals) > e.Caps.MaxGoals {
		return NewResult(ctx, fmt.Sprintf(
			"error: too many goals — %d exceeds the AURA_SWARM_MAX_GOALS cap of %d; split into fewer parallel subtasks or answer directly",
			len(a.Goals), e.Caps.MaxGoals))
	}
	if e.Runner == nil {
		return ToolResult{}, fmt.Errorf("swarm_spawn: runner is not configured")
	}
	return e.Runner.Run(ctx, a.Goals, a.Context)
}

// swarmSpawnSchema is the typed JSON-schema shape renderSwarmSpawnParams
// marshals — a struct, not a hand-built string, so the live cap values are
// interpolated through Go's own type system rather than string-templated into
// a literal. Description carries the SWARM-02 live-cap summary at the schema
// root (json schema's own "description" annotation slot); the per-property
// descriptions stay scoped to their own arg.
type swarmSpawnSchema struct {
	Type        string                        `json:"type"`
	Description string                        `json:"description"`
	Properties  map[string]swarmSpawnPropSpec `json:"properties"`
	Required    []string                      `json:"required"`
}

// swarmSpawnPropSpec is one JSON-schema property. Items/MinItems are omitted
// (omitempty) for the context property, which is a plain string.
type swarmSpawnPropSpec struct {
	Type        string               `json:"type"`
	Items       *swarmSpawnItemsSpec `json:"items,omitempty"`
	MinItems    int                  `json:"minItems,omitempty"`
	Description string               `json:"description"`
}

type swarmSpawnItemsSpec struct {
	Type string `json:"type"`
}

// renderSwarmSpawnParams builds swarm_spawn's JSON Parameters from the injected
// live caps via a typed struct + encoding/json marshal — never a static
// json.RawMessage literal (SWARM-02's own prohibition) and never a hardcoded
// env var NAME (only the VALUE — AURA_SWARM_CHILD_TIMEOUT_SEC was retired by
// plan 51-09 in favour of AURA_SWARM_CHILD_IDLE_SEC, and a hardcoded name here
// would have gone stale silently). encoding/json marshals map keys in sorted
// order, so the output is deterministic across repeated calls with the same
// caps (Spec() is read at call time, never cached).
func renderSwarmSpawnParams(caps SwarmCaps) json.RawMessage {
	schema := swarmSpawnSchema{
		Type: "object",
		Description: fmt.Sprintf(
			"Operator caps in effect for this call: up to %d goals per call, "+
				"%d running concurrently per wave, each worker reaped after %d seconds "+
				"with no progress (D-03: silence, not age), and nested delegation capped "+
				"at depth %d.",
			caps.MaxGoals, caps.MaxConcurrent, caps.ChildIdleSec, caps.MaxDepth),
		Properties: map[string]swarmSpawnPropSpec{
			"goals": {
				Type:     "array",
				Items:    &swarmSpawnItemsSpec{Type: "string"},
				MinItems: 1,
				Description: fmt.Sprintf(
					"Two or more independent, self-contained subtask briefs (up to %d per call). "+
						"Each entry is one worker's complete instructions (objective + output format + boundaries); "+
						"the worker sees only this text.",
					caps.MaxGoals),
			},
			"context": {
				Type: "string",
				Description: "The file paths, error messages and constraints the worker needs — " +
					"kept separate from the goal.",
			},
		},
		Required: []string{"goals"},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		// schema is a static Go struct with no dynamic/unmarshalable field types —
		// a marshal failure here is a programmer error, not a runtime condition.
		panic(fmt.Sprintf("swarm_spawn: render params: %v", err))
	}
	return json.RawMessage(b)
}
