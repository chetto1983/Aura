package agent

import (
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/agent/prompt"
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
	return &LlmAgent{
		name:         name,
		description:  desc,
		client:       cfg.Client,
		cfg:          cfg.LLM,
		registry:     cfg.Registry,
		activated:    make(map[string]struct{}),
		previewCap:   cfg.PreviewCap,
		runDir:       cfg.RunDir,
		sessionID:    cfg.SessionID,
		workspace:    cfg.Workspace,
		builder:      prompt.NewPromptBuilder(),
		hooks:        cfg.HookManager,
		gateway:      cfg.Gateway,
		ledgerConvID: ledgerConvID,
		history:      hist,
		breaker:      resolveBreaker(cfg),
		classifier:   resolveClassifier(cfg),
		sources:      display.NewRegistry(),
	}
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
