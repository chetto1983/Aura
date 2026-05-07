package orchestration

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrHiddenTool = errors.New("tool is not exposed in the active tool profile")

type TraceEvent struct {
	PromptVersion          string
	PromptHash             string
	PromptModules          []string
	ToolProfile            string
	ProfileSelectReason    string
	ToolsExposed           []string
	ToolsCalled            []string
	ToolName               string
	HiddenToolRejected     bool
	SkillReads             int
	SwarmUsed              bool
	SandboxUsed            bool
	TokensPrompt           int
	TokensCompletion       int
	TokensTotal            int
	EstimatedContextTokens int
	CostUSD                float64
	LatencyMS              int64
	LLMCalls               int
	ToolCalls              int
	Metadata               map[string]string
}

type Hooks interface {
	BeforeProfileSelect(TraceEvent)
	AfterProfileSelect(TraceEvent)
	BeforePromptCompose(TraceEvent)
	AfterPromptCompose(TraceEvent)
	BeforeExposeTools(TraceEvent)
	BeforeToolCall(TraceEvent) error
	AfterToolCall(TraceEvent)
	AfterTurn(TraceEvent)
}

type DefaultHooks struct{}

func EnsureHooks(h Hooks) Hooks {
	if h == nil {
		return DefaultHooks{}
	}
	return h
}

func (DefaultHooks) BeforeProfileSelect(TraceEvent) {}
func (DefaultHooks) AfterProfileSelect(TraceEvent)  {}
func (DefaultHooks) BeforePromptCompose(TraceEvent) {}
func (DefaultHooks) AfterPromptCompose(TraceEvent)  {}
func (DefaultHooks) BeforeExposeTools(TraceEvent)   {}
func (DefaultHooks) AfterToolCall(TraceEvent)       {}
func (DefaultHooks) AfterTurn(TraceEvent)           {}

func (DefaultHooks) BeforeToolCall(event TraceEvent) error {
	if strings.TrimSpace(event.ToolName) == "" {
		return nil
	}
	for _, exposed := range event.ToolsExposed {
		if exposed == event.ToolName {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrHiddenTool, event.ToolName)
}

func (event TraceEvent) RedactedSummary() map[string]string {
	out := map[string]string{
		"prompt_version": event.PromptVersion,
		"prompt_hash":    event.PromptHash,
		"tool_profile":   event.ToolProfile,
	}
	if event.ProfileSelectReason != "" {
		out["profile_select_reason"] = event.ProfileSelectReason
	}
	for key, value := range event.Metadata {
		out[key] = RedactTraceValue(value)
	}
	return out
}

var traceRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(sk-[A-Za-z0-9._-]+)`),
	regexp.MustCompile(`(?i)(api-[A-Za-z0-9._-]+)`),
	regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9._-]+)`),
	regexp.MustCompile(`(?i)((?:LLM_API_KEY|EMBEDDING_API_KEY|TELEGRAM_TOKEN|GARAGE_S3_SECRET_KEY)\s*=\s*)([^\s]+)`),
}

func RedactTraceValue(value string) string {
	redacted := value
	for _, re := range traceRedactors {
		redacted = re.ReplaceAllStringFunc(redacted, func(match string) string {
			parts := re.FindStringSubmatch(match)
			if len(parts) == 3 {
				return parts[1] + "[redacted]"
			}
			return "[redacted]"
		})
	}
	return redacted
}
