package runner

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
)

// runner_deps.go carries the Runner's constructor surface — Deps, ResumeHook, and
// New() — split out of runner.go on the 52-05 refactor-on-touch touch (600-LOC
// ceiling: runner.go was at 596/600 before this plan even added its own line,
// per 52-04-SUMMARY.md's own headroom note). Nothing here changes behavior; it is
// a pure file move. The Runner TYPE itself stays in runner.go beside Turn/runTurn,
// since New()'s only job is to build one.

// defaultTitleTimeout bounds the best-effort auto-title LLM call (D-A5-01); it
// outlives the turn ctx (WithoutCancel) but is never unbounded.
const defaultTitleTimeout = 30 * time.Second

// defaultStopTimeout bounds the Runner.Stop wg.Wait() drain so a hung title worker
// cannot wedge shutdown forever.
const defaultStopTimeout = 10 * time.Second

// Deps are the Runner's constructor inputs: the three consumer-side Stores (narrow
// interfaces, D-A2-02), the LLM client + tool registry the fresh per-round LlmAgent
// is built from (D-A1-05/Pattern-4), the llm.Config (model + L2 context window
// inputs), and the bounded worker timeouts.
type Deps struct {
	Conv            ConversationStore
	Pause           PauseStore
	ApprovalExpiry  ApprovalExpiryStore
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
	// Timezone is the DEPLOYMENT fallback zone; an identity's own profile wins. Empty means
	// the process zone. See internal/agent/tools/clock.go for why UTC is not neutral.
	Timezone string
	// Profiles serves the operator profile from Postgres (migration 0097): the clock zone
	// and the deterministic block that rides messages[1]. Optional -- without it the
	// deployment zone applies and no profile block is rendered.
	Profiles ProfileProvider
	LLM      llm.Config
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
	// Steer is the shared process-wide mid-turn steer inbox (amendment #132,
	// AURA_AGUI_RUN_STEER, D-01/D-12). The composition root constructs ONE
	// instance and injects it here AND into agui.Server (SetSteerInbox) so a
	// push and a drain never disagree about which queue they mean (T-52-31).
	// nil means the flag is off: buildAgent's steerInboxOrNil then leaves the
	// per-turn agent's drain a total no-op, exactly like a nil gateway means
	// Allow.
	Steer *steer.Inbox
}

// ResumeHook is called after a paused ask_user response is persisted and before
// the runner resumes the pending turn.
type ResumeHook func(ctx context.Context, pending askuser.Pending, resp ResponseInput) error

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
		approvalExpiry:           d.ApprovalExpiry,
		identity:                 d.Identity,
		cacheMetrics:             d.CacheMetrics,
		toolInvocations:          d.ToolInvocations,
		memoryContext:            d.MemoryContext,
		client:                   d.Client,
		registry:                 d.Registry,
		location:                 tools.LocationOrUTC(d.Timezone),
		profiles:                 d.Profiles,
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
		steer:                    d.Steer,
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
