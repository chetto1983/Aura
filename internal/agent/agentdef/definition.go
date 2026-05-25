package agentdef

type AgentTier string

const (
	TierChat      AgentTier = "chat"
	TierReasoning AgentTier = "reasoning"
	TierWorker    AgentTier = "worker"
)

const (
	DefaultMaxIterations  = 20
	DefaultMaxResultChars = 24000
)

type ToolScope struct {
	Named []string `json:"named,omitempty"`
}

type SubagentRef struct {
	ID           string `json:"id"`
	DelegateName string `json:"delegate_name,omitempty"`
}

type AgentDefinition struct {
	ID           string        `json:"id"`
	DisplayName  string        `json:"display_name,omitempty"`
	WhenToUse    string        `json:"when_to_use,omitempty"`
	PromptPath   string        `json:"prompt_path,omitempty"`
	PromptInline string        `json:"prompt,omitempty"`
	ModelHint    string        `json:"model_hint,omitempty"`
	Temperature  *float64      `json:"temperature,omitempty"`
	Tools        ToolScope     `json:"tools,omitempty"`
	Subagents    []SubagentRef `json:"subagents,omitempty"`
	Tier         AgentTier     `json:"tier,omitempty"`

	MaxIterations   int `json:"max_iterations,omitempty"`
	MaxInputTokens  int `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	MaxResultChars  int `json:"max_result_chars,omitempty"`

	InheritUserMemory          bool `json:"inherit_user_memory,omitempty"`
	InheritOperationalMemory   bool `json:"inherit_operational_memory,omitempty"`
	InheritWikiContext         bool `json:"inherit_wiki_context,omitempty"`
	InheritConversationContext bool `json:"inherit_conversation_context,omitempty"`
	InheritIdentity            bool `json:"inherit_identity,omitempty"`
	InheritMemory              bool `json:"inherit_memory,omitempty"`
	InheritSafety              bool `json:"inherit_safety,omitempty"`
	InheritSkills              bool `json:"inherit_skills,omitempty"`
	InheritProfile             bool `json:"inherit_profile,omitempty"`

	Prompt string `json:"-"`
	Source string `json:"-"`
}

func cloneDefinition(def AgentDefinition) AgentDefinition {
	out := def
	out.Tools.Named = append([]string(nil), def.Tools.Named...)
	out.Subagents = append([]SubagentRef(nil), def.Subagents...)
	return out
}
