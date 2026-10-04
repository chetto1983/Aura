package agent

import (
	"time"

	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
)

// NewLlmAgent builds an LlmAgent. messages[0] is ALWAYS the byte-stable system
// prompt (D-08/D-09) followed by the supplied user turns; the agent owns history
// from here. Name/Description default when empty.
func NewLlmAgent(cfg LlmAgentConfig) *LlmAgent {
	hist := make([]llm.Message, 0, len(cfg.UserTurns)+1)
	hist = append(hist, llm.Message{Role: llm.RoleSystem, Content: systemMessage()})
	hist = append(hist, cfg.UserTurns...)

	name := cfg.Name
	if name == "" {
		name = "aura"
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Aura's autonomous tool-dispatch agent"
	}
	// The gateway ledger key defaults to sessionID (the main runner path, where
	// session_id == conversation_id UUID); headless swarm/cron roots pass the
	// originating conversation UUID explicitly so a flat session never keys the ledger.
	ledgerConvID := cfg.LedgerConversationID
	if ledgerConvID == "" {
		ledgerConvID = cfg.SessionID
	}
	agent := &LlmAgent{
		name:              name,
		description:       desc,
		client:            cfg.Client,
		cfg:               cfg.LLM,
		registry:          cfg.Registry,
		activated:         deriveActivated(hist, cfg.Registry),
		everLoaded:        deriveEverLoaded(hist, cfg.Registry),
		previewCap:        cfg.PreviewCap,
		runDir:            cfg.RunDir,
		sessionID:         cfg.SessionID,
		workspace:         cfg.Workspace,
		location:          cfg.Location,
		builder:           prompt.NewPromptBuilder(),
		hooks:             cfg.HookManager,
		gateway:           cfg.Gateway,
		messageDrafts:     cfg.MessageDrafts,
		steer:             cfg.Steer,
		ledger:            cfg.Ledger,
		ledgerConvID:      ledgerConvID,
		history:           hist,
		breaker:           resolveBreaker(cfg),
		classifier:        resolveClassifier(cfg),
		reasoningOverride: cfg.ReasoningOverride,
		sources:           display.NewRegistry(),
		background:        cfg.BackgroundCalls,
	}
	return agent
}

// resolveBreaker returns the injected SHARED breaker (B-05: the Runner's
// process-lifetime singleton) or a fresh per-agent breaker with the default policy
// when none is wired (tests/standalone construction).
func resolveBreaker(cfg LlmAgentConfig) *llm.Breaker {
	if cfg.Breaker != nil {
		return cfg.Breaker
	}
	return llm.NewDefaultBreaker()
}

// Name is the Event Author / FindAgent key.
func (a *LlmAgent) Name() string { return a.name }

// Description is the human/LLM-facing one-liner.
func (a *LlmAgent) Description() string { return a.description }

// OwnsBudget tells workflow parents that LlmAgent already consumes the shared
// Budget at its own loop gates, so wrappers must not charge its emitted tool-call
// events a second time.
func (*LlmAgent) OwnsBudget() bool { return true }

// SubAgents returns nil — LlmAgent is a leaf.
func (a *LlmAgent) SubAgents() []Agent { return nil }

// FindAgent returns self when name matches, else nil.
func (a *LlmAgent) FindAgent(name string) Agent {
	if a.name == name {
		return a
	}
	return nil
}

// LlmAgentConfig carries the LlmAgent constructor inputs.
type LlmAgentConfig struct {
	Name        string
	Description string
	Client      llm.Client
	LLM         llm.Config
	Registry    *tools.Registry
	PreviewCap  int    // AURA_CONTEXT_PREVIEW_CAP_BYTES (config.ToolPreviewCap)
	RunDir      string // config.RunDir — sidecar root
	SessionID   string // Event.ThreadID; sidecar dir key (D-26)
	Workspace   string // the shell workspace path, rendered into the per-turn tail hint (#52/D-41); "" omits
	// Location renders every clock the model reads. Nil means UTC, which is what the tail
	// hint carried until 2026-08-16 -- and a UTC clock is an arithmetic problem the model
	// answers wrongly (see internal/agent/tools/clock.go for the measurement).
	Location  *time.Location
	UserTurns []llm.Message
	// Classifier is the SHARED long-lived reasoning-tier classifier (anchors built
	// once, reused across turns). Production injects this via the Runner so the
	// static curated-anchor build is amortized rather than paid per turn.
	Classifier *prompt.ReasoningClassifier
	// Embedder is a convenience for tests/standalone construction:
	// when Classifier is nil and Embedder is set, NewLlmAgent builds a per-agent
	// classifier. Production leaves these unset and passes Classifier instead.
	Embedder prompt.Embedder
	// Breaker is the SHARED process-lifetime circuit breaker (B-05). The Runner owns
	// ONE breaker and injects it into every per-turn agent so a provider outage trips
	// cross-turn protection (a fresh per-agent breaker reset each turn and never
	// opened). nil => NewLlmAgent mints a fresh per-agent breaker (tests/standalone).
	Breaker *llm.Breaker
	// HookManager is the optional Phase-21 extension surface. nil is a no-op.
	HookManager *HookManager
	// Gateway is the optional Phase-35 policy PEP (GATE-01). nil is an Allow no-op
	// (dev-parity). The composition roots inject the one process-wide *gateway.Gateway.
	Gateway       *gateway.Gateway
	MessageDrafts MessageDraftCreator
	Steer         SteerInbox // optional mid-turn redirect inbox; nil means drain is a no-op
	// Ledger is the optional verification evidence ledger the verify-on-stop gate
	// reads. nil disables that gate (tests/standalone, and any deployment with no
	// Postgres pool behind NewEvidenceStore).
	Ledger VerificationLedger
	// LedgerConversationID is the ORIGINATING conversation UUID the gateway keys its
	// decision-fact ledger on. Empty defaults to SessionID in NewLlmAgent — correct for
	// the main runner path (session_id == conversation_id UUID); the headless swarm/cron
	// roots pass the originating conversation UUID so a flat session never keys the ledger.
	LedgerConversationID string
	// ReasoningOverride is the FIXED per-turn reasoning effort selected in the web
	// Composer (37E), threaded here by runner.buildAgent from runner.WithReasoningOverride
	// on ctx. When non-empty the agent BYPASSES the adaptive classifier and forces
	// req.Reasoning on a reasoning target (OpenRouter OR llama.cpp, D-08); empty is the
	// "auto" default, leaving today's adaptive path byte-identical (D-04, zero regression).
	ReasoningOverride llm.ReasoningEffort
	// BackgroundCalls moves a call still running after the budget's background window
	// out of the turn (prd.md §15). Only the interactive runner sets it: its turns belong
	// to a conversation the completion can wake. Nil keeps every call in its turn.
	BackgroundCalls *tools.BackgroundCalls
}
