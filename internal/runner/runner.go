package runner

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// defaultTitleTimeout bounds the best-effort auto-title LLM call (D-A5-01); it
// outlives the turn ctx (WithoutCancel) but is never unbounded.
const defaultTitleTimeout = 30 * time.Second

// defaultStopTimeout bounds the Runner.Stop wg.Wait() drain so a hung title worker
// cannot wedge shutdown forever.
const defaultStopTimeout = 10 * time.Second

// autoTitleMinSeq is the turn count past which the auto-title worker fires (Req#9:
// "after seq >= 3"). System(1) + first user(2) + first assistant(3).
const autoTitleMinSeq = 3

// localIdentityName is the seeded single-user identity that owns new conversations
// (Slice 1.7 scaffolding).
const localIdentityName = "local"

// Deps are the Runner's constructor inputs: the three consumer-side Stores (narrow
// interfaces, D-A2-02), the LLM client + tool registry the fresh per-round LlmAgent
// is built from (D-A1-05/Pattern-4), the llm.Config (model + L2 context window
// inputs), and the bounded worker timeouts.
type Deps struct {
	Conv         ConversationStore
	Pause        PauseStore
	Identity     IdentityStore
	Client       llm.Client
	Registry     *tools.Registry
	LLM          llm.Config
	RunDir       string
	PreviewCap   int
	EvictAfter   int // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 eviction age
	TitleTimeout time.Duration
	StopTimeout  time.Duration
}

// Runner is the orchestration layer (D-A1-01): it drives the agent turn-by-turn,
// persists each turn, is the SOLE writer of paused_states, resolves resumes as a
// FRESH agent.Run over rehydrated history (SC-4), and owns the auto-title
// WaitGroup. It is NOT an agent.Agent and does not collide with
// workflow.LoopAgent (AM-03). The exported Conv field lets the composition root /
// CLI read conversations directly (list/search/lifecycle) without re-plumbing the
// narrow interface; pause/title orchestration stays in the Runner.
type Runner struct {
	Conv     ConversationStore
	pause    PauseStore
	identity IdentityStore

	client     llm.Client
	registry   *tools.Registry
	cfg        llm.Config
	runDir     string
	previewCap int
	evictAfter int

	titleTimeout time.Duration
	stopTimeout  time.Duration

	wg sync.WaitGroup // tracks the auto-title workers (D-A5-01); Stop joins it (goleak-clean)
}

// New builds a Runner over the supplied dependencies, applying the timeout
// defaults when a caller leaves them zero.
func New(d Deps) *Runner {
	titleTimeout := d.TitleTimeout
	if titleTimeout <= 0 {
		titleTimeout = defaultTitleTimeout
	}
	stopTimeout := d.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = defaultStopTimeout
	}
	return &Runner{
		Conv:         d.Conv,
		pause:        d.Pause,
		identity:     d.Identity,
		client:       d.Client,
		registry:     d.Registry,
		cfg:          d.LLM,
		runDir:       d.RunDir,
		previewCap:   d.PreviewCap,
		evictAfter:   d.EvictAfter,
		titleTimeout: titleTimeout,
		stopTimeout:  stopTimeout,
	}
}

// ResponseInput is the CLI/caller-facing resume payload (the MCP three-action model,
// D-A3-01 / AM-02). It maps to askuser.ResumeAnswer at the Store boundary.
type ResponseInput struct {
	Action  string // accept | decline | cancel
	Content string
}

// NewConversation creates a new active conversation owned by the seeded `local`
// identity and returns its id. The composition root / `aura chat new` calls it.
func (r *Runner) NewConversation(ctx context.Context) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint conversation id: %w", err)
	}
	return r.NewConversationWithID(ctx, id.String())
}

// NewConversationWithID creates a conversation with a caller-supplied id (the REPL
// mints the id so it can key the sidecar dir before the row exists).
func (r *Runner) NewConversationWithID(ctx context.Context, conversationID string) (string, error) {
	owner, err := r.identity.GetIdentityByName(ctx, localIdentityName)
	if err != nil {
		return "", fmt.Errorf("new conversation: resolve %q identity: %w", localIdentityName, err)
	}
	if _, err := r.Conv.Create(ctx, conversations.CreateParams{
		ID:         conversationID,
		IdentityID: owner.ID,
		Model:      r.cfg.Model,
	}); err != nil {
		return "", fmt.Errorf("new conversation: %w", err)
	}
	return conversationID, nil
}

// Turn drives ONE LLM round over the conversation and is the sole loop-driver
// (D-A1-06). userMsg!=nil starts a round with a fresh user message; userMsg=nil is
// "continue after resume" — the resolved answers are already RoleTool turns in the
// persisted history (SubmitAnswer wrote them), so Turn just re-runs a fresh agent
// over the rehydrated history (SC-4: no silent re-run, the answer pair is already
// in the messages). On an Actions.AwaitingInput Event it writes N paused_states
// rows (SOLE writer, T-04-19) and stops the loop while >=1 stays unresolved.
//
// Resume = a FRESH agent.Run over rehydrated history (D-A1-05): a range-over-func
// iterator cannot be suspended, so durability is entirely in the Stores. The
// yield-after-false guard is honored (never yield again once the consumer returns
// false).
func (r *Runner) Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Persist the new user turn (if any) BEFORE rehydrating so the agent sees it.
		if userMsg != nil {
			if err := r.appendUserTurn(ctx, convID, *userMsg); err != nil {
				yield(nil, err)
				return
			}
		}

		history, err := r.Conv.LoadManagedHistory(ctx, convID, r.contextConfig())
		if err != nil {
			yield(nil, err)
			return
		}

		la, ic, err := r.buildAgent(ctx, convID, history)
		if err != nil {
			yield(nil, err)
			return
		}

		// Drive one fresh agent round, persisting each emitted turn and observing the
		// pause Event(s). tracker accumulates what to persist + whether we paused; on a
		// pause it also accumulates the round's ask_user calls so flushPause can write
		// them as ONE assistant turn (CR-02). The flush is deferred to round end
		// because the agent emits one pause Event per call but rewrites its history to
		// a single multi-tool_call assistant message.
		tr := &turnTracker{convID: convID}
		for ev, runErr := range la.Run(ic) {
			if runErr != nil {
				yield(nil, runErr)
				return
			}
			if perr := r.persistEvent(ctx, tr, ev); perr != nil {
				yield(nil, perr)
				return
			}
			if !yield(ev, nil) {
				return // consumer stopped — iter.Seq2 contract forbids a further yield
			}
		}

		// Flush the single combined assistant ask_user turn (no-op when the round did
		// not pause). Done after la.Run drains so the persisted turn carries ALL the
		// round's ask_user tool_calls in one wire-valid message (CR-02).
		if err := r.flushPause(ctx, tr); err != nil {
			yield(nil, err)
			return
		}

		// Post-round bookkeeping: fire the auto-title worker when the conversation has
		// reached seq>=3 and is not titled yet. Skipped on a pause (no assistant turn).
		if !tr.paused {
			r.maybeAutoTitle(ctx, convID, history)
		}
	}
}

// appendUserTurn persists the user message as the next turn.
func (r *Runner) appendUserTurn(ctx context.Context, convID, content string) error {
	seq, err := r.nextSeq(ctx, convID)
	if err != nil {
		return err
	}
	return r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Seq: seq, Role: llm.RoleUser, Content: content,
	})
}

// nextSeq returns the seq of the next turn (CountTurns + 1).
func (r *Runner) nextSeq(ctx context.Context, convID string) (int, error) {
	n, err := r.Conv.CountTurns(ctx, convID)
	if err != nil {
		return 0, fmt.Errorf("next seq: %w", err)
	}
	return n + 1, nil
}

// contextConfig builds the L1/L2/L2.5 ladder inputs from the Runner's llm.Config +
// eviction knob.
func (r *Runner) contextConfig() conversations.ContextConfig {
	return conversations.ContextConfig{
		ContextWindow:       r.cfg.ContextWindow,
		MaxOutputTokens:     r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns: r.evictAfter,
	}
}

// buildAgent constructs a FRESH LlmAgent seeded with the rehydrated history
// (D-A1-05 / Pattern-4) and its InvocationContext (a per-turn Budget from the
// AURA_LOOP_* env). The history is passed via LlmAgentConfig.UserTurns; the agent
// prepends its own byte-stable system message (AM-01: LoadHistory is the Store's,
// not the agent's). When the loaded history already carries a leading system turn
// (a persisted seq=1), it is dropped here so the agent's own system message is not
// duplicated.
func (r *Runner) buildAgent(ctx context.Context, convID string, history []llm.Message) (*agent.LlmAgent, agent.InvocationContext, error) {
	requestID, err := uuid.NewV7()
	if err != nil {
		return nil, agent.InvocationContext{}, fmt.Errorf("mint request id: %w", err)
	}
	bud, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		return nil, agent.InvocationContext{}, fmt.Errorf("budget config (check AURA_LOOP_* env): %w", err)
	}
	seed := stripLeadingSystem(history)
	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     r.client,
		LLM:        r.cfg,
		Registry:   r.registry,
		PreviewCap: r.previewCap,
		RunDir:     r.runDir,
		SessionID:  convID, // session_id == conversation_id (D-26)
		UserTurns:  seed,
	})
	ic := agent.InvocationContext{
		Ctx:       ctx,
		Agent:     la,
		RequestID: requestID,
		Branch:    "root",
		Budget:    bud,
	}
	return la, ic, nil
}

// stripLeadingSystem drops a persisted leading system turn so the agent's own
// byte-stable system message is the sole messages[0] (KV-cache discipline,
// Pitfall 1). A history without a leading system turn is returned unchanged.
func stripLeadingSystem(history []llm.Message) []llm.Message {
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		return history[1:]
	}
	return history
}
