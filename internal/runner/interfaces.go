// Package runner is the orchestration layer that ties the Phase-4 substrate
// together (PRD 1.5 resume + 1.8 lifecycle + 1.8.5 search): the Runner drives the
// agent turn-by-turn, persists each turn via conversations.Store, is the SOLE
// writer of aura.paused_states (it observes the Actions.AwaitingInput pause Event
// and inserts the rows), resolves resumes as a FRESH agent.Run over rehydrated
// history (SC-4: no silent LLM re-run, no duplicate ask_user tool_call), and owns
// the goleak-clean auto-title WaitGroup (D-A5-01).
//
// The Runner is NOT an agent.Agent and must NOT collide with
// internal/agent/workflow.LoopAgent (a control-flow Agent) — it is an orchestrator
// in the ADK-Go `Runner` sense (AM-03). It consumes CONSUMER-SIDE narrow interfaces
// declared here (D-A2-02, "accept interfaces, return structs"): only the methods it
// calls, satisfied implicitly by the concrete *conversations.Store / *askuser.Store
// / *identity.Store. Unit tests pass hand-written in-memory fakes (no DB → supports
// the 85% coverage floor); db_integration tests use the real Stores.
package runner

import (
	"context"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// ConversationStore is the narrow conversation surface the Runner consumes
// (D-A2-02). *conversations.Store satisfies it implicitly. LoadManagedHistory
// returns the L1/L2/L2.5-ladder-applied history; LoadHistory is the raw
// byte-identical reconstruction (used by the auto-title worker so the title sees
// the full opening turns, not the compacted view).
type ConversationStore interface {
	Create(ctx context.Context, p conversations.CreateParams) (conversations.Conversation, error)
	Get(ctx context.Context, conversationID string) (conversations.Conversation, error)
	List(ctx context.Context, includeArchived bool) ([]conversations.Conversation, error)
	UpdateStatus(ctx context.Context, conversationID, status string) error
	Rename(ctx context.Context, conversationID, title string) error
	SetTitleIfNull(ctx context.Context, conversationID, title string) error
	CountTurns(ctx context.Context, conversationID string) (int, error)
	AppendTurn(ctx context.Context, p conversations.AppendTurnParams) error
	AppendAssistantTurnWithCacheMetric(ctx context.Context, p conversations.AppendTurnParams, metric sqlc.InsertCacheMetricParams) error
	LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
	LoadManagedHistory(ctx context.Context, conversationID string, cfg conversations.ContextConfig) ([]llm.Message, error)
	// LoadManagedHistoryForBranch is the D-09 / CHAT-05 path-aware variant: it walks the
	// SELECTED branch leaf->root and feeds that into the same ladder. TurnBranch uses it
	// for the re-run-from-a-point; *conversations.Store satisfies it (plan 25-06).
	LoadManagedHistoryForBranch(ctx context.Context, conversationID string, leafSeq int, cfg conversations.ContextConfig) ([]llm.Message, error)
	SearchConversationTurns(ctx context.Context, query string, limit int) ([]conversations.SearchResult, error)
	Delete(ctx context.Context, conversationID string) error
	// GetForIdentity + DeleteForIdentity are the Phase-36 owner-scoped surface (MUSR-01 /
	// D-06) the delete lifecycle (runner_delete.go) routes through: the owner gate resolves a
	// foreign/absent id to ErrConversationNotFound BEFORE any teardown, and the hard delete
	// returns rows-affected (0 = not owned → the surface maps 403/404). *conversations.Store
	// satisfies both (store_identity.go).
	GetForIdentity(ctx context.Context, conversationID, identityID string) (conversations.Conversation, error)
	DeleteForIdentity(ctx context.Context, conversationID, identityID string) (int64, error)
}

// ContextBlockProvider renders identity-aware context for messages[1]. The Runner
// composes its output before the legacy AlwaysBlock skill renderer.
type ContextBlockProvider func(ctx context.Context, owner identity.Identity) string

// ArchivalRecaller recalls a durable long-term memory block for messages[1] (L4
// archival retrieval, PRD amendment #21). userIdentifier is the owner's identity id
// (the same value the memory-MCP write path scopes on — see mcptools bridge), query
// is the current turn's focus text ("" => identity-only recall). It returns the block
// to inject (empty => nothing recalled). nil on the Runner => the seam is a no-op, so
// the default-off state is produced upstream by the composition root, never here.
type ArchivalRecaller func(ctx context.Context, userIdentifier, query string) (string, error)

// PauseStore is the narrow paused_states surface the Runner consumes (D-A2-02).
// *askuser.Store satisfies it implicitly. The Runner writes Insert on a pause
// Event (T-04-19).
//
// T-04-19 WIDENING (Phase-29 D-13, Option A): the Runner is no longer the SOLE
// writer of aura.paused_states. The capability-gated operator-origin
// governance-write path (the cmd/aura skills-write provider, reachable only behind
// RequireCapability(governance.write)) is a SECOND legitimate writer: a cockpit
// skill install mints an operator-origin ask_user pause via askuser.Store.Insert
// (Kind=approval + ResumeContext={type:"skill_approval"}) so it surfaces in the same
// /api/approvals queue and resolves through the same source-agnostic
// Runner.SubmitAnswers -> ResumeHandler.Resume bridge. The widening is
// capability-scoped, not a blanket relaxation — no model/agent/unauthenticated
// caller can mint a pause (the agent stays name-gated to ask_user,
// llm_agent_pause.go).
type PauseStore interface {
	Insert(ctx context.Context, p askuser.InsertParams) error
	GetByToken(ctx context.Context, token string) (askuser.Pending, error)
	ListPending(ctx context.Context, conversationID string) ([]askuser.Pending, error)
	MarkResumed(ctx context.Context, token string, ans askuser.ResumeAnswer) error
	MarkResumedBatch(ctx context.Context, answers map[string]askuser.ResumeAnswer) error
	AutoResolveForConversation(ctx context.Context, conversationID string) error
}

// ResumeClaim binds one pause's claim (token + AM-02 answer) to the RoleTool answer
// turn it appends, so the ResumeCommitter can resolve and inject a pause in ONE
// cross-store tx (D-03). Turn.Seq is left 0 by the caller: the atomic Pool impl
// reserves the seq under the conversation row-lock inside its tx; the pool-less split
// fallback lets conversations.Store.AppendTurn allocate it.
type ResumeClaim struct {
	Token  string
	Answer askuser.ResumeAnswer
	Turn   conversations.AppendTurnParams
}

// ResumeCommitter is the narrow cross-store HITL-durability seam the Runner consumes
// (D-03/D-05). It owns BOTH the resume path (claim a pause + append its answer turn)
// AND the pause-exposure path (append the assistant ask_user tool_call turn + insert
// its paused_states rows), each as ONE db.WithTx so a claim and its answer — or an
// assistant turn and its pauses — commit all-or-nothing. The pool-owning
// *PoolResumeCommitter (resume_committer.go) satisfies it in production; the pool-less
// splitResumeCommitter is the non-atomic compatibility fallback runner.New defaults to.
type ResumeCommitter interface {
	// CommitResume claims ONE pause (MarkResumed's rows==0 gate → askuser.ErrPauseNotFound
	// for an unknown/already-resumed token) then appends its RoleTool answer turn, in one
	// tx. A failed claim appends nothing; a failed append rolls the claim back (the pause
	// stays pending and is retryable — LOOP-03/F-029).
	CommitResume(ctx context.Context, claim ResumeClaim) error
	// CommitResumeBatch claims ALL pauses (sorted-token, deadlock-free) then appends every
	// answer turn, in one tx. Any rows==0 claim rolls the whole tx back → exactly one
	// answer per pause, no orphan RoleTool turns (LOOP-02/F-004).
	CommitResumeBatch(ctx context.Context, claims []ResumeClaim) error
	// CommitPause writes the assistant ask_user tool_call turn + all N paused_states rows
	// in one tx, so a pause becomes consumable only AFTER its wire-valid assistant history
	// is durable (LOOP-04/F-030).
	CommitPause(ctx context.Context, assistantTurn conversations.AppendTurnParams, pauses []askuser.InsertParams) error
}

// CacheMetricStore is the narrow aura.cache_metrics surface the Runner consumes
// (D-A2-02). *cachemetrics.Store satisfies it implicitly. The Runner writes ONE
// append-only metric row per completed assistant turn from the already-computed
// llm.Usage (D-02/D-02a). It is intentionally Insert-only — the window reads
// (ListSince/AggregateSince) are consumed by `aura cache-stats` in 06-04, not by the
// turn loop, so they are not part of the surface the Runner depends on.
type CacheMetricStore interface {
	Insert(ctx context.Context, p sqlc.InsertCacheMetricParams) error
}

// ToolInvocationStore is the narrow append-only tool ledger surface the Runner
// consumes. It records start/end facts emitted by the agent around dispatched
// tool execution.
type ToolInvocationStore interface {
	Insert(ctx context.Context, e toolinvocations.Event) error
}

// IdentityStore is the narrow identity surface the Runner consumes (D-A2-02).
// *identity.Store satisfies it implicitly. New conversations prefer a real user
// identity and retain `local` only as a legacy fallback for pre-Authula installs.
type IdentityStore interface {
	ListIdentities(ctx context.Context) ([]identity.Identity, error)
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
	GetIdentityByID(ctx context.Context, identityID string) (identity.Identity, error)
}
