package runner

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
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

// localIdentityName is the pre-Authula seeded identity. It remains only as a
// fallback for legacy databases that have no user identity yet.
const localIdentityName = "local"

// ErrThreadBusy reports that a caller tried to start a second concurrent run for
// the same conversation through a non-blocking channel such as AG-UI.
var ErrThreadBusy = errors.New("thread already has an in-flight run")

type threadLockHeldKey struct{}

// WithThreadLockHeld marks a context whose caller already owns the per-thread
// runner lock. It lets HTTP gateways reject busy threads up front without making
// Runner.Turn take the same lock twice.
func WithThreadLockHeld(ctx context.Context) context.Context {
	return context.WithValue(ctx, threadLockHeldKey{}, true)
}

func threadLockHeld(ctx context.Context) bool {
	held, _ := ctx.Value(threadLockHeldKey{}).(bool)
	return held
}

// Deps are the Runner's constructor inputs: the three consumer-side Stores (narrow
// interfaces, D-A2-02), the LLM client + tool registry the fresh per-round LlmAgent
// is built from (D-A1-05/Pattern-4), the llm.Config (model + L2 context window
// inputs), and the bounded worker timeouts.
type Deps struct {
	Conv            ConversationStore
	Pause           PauseStore
	Identity        IdentityStore
	CacheMetrics    CacheMetricStore
	ToolInvocations ToolInvocationStore
	MemoryContext   MemoryContextProvider
	// ResumeCommitter is the cross-store HITL-durability seam (D-03/D-05). The
	// composition root injects a pool-owning *PoolResumeCommitter so single/batch resume
	// and pause exposure each commit in ONE db.WithTx; nil => New defaults to the
	// pool-less splitResumeCommitter (unit tests, cache_audit) with no code change.
	ResumeCommitter ResumeCommitter
	Client          llm.Client
	Registry        *tools.Registry
	LLM             llm.Config
	// Breaker is the SHARED process-lifetime LLM circuit breaker (B-05). The
	// composition root may inject one; nil => New mints the default. It is threaded
	// into every per-turn agent so a provider outage trips cross-turn protection.
	Breaker    *llm.Breaker
	RunDir     string
	PreviewCap int
	HistoryCap int
	EvictAfter int // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 eviction age
	// CompactionEnabled turns on L2.4 LLM compaction in the context ladder
	// (AURA_CONTEXT_COMPACTION_ENABLED). CompactionModel overrides the summarizer model
	// (AURA_CONTEXT_COMPACTION_MODEL); empty → the main chat model (d.LLM.Model).
	CompactionEnabled bool
	CompactionModel   string
	// MemoryPreloadEnabled turns on the proactive per-message memory preload
	// (AURA_MEMORY_PRELOAD_ENABLED): a memory_search over the current user text injected
	// alongside the always-on digest. Default off — it adds an MCP round-trip per turn.
	MemoryPreloadEnabled bool
	Workspace            string // shell workspace announced per turn (#52/D-41); "" → the process cwd
	TitleTimeout         time.Duration
	StopTimeout          time.Duration
	// ReasoningPersistMaxRunes caps the display-only CoT accumulated per turn and
	// persisted onto conversation_turns.reasoning (amendment #91 / fix-plan 1.12,
	// AURA_REASONING_PERSIST_MAX_RUNES — the composition root passes the resolved
	// knob). <=0 disables persistence entirely, which is also the hand-built-Deps
	// zero value, so tests opt in explicitly.
	ReasoningPersistMaxRunes int
	// AlwaysBlock renders the messages[1] always-on skill block per turn from current
	// loader state (D-07). The composition root wires it over skills.RenderAlwaysBlock
	// + the live loader; nil means no skills are wired (the block is empty). Rebuilt
	// every turn so a skill add/remove changes messages[1] without busting messages[0].
	AlwaysBlock func() string
	ResumeHook  ResumeHook
	// Embedder wires the local embedding-based reasoning-tier classifier into
	// each per-turn agent (replaces the LLM router round-trip). nil => the agent
	// falls back to the LLM router. The composition root passes a
	// documents.EmbeddingClient built over the resolved config.EmbedRoute
	// (local sidecar or the cloud endpoint, the one-knob swap).
	Embedder prompt.Embedder
	// HookManager is the optional agent extension surface. nil keeps the agent's
	// hook calls as no-ops; production may inject command hooks here.
	HookManager *agent.HookManager
	// VerificationStore is the process-wide verification evidence ledger (it holds the
	// pgxpool, so it is built ONCE at the composition root, never per turn). The runner
	// derives the per-turn read and write halves from it (runner_verification.go). nil —
	// which agent.NewEvidenceStore returns for a nil pool — disables the verify-on-stop
	// gate entirely rather than degrading to a panic.
	VerificationStore *agent.EvidenceStore
	// VerificationDetector resolves the per-identity project detector the gate's read half
	// and the hook's write half both consult. It is process-wide (it memoizes box probes),
	// so the composition root builds it ONCE beside VerificationStore. nil disables the
	// verify-on-stop gate: a detector that recognises nothing leaves the gate with nothing
	// to say, so paying for a ledger read at every voluntary termination would be waste.
	VerificationDetector ProjectDetectorSource
	// Gateway is the Phase-35 policy PEP (GATE-01) injected into every per-turn agent.
	// The runner is the INTERACTIVE composition root, so it marks its turn ctx with a
	// live responder (gateway.WithResponder) — under a strict profile a mutating
	// GateRecommended call routes to an approval pause here, never a headless deny. nil
	// is an Allow no-op (dev-parity).
	Gateway *gateway.Gateway
	// ShareRevoker is the D-15 consumer-declared seam (runner_delete.go step 4.5): the
	// composition root injects the live *share.Service, structurally satisfying the
	// interface with no internal/share import here. nil => share was never mounted in this
	// deployment, and step 4.5 is a silent skip (not a panic).
	ShareRevoker ShareRevoker
}

// ResumeHook is called after a paused ask_user response is persisted and before
// the runner resumes the pending turn.
type ResumeHook func(ctx context.Context, pending askuser.Pending, resp ResponseInput) error

// Runner is the orchestration layer (D-A1-01): it drives the agent turn-by-turn,
// persists each turn, is the SOLE writer of paused_states, resolves resumes as a
// FRESH agent.Run over rehydrated history (SC-4), and owns the auto-title
// WaitGroup. It is NOT an agent.Agent and does not collide with
// workflow.LoopAgent (AM-03). The exported Conv field lets the composition root /
// CLI read conversations directly (list/search/lifecycle) without re-plumbing the
// narrow interface; pause/title orchestration stays in the Runner.
type Runner struct {
	Conv            ConversationStore
	pause           PauseStore
	identity        IdentityStore
	cacheMetrics    CacheMetricStore
	toolInvocations ToolInvocationStore
	memoryContext   MemoryContextProvider
	resumeCommitter ResumeCommitter // cross-store HITL-durability seam (D-03/D-05); split fallback when unset

	client     llm.Client
	registry   *tools.Registry
	cfg        llm.Config
	breaker    *llm.Breaker // SHARED process-lifetime LLM circuit breaker, injected into every per-turn agent (B-05)
	runDir     string
	previewCap int
	historyCap int
	evictAfter int
	// compactionEnabled + compactionModel drive the L2.4 summarizer the context ladder
	// receives via ContextConfig.Summarizer (compactionModel is pre-resolved to the main
	// chat model when the knob is empty).
	compactionEnabled    bool
	compactionModel      string
	memoryPreloadEnabled bool   // proactive per-message memory_search preload (AURA_MEMORY_PRELOAD_ENABLED)
	workspace            string // the shell workspace path the per-turn tail hint announces (#52/D-41)
	// reasoningPersistMaxRunes bounds the per-turn display-only CoT accumulator
	// (amendment #91); <=0 disables persistence (see Deps.ReasoningPersistMaxRunes).
	reasoningPersistMaxRunes int
	// gatewayOwnsToolStarts skips the ledger `start` write because the gateway already makes
	// it as its reservation. Derived from the injected gateway, never hand-set: see
	// gateway.OwnsToolStartRows for why a second writer of that row breaks GATE-03/04.
	gatewayOwnsToolStarts bool

	titleTimeout time.Duration
	stopTimeout  time.Duration
	resumeHook   ResumeHook

	hookManager *agent.HookManager // optional per-turn LlmAgent hooks
	// verificationStore is the process-wide evidence ledger (pool-owning); nil disables
	// the verify-on-stop gate. See runner_verification.go for the per-turn halves.
	verificationStore *agent.EvidenceStore
	// verificationDetector resolves the per-identity project detector both halves read;
	// nil disables the gate for the same reason a nil store does — without detection
	// nothing is ever a project, so the gate could only ever say nothing.
	verificationDetector ProjectDetectorSource
	alwaysBlock          func() string               // renders the messages[1] always-block per turn (D-07); nil → empty
	classifier           *prompt.ReasoningClassifier // SHARED reasoning-tier classifier (anchors built once); nil → LLM router
	gateway              *gateway.Gateway            // Phase-35 policy PEP injected into every per-turn agent; nil → Allow no-op
	shareRevoker         ShareRevoker                // D-15 consumer-declared seam (runner_delete.go step 4.5); nil → step 4.5 is a silent skip

	// threadLocks + sessions are the two per-conversation in-memory maps, BOTH keyed by
	// the composite (identity, session) sessionKey (D-23, runner_session.go): threadLocks
	// serializes concurrent turns on one conversation; sessions holds each live turn's
	// ctx-cancel so the delete lifecycle can abort exactly the owner's in-flight turn.
	threadLocks sync.Map       // sessionKey -> *sync.Mutex
	sessions    sync.Map       // sessionKey -> context.CancelFunc (in-flight turn)
	wg          sync.WaitGroup // tracks the auto-title workers (D-A5-01); Stop joins it (goleak-clean)
	// stopMu guards (re)arming the SINGLE wg-drain waiter that closes stopDone. While a
	// title worker runs, stopDone stays non-nil so repeated Stop reuses ONE waiter — a hung
	// worker leaves exactly one blocked waiter no matter how often Stop is called
	// (D-14/LOOP-11/F-045). On a clean drain the waiter resets stopDone to nil so a LATER
	// Stop re-arms a fresh waiter and actually joins a title worker spawned after that drain
	// (WR-02: the pre-fix one-shot sync.Once closed stopDone permanently, so every later
	// Stop returned "drained" while a post-drain worker was still in flight).
	stopMu   sync.Mutex
	stopDone chan struct{}
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
	// The workspace announced to the model (#52/D-41) is where its shell starts:
	// shell_exec with an empty WorkspaceRoot runs in the Aura process's cwd, so the
	// hint mirrors exactly that (Claude-Code parity: the harness tells the model its
	// working directory). Best-effort — "" omits the hint, never fatal.
	workspace := d.Workspace
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = filepath.ToSlash(wd)
		}
	}
	// Resolve the compaction summarizer model once: an empty knob falls back to the
	// main chat model, mirroring the llm.Config secondary-model convention.
	compactionModel := d.CompactionModel
	if compactionModel == "" {
		compactionModel = d.LLM.Model
	}
	// Build the static curated-seed reasoning classifier once so its anchors are
	// amortized across every turn.
	classifier := prompt.NewReasoningClassifier(d.Embedder)
	// Wire the SAME embedder into the tool_search ranker (08.2-03): free-text
	// tool_search ranks deferred tools by embedding cosine, so the embed sidecar is a
	// HARD dependency for tool_search (Req-6). The reasoning classifier keeps its SOFT
	// LLM-router fallback (risk #9) — the hard-dep is tool_search-only. A non-fatal
	// boot health-check logs an unreachable sidecar but never fails boot, so an
	// MCP-free `aura chat` is not coupled to embed availability (Open-Q #2).
	r := &Runner{
		Conv:                     d.Conv,
		pause:                    d.Pause,
		identity:                 d.Identity,
		cacheMetrics:             d.CacheMetrics,
		toolInvocations:          d.ToolInvocations,
		memoryContext:            d.MemoryContext,
		client:                   d.Client,
		registry:                 d.Registry,
		cfg:                      d.LLM,
		runDir:                   d.RunDir,
		previewCap:               d.PreviewCap,
		historyCap:               d.HistoryCap,
		evictAfter:               d.EvictAfter,
		compactionEnabled:        d.CompactionEnabled,
		compactionModel:          compactionModel,
		memoryPreloadEnabled:     d.MemoryPreloadEnabled,
		workspace:                workspace,
		reasoningPersistMaxRunes: d.ReasoningPersistMaxRunes,
		gatewayOwnsToolStarts:    d.Gateway.OwnsToolStartRows(),
		titleTimeout:             titleTimeout,
		stopTimeout:              stopTimeout,
		resumeHook:               d.ResumeHook,
		hookManager:              d.HookManager,
		verificationStore:        d.VerificationStore,
		verificationDetector:     d.VerificationDetector,
		alwaysBlock:              d.AlwaysBlock,
		classifier:               classifier,
		breaker:                  d.Breaker,
		gateway:                  d.Gateway,
		shareRevoker:             d.ShareRevoker,
		resumeCommitter:          d.ResumeCommitter,
		// stopDone starts nil: the first waitWorkers arms the wg-drain waiter, and each
		// clean drain resets it to nil so a later Stop re-arms (WR-02).
	}
	// Default to the pool-less split committer when the composition root injected none
	// (D-03): pool-owning callers pass a *PoolResumeCommitter for atomic cross-store
	// resume/pause; unit tests + cache_audit get the non-atomic fallback for free.
	if r.resumeCommitter == nil {
		r.resumeCommitter = newSplitResumeCommitter(d.Conv, d.Pause)
	}
	// One process-lifetime breaker shared by every per-turn agent (B-05). A provider
	// outage trips it once and short-circuits subsequent turns until cooldown — the
	// per-turn breaker reset on each rebuild and never opened across turns.
	if r.breaker == nil {
		r.breaker = llm.NewDefaultBreaker()
	}
	return r
}

// ResponseInput is the CLI/caller-facing resume payload (the MCP three-action model,
// D-A3-01 / AM-02). It maps to askuser.ResumeAnswer at the Store boundary.
type ResponseInput struct {
	Action  string // accept | decline | cancel
	Content string
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
	return r.runTurn(ctx, convID, turnInput{visibleUserMsg: userMsg, modelUserMsg: userMsg})
}

// TurnBranch is the D-09 / CHAT-05 re-run-from-a-point primitive: it drives a fresh
// agent round over the SELECTED branch path (leafSeq) instead of the full linear
// history, with no fresh user message (continue-after-resume semantics, userMsg=nil) —
// the new branch's turns were already persisted by ForkBranch. The messages[0] head is
// byte-identical to the linear case (LoadManagedHistoryForBranch preserves the protected
// head; only body turns differ per branch — the CAP-04 cache invariant). leafSeq <= 0
// falls back to the canonical branch leaf (the same history Turn would load).
func (r *Runner) TurnBranch(ctx context.Context, convID string, leafSeq int) iter.Seq2[*agent.Event, error] {
	return r.runTurn(ctx, convID, turnInput{branchLeaf: leafSeq})
}

// runTurn dispatches a turn under the per-conversation lock (or directly when the lock
// is already held). branchLeaf > 0 selects the path-aware branch history loader; 0 is
// the linear full-history default Turn uses.
func (r *Runner) runTurn(ctx context.Context, convID string, input turnInput) iter.Seq2[*agent.Event, error] {
	if threadLockHeld(ctx) {
		return r.turnLocked(ctx, convID, input)
	}
	return func(yield func(*agent.Event, error) bool) {
		mu := r.lockForThread(ctx, convID)
		mu.Lock()
		defer mu.Unlock()
		for ev, err := range r.turnLocked(WithThreadLockHeld(ctx), convID, input) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

// The per-conversation run lock (TryLockThread / lockForThread) and the live-turn cancel
// registry (trackSession / cancelSession) live in runner_session.go — both keyed by the
// composite (identity, session) sessionKey (D-23).

func (r *Runner) turnLocked(ctx context.Context, convID string, input turnInput) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		scopedCtx, err := r.scopeContextToConversation(ctx, convID)
		if err != nil {
			yield(nil, err)
			return
		}
		ctx = scopedCtx
		requestID, err := uuid.NewV7()
		if err != nil {
			yield(nil, fmt.Errorf("mint request id: %w", err))
			return
		}
		ctx = tools.WithRequestID(ctx, requestID.String())

		// Persist the new user turn (if any) BEFORE rehydrating so the agent sees it.
		if input.visibleUserMsg != nil {
			if err := r.appendUserTurn(ctx, convID, *input.visibleUserMsg); err != nil {
				yield(nil, err)
				return
			}
			if answer, ok := fastReplyFor(*input.visibleUserMsg); ok {
				ev := fastReplyEvent(convID, requestID, answer)
				tr := &turnTracker{convID: convID}
				if err := r.persistEvent(ctx, tr, ev); err != nil {
					yield(nil, err)
					return
				}
				if !yield(fastReplyChunkEvent(ev), nil) {
					return
				}
				yield(ev, nil)
				return
			}
		}

		cfg := r.contextConfig(r.loadMemoryContext(ctx, input.visibleUserMsg))
		history, err := r.loadTurnHistory(ctx, convID, cfg, input.branchLeaf)
		if err != nil {
			yield(nil, err)
			return
		}
		agentHistory := currentRoundModelHistory(history, input.visibleUserMsg, input.modelUserMsg)

		la, ic, cancelAgent, err := r.buildAgent(
			ctx, convID, requestID, agentHistory,
		)
		if err != nil {
			yield(nil, err)
			return
		}
		defer cancelAgent()
		// Register the live turn's ctx-cancel under the (identity, session) key so a
		// concurrent conversation-delete can abort THIS owner's in-flight turn (MUSR-05
		// step 1 / D-23). ctx is owner-scoped here (scopeContextToConversation set it), so
		// the key matches the one the delete lifecycle derives from the resolved owner id.
		untrackSession := r.trackSession(ctx, convID, cancelAgent)
		defer untrackSession()

		// Drive one fresh agent round, persisting each emitted turn and observing the
		// pause Event(s). tracker accumulates what to persist + whether we paused; on a
		// pause it also accumulates the round's ask_user calls so flushPause can write
		// them as ONE assistant turn (CR-02). The flush is deferred to round end
		// because the agent emits one pause Event per call but rewrites its history to
		// a single multi-tool_call assistant message.
		tr := &turnTracker{convID: convID}
		// flushPause writes the single combined assistant ask_user tool_call turn (CR-02)
		// — the message the injected RoleTool answers attach to on resume. It MUST run
		// even when the consumer stops iterating ON the pause Event: the AG-UI translator
		// returns on the interrupt outcome, so `yield` below returns false and the loop
		// early-returns. A plain post-loop call is then SKIPPED, leaving resume history
		// with orphaned tool answers (a tool result with no matching assistant tool_call)
		// → the model can't tell the pause was answered and re-asks ask_user every resume
		// → an infinite pause loop (live-found via Telegram). Deferring guarantees the turn
		// is persisted on every return path; WithoutCancel so a /cancel of the turn ctx
		// cannot abort the durable write the resume (and the cancel path) depend on.
		flushed := false
		flushOnce := func() error {
			if flushed {
				return nil
			}
			flushed = true
			return r.flushPause(context.WithoutCancel(ctx), tr)
		}
		// roundErr carries WHY the round stopped to the deferred recorder below. The
		// error is yielded to the consumer, which for an HTTP run is a stream that may
		// already be gone; the conversation keeps no copy unless it is written here.
		var roundErr error
		defer func() {
			if err := flushOnce(); err != nil {
				slog.Error("runner: flush pause assistant turn failed; resume history may be malformed",
					"conv", convID, "err", err)
			}
			if err := r.recordInterruptedRound(ctx, tr, roundErr); err != nil {
				slog.Error("runner: could not record an interrupted round; the question stays unanswered with nothing to say so",
					"conv", convID, "err", err)
			}
		}()
		for ev, runErr := range la.Run(ic) {
			if runErr != nil {
				roundErr = runErr
				yield(nil, runErr)
				return
			}
			if perr := r.persistEvent(ctx, tr, ev); perr != nil {
				roundErr = perr
				yield(nil, perr)
				return
			}
			if !yield(ev, nil) {
				return // consumer stopped — iter.Seq2 contract forbids a further yield
			}
		}

		// Natural completion (the consumer drained the pause too — the CLI path): flush
		// here so a persist error surfaces on the iter.Seq2 error slot. The deferred flush
		// (above) covers the early-return path where yielding is forbidden; flushed dedups.
		if err := flushOnce(); err != nil {
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

func (r *Runner) scopeContextToConversation(ctx context.Context, convID string) (context.Context, error) {
	conv, err := r.Conv.Get(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("scope conversation identity: %w", err)
	}
	if current := identityctx.IdentityID(ctx); current != "" {
		if current != conv.IdentityID {
			return nil, fmt.Errorf("conversation identity mismatch: context %s does not own conversation %s", current, convID)
		}
		return ctx, nil
	}
	return identityctx.WithIdentityID(ctx, conv.IdentityID), nil
}

// appendUserTurn persists the user message as the next turn.
func (r *Runner) appendUserTurn(ctx context.Context, convID, content string) error {
	return r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Role: llm.RoleUser, Content: content,
	})
}

// buildAgent constructs a FRESH LlmAgent seeded with the rehydrated history
// (D-A1-05 / Pattern-4) and its InvocationContext (a per-turn Budget from the
// AURA_LOOP_* env). The history is passed via LlmAgentConfig.UserTurns; the agent
// prepends its own byte-stable system message (AM-01: LoadHistory is the Store's,
// not the agent's). When the loaded history already carries a leading system turn
// (a persisted seq=1), it is dropped here so the agent's own system message is not
// duplicated.
func (r *Runner) buildAgent(ctx context.Context, convID string, requestID uuid.UUID, history []llm.Message) (*agent.LlmAgent, agent.InvocationContext, context.CancelFunc, error) {
	bud, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		return nil, agent.InvocationContext{}, nil, fmt.Errorf("budget config (check AURA_LOOP_* env): %w", err)
	}
	boundedCtx, cancel := bud.WithDeadline(ctx)
	// The runner is the INTERACTIVE composition root: mark the turn ctx with a live
	// responder so a strict-profile mutating approval routes to an in-session pause here
	// (D-03), never to the headless deny-with-guidance branch cron/swarm take.
	boundedCtx = gateway.WithResponder(boundedCtx)
	seed := stripLeadingSystem(history)
	// Read the FIXED per-turn reasoning-effort override the run handler put on ctx
	// (37E). Absent => zero effort => the agent's adaptive path runs unchanged (D-04);
	// a fixed level bypasses the classifier and forces req.Reasoning (D-08).
	reasoningEffort, _ := reasoningOverride(ctx)
	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     r.client,
		LLM:        r.cfg,
		Registry:   r.registry,
		PreviewCap: r.previewCap,
		RunDir:     r.runDir,
		SessionID:  convID, // session_id == conversation_id (D-26)
		Workspace:  r.workspace,
		UserTurns:  seed,
		Classifier: r.classifier, // shared, anchors built once
		Breaker:    r.breaker,    // shared process-lifetime breaker (B-05)
		// Both halves of the verification ledger are per turn because both are scoped to
		// (identity, session); the pool-owning store behind them is per process.
		HookManager:       r.verificationHooks(ctx, convID),
		Ledger:            r.verificationLedger(ctx),
		Gateway:           r.gateway,       // Phase-35 PEP; LedgerConversationID defaults to convID (UUID)
		ReasoningOverride: reasoningEffort, // 37E fixed effort; "" => auto (adaptive path)
	})
	ic := agent.InvocationContext{
		Ctx:       boundedCtx,
		Agent:     la,
		RequestID: requestID,
		Branch:    "root",
		Budget:    bud,
	}
	return la, ic, cancel, nil
}
