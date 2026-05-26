package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
)

const (
	promptHealthCaseName     = "prompt-health"
	promptHealthArtifactPath = ".planning/post-drift-2026-05-21/Phase-PROMPT-CTX/prompt-health-baseline.json"
)

var promptHealthViews = []string{
	"prompt_health_turns_24h",
	"prompt_health_rollup_24h",
	"prompt_health_tool_outcomes_24h",
	"prompt_health_memory_kinds",
	"prompt_health_run_events_24h",
}

type PromptHealthReport struct {
	GeneratedAt  string                `json:"generated_at"`
	WindowHours  int                   `json:"window_hours"`
	ArtifactPath string                `json:"artifact_path,omitempty"`
	Prompt       PromptHealthSnapshot  `json:"prompt"`
	DB           PromptHealthDB        `json:"db"`
	Redaction    PromptHealthRedaction `json:"redaction"`
	Warnings     []string              `json:"warnings,omitempty"`
}

type PromptHealthSnapshot struct {
	Version                 string   `json:"version"`
	Hash                    string   `json:"hash"`
	StableHash              string   `json:"stable_hash"`
	RuntimeCapsuleHash      string   `json:"runtime_capsule_hash"`
	TotalChars              int      `json:"total_chars"`
	StableChars             int      `json:"stable_chars"`
	RuntimeCapsuleChars     int      `json:"runtime_capsule_chars"`
	Modules                 []string `json:"modules"`
	SectionAnchors          []string `json:"section_anchors"`
	LegacyOverlayReferences []string `json:"legacy_overlay_references,omitempty"`
}

type PromptHealthDB struct {
	ViewsPresent bool                      `json:"views_present"`
	Rollup       PromptHealthRollup        `json:"rollup"`
	Percentiles  PromptHealthPercentiles   `json:"percentiles"`
	Outliers     []PromptHealthTurn        `json:"outliers"`
	ToolOutcomes []PromptHealthToolBucket  `json:"tool_outcomes"`
	MemoryKinds  []PromptHealthMemoryKind  `json:"memory_kinds"`
	RunEvents    []PromptHealthEventBucket `json:"run_events"`
}

type PromptHealthRollup struct {
	TurnRows                  int64 `json:"turn_rows"`
	AssistantTurns            int64 `json:"assistant_turns"`
	DistinctChats             int64 `json:"distinct_chats"`
	LLMCalls                  int64 `json:"llm_calls"`
	ToolCalls                 int64 `json:"tool_calls"`
	Compactions               int64 `json:"compactions"`
	PromptTokens              int64 `json:"prompt_tokens"`
	CompletionTokens          int64 `json:"completion_tokens"`
	MaxPromptTokens           int64 `json:"max_prompt_tokens"`
	MaxLLMCalls               int64 `json:"max_llm_calls"`
	MaxToolCalls              int64 `json:"max_tool_calls"`
	MaxElapsedMS              int64 `json:"max_elapsed_ms"`
	MaxCompactionTokensBefore int64 `json:"max_compaction_tokens_before"`
	MaxCompactionTokensAfter  int64 `json:"max_compaction_tokens_after"`
}

type PromptHealthPercentiles struct {
	PromptTokensP50 int64 `json:"prompt_tokens_p50"`
	PromptTokensP95 int64 `json:"prompt_tokens_p95"`
}

type PromptHealthTurn struct {
	ID             int64  `json:"id"`
	Channel        string `json:"channel"`
	ChatHash       string `json:"chat_hash"`
	TurnIndex      int64  `json:"turn_index"`
	Role           string `json:"role"`
	CreatedAt      string `json:"created_at"`
	LLMCalls       int64  `json:"llm_calls"`
	ToolCallsCount int64  `json:"tool_calls_count"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	TokensIn       int64  `json:"tokens_in"`
	TokensOut      int64  `json:"tokens_out"`
}

type PromptHealthToolBucket struct {
	ToolName     string `json:"tool_name"`
	Outcome      string `json:"outcome"`
	Class        string `json:"class"`
	Count        int64  `json:"count"`
	MaxElapsedMS int64  `json:"max_elapsed_ms"`
	LastSeen     string `json:"last_seen"`
}

type PromptHealthMemoryKind struct {
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Count       int64  `json:"count"`
	LastUpdated string `json:"last_updated"`
}

type PromptHealthEventBucket struct {
	Type      string `json:"type"`
	RunOrigin string `json:"run_origin"`
	Count     int64  `json:"count"`
	LastSeen  string `json:"last_seen"`
}

type PromptHealthRedaction struct {
	Passed             bool `json:"passed"`
	RawPrompt          bool `json:"raw_prompt"`
	RawConversation    bool `json:"raw_conversation"`
	RawToolArguments   bool `json:"raw_tool_arguments"`
	RawToolOutputs     bool `json:"raw_tool_outputs"`
	RawMemoryDocuments bool `json:"raw_memory_documents"`
}

func runPromptHealthCase(env *Env, asJSON bool) error {
	report, err := generatePromptHealthReport(env, time.Now().UTC())
	if err != nil {
		return err
	}
	report.ArtifactPath = promptHealthArtifactPath
	if err := writePromptHealthArtifact(report); err != nil {
		return err
	}
	printPromptHealthReport(report, asJSON)
	return nil
}

func generatePromptHealthReport(env *Env, now time.Time) (PromptHealthReport, error) {
	if env == nil || env.DB == nil {
		return PromptHealthReport{}, fmt.Errorf("prompt health requires DB")
	}
	report := PromptHealthReport{
		GeneratedAt: now.Format(time.RFC3339),
		WindowHours: 24,
		Prompt:      promptHealthSnapshot(now),
		Redaction: PromptHealthRedaction{
			Passed:             true,
			RawPrompt:          false,
			RawConversation:    false,
			RawToolArguments:   false,
			RawToolOutputs:     false,
			RawMemoryDocuments: false,
		},
	}
	viewsPresent, err := env.promptHealthViewsPresent()
	if err != nil {
		report.Warnings = append(report.Warnings, "view check failed: "+err.Error())
	}
	report.DB.ViewsPresent = viewsPresent
	if !viewsPresent {
		report.Warnings = append(report.Warnings, "prompt_health_* views missing; using raw-table fallback queries")
	}

	if report.DB.Rollup, err = env.promptHealthRollup(); err != nil {
		return PromptHealthReport{}, err
	}
	if report.DB.Percentiles, err = env.promptHealthPercentiles(); err != nil {
		return PromptHealthReport{}, err
	}
	if report.DB.Outliers, err = env.promptHealthOutliers(); err != nil {
		return PromptHealthReport{}, err
	}
	if report.DB.ToolOutcomes, err = env.promptHealthToolOutcomes(); err != nil {
		return PromptHealthReport{}, err
	}
	if report.DB.MemoryKinds, err = env.promptHealthMemoryKinds(); err != nil {
		return PromptHealthReport{}, err
	}
	if report.DB.RunEvents, err = env.promptHealthRunEvents(); err != nil {
		return PromptHealthReport{}, err
	}
	return report, nil
}

func promptHealthSnapshot(now time.Time) PromptHealthSnapshot {
	runtimeCapsule := conversation.RenderTurnRuntimeCapsule(now, time.Local)
	stable := conversation.DefaultSystemPrompt()
	plan := agent.ComposeAgentPrompt(nil, time.Local, "", "", "", "", "", now)
	return PromptHealthSnapshot{
		Version:                 plan.Version,
		Hash:                    plan.Hash,
		StableHash:              shortHash(stable),
		RuntimeCapsuleHash:      shortHash(runtimeCapsule),
		TotalChars:              len(plan.Content),
		StableChars:             len(stable),
		RuntimeCapsuleChars:     len(runtimeCapsule),
		Modules:                 append([]string(nil), plan.Modules...),
		SectionAnchors:          promptSectionAnchors(plan.Content),
		LegacyOverlayReferences: legacyOverlayReferences(plan.Content),
	}
}

func promptSectionAnchors(content string) []string {
	var anchors []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			anchors = append(anchors, line)
		}
	}
	return anchors
}

func legacyOverlayReferences(content string) []string {
	var found []string
	for _, marker := range []string{"AGENT.md", "USER.md", "SOUL.md", "TOOLS.md"} {
		if strings.Contains(content, marker) {
			found = append(found, marker)
		}
	}
	return found
}

func writePromptHealthArtifact(report PromptHealthReport) error {
	if err := os.MkdirAll(filepath.Dir(promptHealthArtifactPath), 0o755); err != nil {
		return fmt.Errorf("create prompt health artifact dir: %w", err)
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prompt health artifact: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(promptHealthArtifactPath, raw, 0o644); err != nil {
		return fmt.Errorf("write prompt health artifact: %w", err)
	}
	return nil
}

func printPromptHealthReport(report PromptHealthReport, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(report)
		return
	}
	fmt.Printf("prompt_health: generated_at=%s artifact=%s\n", report.GeneratedAt, report.ArtifactPath)
	fmt.Printf("prompt: version=%s hash=%s stable_hash=%s chars=%d modules=%s legacy_refs=%s\n",
		report.Prompt.Version,
		report.Prompt.Hash,
		report.Prompt.StableHash,
		report.Prompt.TotalChars,
		strings.Join(report.Prompt.Modules, ","),
		strings.Join(report.Prompt.LegacyOverlayReferences, ","),
	)
	fmt.Printf("db: views_present=%t turns=%d assistant_turns=%d chats=%d llm_calls=%d tool_calls=%d compactions=%d p50_prompt=%d p95_prompt=%d max_prompt=%d\n",
		report.DB.ViewsPresent,
		report.DB.Rollup.TurnRows,
		report.DB.Rollup.AssistantTurns,
		report.DB.Rollup.DistinctChats,
		report.DB.Rollup.LLMCalls,
		report.DB.Rollup.ToolCalls,
		report.DB.Rollup.Compactions,
		report.DB.Percentiles.PromptTokensP50,
		report.DB.Percentiles.PromptTokensP95,
		report.DB.Rollup.MaxPromptTokens,
	)
	if len(report.DB.ToolOutcomes) > 0 {
		fmt.Println("top_tool_outcomes:")
		for _, row := range report.DB.ToolOutcomes[:min(len(report.DB.ToolOutcomes), 8)] {
			fmt.Printf("  %s outcome=%s class=%s count=%d max_elapsed=%dms\n", row.ToolName, row.Outcome, row.Class, row.Count, row.MaxElapsedMS)
		}
	}
	if len(report.Warnings) > 0 {
		fmt.Println("warnings:")
		for _, warning := range report.Warnings {
			fmt.Println("  " + warning)
		}
	}
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
