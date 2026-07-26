// Package handlers holds the per-TaskKind scheduler handlers (D-28 / Slice 0.9):
// each TaskKind is ONE file with a Handler impl + a HandlerMeta — no central
// dispatch switch. The package is deliberately free of any internal/cron import
// (D-24 forbids the reverse-import cycle): a handler receives a plain Job value and
// returns a summary string; the cron dispatcher (internal/cron/dispatch.go) owns
// run completion, notification, and the agent_job_runs writes. agent_job mirrors
// swarm.runChild (a budgeted, tool-bound LlmAgent) WITHOUT importing internal/swarm
// (D-24): tools.Without was promoted to the tools package precisely so cron can drop
// swarm_spawn without that import.
package handlers

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
)

// TaskKind mirrors cron.TaskKind as a plain string so the handlers package stays
// free of an internal/cron import (D-24). The cron dispatcher maps its own
// cron.TaskKind onto these constants when routing.
type TaskKind string

// The task kinds; one Handler per kind (D-28). skill_ttl_sweep (D-16) is the
// Phase-11 system-seeded snippet TTL sweep.
const (
	KindReminder       TaskKind = "reminder"
	KindAgentJob       TaskKind = "agent_job"
	KindBackupPostgres TaskKind = "backup_postgres"
	KindBackupNeo4j    TaskKind = "backup_neo4j"
	KindSkillTTLSweep  TaskKind = "skill_ttl_sweep"
)

// HandlerMeta is the per-kind static contract (Slice 0.9): the kind it serves, its
// wall-clock budget, and whether a missed window reschedules it on recovery (D-18).
// It replaces a dispatch switch — the dispatcher reads Meta() to route and bound.
type HandlerMeta struct {
	Kind                  TaskKind
	MaxDuration           time.Duration
	ReschedulesOnRecovery bool
}

// Job is the plain-value input a Handler runs against — the cron dispatcher
// projects a claimed task + run onto this shape so the handlers package never
// imports internal/cron (D-24). Payload is the raw task payload JSON; StepBudget is
// the agent_job_runs.step_budget row value the agent_job inherits (D-24); RunID keys
// the ephemeral agent session (amendment #23).
type Job struct {
	Payload    []byte
	StepBudget int
	RunID      string
	// MissedSince is non-zero only for a boot catch-up run (D-18): the original
	// slipped fire. The backup handler emits the SC#3 missed-past-the-window alert
	// from it; a zero value means this is a normal on-time run.
	MissedSince time.Time
	// OriginConversationID is the conversation UUID the task was scheduled from (may be
	// empty for a task with no origin). It is the gateway ledger key for a headless
	// agent_job so a mutating denial is durably recorded on a real conversation UUID
	// (Open Q1) — the flat "agent_job:<runID>" session never keys the ledger.
	OriginConversationID string
}

// Handler is the per-kind unit of work (Slice 0.9: Handler ~ a small Run seam, not
// the full agent.Agent — a handler may itself CONSTRUCT an agent.Agent, e.g.
// agent_job). Run returns the audit summary written to agent_job_runs.summary plus a
// terminal error (nil on success). A handler NEVER blocks on human input — agent_job
// auto-rejects ask_user (D-25).
type Handler interface {
	Meta() HandlerMeta
	Run(ctx context.Context, job Job) (summary string, err error)
}

// AgentDeps carries the shared runtime an agent_job needs to construct its ephemeral
// LlmAgent (mirroring swarm.RunConfig): the LLM client + config, the PARENT tool
// registry (the agent_job runs the registry minus swarm_spawn, D-13), and the
// sidecar knobs. It is plain composition — no internal/cron types.
type AgentDeps struct {
	Client           llm.Client
	LLM              llm.Config
	Registry         *tools.Registry
	PreviewCap       int
	RunDir           string
	ReasoningControl agent.ReasoningControl
	// Workspace is the shell workspace path announced in the per-turn tail hint
	// (#52/D-41) — without it a scheduled agent never learns where deliverables go.
	// Empty defaults to the process cwd in newAgentWorker, mirroring runner.New.
	Workspace string
	// MaxDuration bounds the whole agent_job wall-clock (D-11 swarm-child analog); 0
	// disables the soft wall budget (the Budget's own wallclock still applies).
	MaxDuration time.Duration
	// Gateway is the Phase-35 policy PEP the ephemeral agent_job agent runs under. A
	// headless job has no interactive responder, so a mutating GateRecommended call
	// degrades to deny-with-guidance and leaves a durable decision-fact keyed on the
	// originating conversation UUID (Open Q1). nil is an Allow no-op.
	Gateway *gateway.Gateway
}

// childRegistry is the agent_job's tool registry: the full parent registry MINUS
// swarm_spawn (D-13). ask_user STAYS (D-25 wants the model to see the rejection).
// Promoted tools.Without keeps internal/swarm out of the import graph (D-24).
func childRegistry(parent *tools.Registry) *tools.Registry {
	return tools.Without(parent, "swarm_spawn")
}

// newAgentWorker constructs the ephemeral LlmAgent for one agent_job turn, mirroring
// swarm.runChild's NewLlmAgent construction VERBATIM except for the FLAT ephemeral
// SessionID (amendment #23: agent_job:<runID>, no DB-backed history). prior carries
// any resume turns (the ask_user auto-reject inject-and-continue, D-25); on the first
// turn it is just the goal user turn.
func newAgentWorker(deps AgentDeps, runID, ledgerConvID string, prior []llm.Message) *agent.LlmAgent {
	// The workspace announced in the tail hint mirrors runner.New: shell_exec and
	// the fs tools with an empty WorkspaceRoot resolve against the process cwd, so
	// the hint must name exactly that when no explicit workspace is configured.
	ws := deps.Workspace
	if ws == "" {
		if wd, err := os.Getwd(); err == nil {
			ws = filepath.ToSlash(wd)
		}
	}
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     deps.Client,
		LLM:        deps.LLM,
		Registry:   childRegistry(deps.Registry),
		PreviewCap: deps.PreviewCap,
		RunDir:     deps.RunDir,
		SessionID:  "agent_job:" + runID,
		// The gateway ledger key is the ORIGINATING conversation UUID, NOT the flat
		// "agent_job:<runID>" SessionID above (uuid.Parse fails on it) (Open Q1).
		LedgerConversationID: ledgerConvID,
		Gateway:              deps.Gateway,
		ReasoningControl:     deps.ReasoningControl,
		Workspace:            ws,
		UserTurns:            prior,
	})
}
