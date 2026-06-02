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
	LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
	LoadManagedHistory(ctx context.Context, conversationID string, cfg conversations.ContextConfig) ([]llm.Message, error)
	SearchConversationTurns(ctx context.Context, query string, limit int) ([]conversations.SearchResult, error)
	Delete(ctx context.Context, conversationID string) error
}

// PauseStore is the narrow paused_states surface the Runner consumes (D-A2-02).
// *askuser.Store satisfies it implicitly. The Runner is the SOLE caller of Insert
// (T-04-19: only the Runner writes paused_states, on a pause Event).
type PauseStore interface {
	Insert(ctx context.Context, p askuser.InsertParams) error
	GetByToken(ctx context.Context, token string) (askuser.Pending, error)
	ListPending(ctx context.Context, conversationID string) ([]askuser.Pending, error)
	MarkResumed(ctx context.Context, token string, ans askuser.ResumeAnswer) error
	MarkResumedBatch(ctx context.Context, answers map[string]askuser.ResumeAnswer) error
	AutoResolveForConversation(ctx context.Context, conversationID string) error
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

// IdentityStore is the narrow identity surface the Runner consumes (D-A2-02).
// *identity.Store satisfies it implicitly. The Runner looks up the owning identity
// when it creates a new conversation (single-user `local` scaffolding, Slice 1.7).
type IdentityStore interface {
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
}
