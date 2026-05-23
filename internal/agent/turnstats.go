package agent

import "github.com/aura/aura/internal/conversation"

// TurnStats aggregates per-turn counters for structured logging and snapshot storage.
// Channel-neutral: the Telegram channel populates channel-specific fields
// (PromptVersion, Toolset, ToolsExposed) before calling From to merge agent loop stats.
type TurnStats struct {
	LLMCalls                int
	ToolCalls               int
	LoopSteps               int
	PromptVersion           string
	PromptModules           []string
	PromptHash              string
	Toolset                 string
	ToolsetSelectReason     string
	ToolsExposed            []string
	ToolsCalled             []string
	ReadSkills              []string
	SkillsRead              bool
	SwarmUsed               bool
	SandboxUsed             bool
	TerminalTool            string
	DuplicateToolCall       bool
	TokensPrompt            int
	TokensCompletion        int
	TokensTotal             int
	CostUSD                 float64
	// TurnActions records per-tool-call details for the "## Already done this
	// turn" block (US-OUT-04).
	TurnActions []conversation.TaskEntry
}

// From populates agent-loop–tracked fields from stats, preserving caller-set metadata
// fields (PromptVersion, Toolset, ToolsExposed, etc.) already set on s.
func (s TurnStats) From(stats Stats) TurnStats {
	s.LLMCalls = stats.LLMCalls
	s.ToolCalls = stats.ToolCalls
	s.LoopSteps = stats.LoopSteps
	s.ToolsCalled = append([]string(nil), stats.ToolsCalled...)
	s.ReadSkills = append([]string(nil), stats.ReadSkills...)
	s.SkillsRead = stats.SkillsRead
	s.SwarmUsed = stats.SwarmUsed
	s.SandboxUsed = stats.SandboxUsed
	s.TerminalTool = stats.TerminalTool
	s.DuplicateToolCall = stats.DuplicateToolCall
	s.TokensPrompt = stats.TokensPrompt
	s.TokensCompletion = stats.TokensCompletion
	s.TokensTotal = stats.TokensTotal
	s.CostUSD = stats.CostUSD
	s.TurnActions = append([]conversation.TaskEntry(nil), stats.TurnActions...)
	return s
}

// ApplyTo writes accumulated turn counters back into agent.Stats after
// a terminal-tool finalizer mutates the turn counters.
func (s TurnStats) ApplyTo(stats *Stats) {
	stats.LLMCalls = s.LLMCalls
	stats.LoopSteps = s.LoopSteps
	stats.TokensPrompt = s.TokensPrompt
	stats.TokensCompletion = s.TokensCompletion
	stats.TokensTotal = s.TokensTotal
	stats.CostUSD = s.CostUSD
}
