package runner

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoninglearn"
	"github.com/chetto1983/aura/internal/toolselectlearn"
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
	Client          llm.Client
	Registry        *tools.Registry
	LLM             llm.Config
	// Breaker is the SHARED process-lifetime LLM circuit breaker (B-05). The
	// composition root may inject one; nil => New mints the default. It is threaded
	// into every per-turn agent so a provider outage trips cross-turn protection.
	Breaker      *llm.Breaker
	RunDir       string
	PreviewCap   int
	EvictAfter   int    // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 eviction age
	Workspace    string // shell workspace announced per turn (#52/D-41); "" → the process cwd
	TitleTimeout time.Duration
	StopTimeout  time.Duration
	// AlwaysBlock renders the messages[1] always-on skill block per turn from current
	// loader state (D-07). The composition root wires it over skills.RenderAlwaysBlock
	// + the live loader; nil means no skills are wired (the block is empty). Rebuilt
	// every turn so a skill add/remove changes messages[1] without busting messages[0].
	ContextBlock ContextBlockProvider
	AlwaysBlock  func() string
	ResumeHook   ResumeHook
	// Embedder wires the local embedding-based reasoning-tier classifier into
	// each per-turn agent (replaces the LLM router round-trip). nil => the agent
	// falls back to the LLM router. The composition root passes the granite
	// sidecar client (documents.EmbeddingClient over Neo4j.EmbedURL).
	Embedder prompt.Embedder
	// ExampleStore folds oracle-labeled examples (Neo4j :ReasoningExample) into
	// the classifier's centroids (self-improvement, spike 053). nil => seed-only.
	ExampleStore prompt.ExampleStore
	// ReasoningSaver persists new oracle-labeled examples for the async learner
	// (the write half of self-improvement). Enabled only when LLM.ReasoningLearning
	// is set; nil => no learning. The composition root passes the same Neo4j store
	// as ExampleStore.
	ReasoningSaver reasoninglearn.Saver
	// HookManager is the optional agent extension surface. nil keeps the agent's
	// hook calls as no-ops; production may inject command hooks here.
	HookManager *agent.HookManager
	// ToolSelectSaver persists oracle-confirmed (query -> tool) examples for the
	// tool-selection active-learning loop (D-06/D-07) to :ToolSelectionExample. nil =>
	// the loop is off (the tool_search ranker runs without the learned stage-2 boost).
	// The composition root passes a *toolselectstore.Store over the same Neo4j client
	// as ReasoningSaver, when the graph client opened.
	ToolSelectSaver toolselectlearn.Saver
}

// toolSelectObserver is the narrow seam the runner holds for the tool-selection
// active-learning loop: the post-turn capture (Observe) + shutdown (Close). It lets a
// test inject a fake to prove the Open-Q #3 wiring (Observe IS called on a real turn),
// not just the synthetic exemplar unit tests. *toolselectlearn.Learner satisfies it.
type toolSelectObserver interface {
	Observe(request, usedTool string)
	Close()
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

	client     llm.Client
	registry   *tools.Registry
	cfg        llm.Config
	breaker    *llm.Breaker // SHARED process-lifetime LLM circuit breaker, injected into every per-turn agent (B-05)
	runDir     string
	previewCap int
	evictAfter int
	workspace  string // the shell workspace path the per-turn tail hint announces (#52/D-41)

	titleTimeout time.Duration
	stopTimeout  time.Duration
	resumeHook   ResumeHook

	contextBlock      ContextBlockProvider
	hookManager       *agent.HookManager          // optional per-turn LlmAgent hooks
	alwaysBlock       func() string               // renders the messages[1] always-block per turn (D-07); nil → empty
	classifier        *prompt.ReasoningClassifier // SHARED reasoning-tier classifier (anchors built once); nil → LLM router
	learner           *reasoninglearn.Learner     // async reasoning self-improvement worker; nil unless ReasoningLearning is on
	toolSelectLearner toolSelectObserver          // async tool-selection self-improvement worker (Open-Q #3); nil unless a saver is wired

	threadLocks sync.Map
	wg          sync.WaitGroup // tracks the auto-title workers (D-A5-01); Stop joins it (goleak-clean)
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
	// Build the reasoning-tier classifier ONCE here so its 18-seed anchors + Neo4j
	// example load are amortized across every turn, not rebuilt per turn.
	classifier := prompt.NewReasoningClassifier(d.Embedder, d.ExampleStore)
	// Wire the SAME granite embedder into the tool_search ranker (08.2-03): free-text
	// tool_search ranks deferred tools by embedding cosine, so the embed sidecar is a
	// HARD dependency for tool_search (Req-6). The reasoning classifier keeps its SOFT
	// LLM-router fallback (risk #9) — the hard-dep is tool_search-only. A non-fatal
	// boot health-check logs an unreachable sidecar but never fails boot, so an
	// MCP-free `aura chat` is not coupled to embed availability (Open-Q #2).
	wireToolSearchEmbedder(d.Registry, d.Embedder)
	r := &Runner{
		Conv:            d.Conv,
		pause:           d.Pause,
		identity:        d.Identity,
		cacheMetrics:    d.CacheMetrics,
		toolInvocations: d.ToolInvocations,
		client:          d.Client,
		registry:        d.Registry,
		cfg:             d.LLM,
		runDir:          d.RunDir,
		previewCap:      d.PreviewCap,
		evictAfter:      d.EvictAfter,
		workspace:       workspace,
		titleTimeout:    titleTimeout,
		stopTimeout:     stopTimeout,
		resumeHook:      d.ResumeHook,
		contextBlock:    d.ContextBlock,
		hookManager:     d.HookManager,
		alwaysBlock:     d.AlwaysBlock,
		classifier:      classifier,
		breaker:         d.Breaker,
	}
	// One process-lifetime breaker shared by every per-turn agent (B-05). A provider
	// outage trips it once and short-circuits subsequent turns until cooldown — the
	// per-turn breaker reset on each rebuild and never opened across turns.
	if r.breaker == nil {
		r.breaker = llm.NewDefaultBreaker()
	}
	// Attach the async self-improvement worker (no-op unless ReasoningLearning is
	// enabled and a save-capable store is wired).
	r.learner = buildReasoningLearner(d, classifier)
	// Attach the tool-selection active-learning loop (D-06/D-07): the detector + the
	// two-tier DeepSeek oracle on the activelearn core, with Refresh re-folding the
	// per-tool centroids into the tool_search ranker. No-op unless a ToolSelectSaver is
	// wired. It reuses the SAME granite embedder already wired into the ranker above.
	// Assign through a typed nil-guard so a nil *Learner does not become a non-nil
	// interface (which would NPE the Observe/Close paths).
	if tsl := buildToolSelectLearner(d); tsl != nil {
		r.toolSelectLearner = tsl
	}
	return r
}

// embedHealthCheckTimeout bounds the best-effort boot probe of the embed sidecar.
// It is short — the probe is a non-fatal liveness log, never a gate on boot.
const embedHealthCheckTimeout = 3 * time.Second

// wireToolSearchEmbedder wires the granite embedder into the registered tool_search
// hook so free-text tool_search ranks deferred tools by embedding cosine (08.2-03).
// The embed sidecar is a HARD dependency for tool_search: with it down, tool_search
// Execute returns an explicit model-visible error (Req-6) — but boot is NOT failed
// (Open-Q #2: an MCP-free `aura chat` must not be coupled to embed availability). A
// non-fatal health-check probes the sidecar once and logs an unreachable :8081 so the
// operator sees the dependency, then continues. nil embedder => tool_search has no
// ranker and its free-text path errors per call; the select: path still works.
func wireToolSearchEmbedder(reg *tools.Registry, embedder prompt.Embedder) {
	if reg == nil {
		return
	}
	t, ok := reg.Get("tool_search")
	if !ok {
		return
	}
	ts, ok := t.(*tools.ToolSearch)
	if !ok {
		return
	}
	ts.Embed = embedder
	if embedder == nil {
		slog.Warn("tool_search semantic ranking disabled: no embedder wired (embed sidecar)")
		return
	}
	// Boot health-check: probe the embed sidecar (granite :8081) once. Log-only —
	// never fatal, never blocks boot.
	ctx, cancel := context.WithTimeout(context.Background(), embedHealthCheckTimeout)
	defer cancel()
	if _, err := embedder.Embed(ctx, []string{"healthcheck"}); err != nil {
		slog.Warn("embed sidecar unreachable at boot: tool_search free-text ranking will error until it recovers (granite :8081)",
			"error", err)
	}
}

// CloseLearner stops the async self-improvement workers (reasoning + tool-selection),
// if any. The composition root calls it at process shutdown (nil-safe).
func (r *Runner) CloseLearner() {
	if r != nil {
		r.learner.Close()
		if r.toolSelectLearner != nil {
			r.toolSelectLearner.Close()
		}
	}
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
		mu := r.lockForThread(convID)
		mu.Lock()
		defer mu.Unlock()
		for ev, err := range r.turnLocked(WithThreadLockHeld(ctx), convID, input) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

// TryLockThread attempts to acquire the per-conversation run lock without
// blocking. HTTP transports use it to return 409 instead of queueing an SSE run
// behind an already-active request.
func (r *Runner) TryLockThread(convID string) (func(), bool) {
	mu := r.lockForThread(convID)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

func (r *Runner) lockForThread(convID string) *sync.Mutex {
	actual, _ := r.threadLocks.LoadOrStore(convID, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func (r *Runner) turnLocked(ctx context.Context, convID string, input turnInput) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		scopedCtx, err := r.scopeContextToConversation(ctx, convID)
		if err != nil {
			yield(nil, err)
			return
		}
		ctx = scopedCtx

		// Persist the new user turn (if any) BEFORE rehydrating so the agent sees it.
		if input.visibleUserMsg != nil {
			if err := r.appendUserTurn(ctx, convID, *input.visibleUserMsg); err != nil {
				yield(nil, err)
				return
			}
			if answer, ok := fastReplyFor(*input.visibleUserMsg); ok {
				ev, err := fastReplyEvent(convID, answer)
				if err != nil {
					yield(nil, err)
					return
				}
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

		cfg, err := r.contextConfig(ctx, convID)
		if err != nil {
			yield(nil, err)
			return
		}
		history, err := r.loadTurnHistory(ctx, convID, cfg, input.branchLeaf)
		if err != nil {
			yield(nil, err)
			return
		}
		agentHistory := currentRoundModelHistory(history, input.visibleUserMsg, input.modelUserMsg)

		la, ic, cancelAgent, err := r.buildAgent(ctx, convID, agentHistory)
		if err != nil {
			yield(nil, err)
			return
		}
		defer cancelAgent()

		// Drive one fresh agent round, persisting each emitted turn and observing the
		// pause Event(s). tracker accumulates what to persist + whether we paused; on a
		// pause it also accumulates the round's ask_user calls so flushPause can write
		// them as ONE assistant turn (CR-02). The flush is deferred to round end
		// because the agent emits one pause Event per call but rewrites its history to
		// a single multi-tool_call assistant message.
		tr := &turnTracker{convID: convID}
		if input.visibleUserMsg != nil {
			tr.userMsg = *input.visibleUserMsg // thread the round's request for the tool-select capture (Open-Q #3)
		}
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
		defer func() {
			if err := flushOnce(); err != nil {
				slog.Error("runner: flush pause assistant turn failed; resume history may be malformed",
					"conv", convID, "err", err)
			}
		}()
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

// loadTurnHistory rehydrates the turn history the agent runs over: the full linear
// history (branchLeaf <= 0, the default Turn path, byte-identical to the pre-D-09 case)
// or the selected branch path (branchLeaf > 0, the D-09 re-run). Both feed the SAME
// L1/L2/L2.5 ladder, so the protected messages[0] head is identical either way — only
// body turns differ per branch (the CAP-04 cache invariant). It does NOT re-implement
// the walk; it calls the store's path-aware loader (plan 25-06).
func (r *Runner) loadTurnHistory(ctx context.Context, convID string, cfg conversations.ContextConfig, branchLeaf int) ([]llm.Message, error) {
	if branchLeaf > 0 {
		return r.Conv.LoadManagedHistoryForBranch(ctx, convID, branchLeaf, cfg)
	}
	return r.Conv.LoadManagedHistory(ctx, convID, cfg)
}

// contextConfig builds the L1/L2/L2.5 ladder inputs from the Runner's llm.Config +
// eviction knob.
func (r *Runner) contextConfig(ctx context.Context, convID string) (conversations.ContextConfig, error) {
	block, err := r.renderContextBlock(ctx, convID)
	if err != nil {
		return conversations.ContextConfig{}, err
	}
	return conversations.ContextConfig{
		ContextWindow:       r.cfg.ContextWindow,
		MaxOutputTokens:     r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns: r.evictAfter,
		AlwaysBlock:         block,
	}, nil
}

func (r *Runner) renderContextBlock(ctx context.Context, convID string) (string, error) {
	var parts []string
	if r.contextBlock != nil {
		conv, err := r.Conv.Get(ctx, convID)
		if err != nil {
			return "", fmt.Errorf("context block: load conversation identity: %w", err)
		}
		owner, err := r.identity.GetIdentityByID(ctx, conv.IdentityID)
		if err != nil {
			return "", fmt.Errorf("context block: resolve identity %s: %w", conv.IdentityID, err)
		}
		if block := strings.TrimSpace(r.contextBlock(ctx, owner)); block != "" {
			parts = append(parts, block)
		}
	}
	if r.alwaysBlock != nil {
		if block := strings.TrimSpace(r.alwaysBlock()); block != "" {
			parts = append(parts, block)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// buildAgent constructs a FRESH LlmAgent seeded with the rehydrated history
// (D-A1-05 / Pattern-4) and its InvocationContext (a per-turn Budget from the
// AURA_LOOP_* env). The history is passed via LlmAgentConfig.UserTurns; the agent
// prepends its own byte-stable system message (AM-01: LoadHistory is the Store's,
// not the agent's). When the loaded history already carries a leading system turn
// (a persisted seq=1), it is dropped here so the agent's own system message is not
// duplicated.
func (r *Runner) buildAgent(ctx context.Context, convID string, history []llm.Message) (*agent.LlmAgent, agent.InvocationContext, context.CancelFunc, error) {
	requestID, err := uuid.NewV7()
	if err != nil {
		return nil, agent.InvocationContext{}, nil, fmt.Errorf("mint request id: %w", err)
	}
	bud, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		return nil, agent.InvocationContext{}, nil, fmt.Errorf("budget config (check AURA_LOOP_* env): %w", err)
	}
	boundedCtx, cancel := bud.WithDeadline(ctx)
	seed := stripLeadingSystem(history)
	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:      r.client,
		LLM:         r.cfg,
		Registry:    r.registry,
		PreviewCap:  r.previewCap,
		RunDir:      r.runDir,
		SessionID:   convID, // session_id == conversation_id (D-26)
		Workspace:   r.workspace,
		UserTurns:   seed,
		Classifier:  r.classifier, // shared, anchors built once
		Breaker:     r.breaker,    // shared process-lifetime breaker (B-05)
		HookManager: r.hookManager,
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
