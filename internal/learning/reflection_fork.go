package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

const KindReflection = "reflection"

const reflectionExtractionPrompt = `You are an extraction assistant. Read the conversation turn below and
emit STRICT JSON with four arrays:
  - observations: factual things you noticed about the user or context
  - patterns: recurring behaviors or preferences (not single events)
  - user_preferences: explicit or strongly-implied likes/dislikes
  - user_reflections: things the user said about themselves

Rules: JSON only. No prose. Empty arrays allowed. Max 3 items per array.
Each item <= 200 chars. Do NOT capture PII or third-party names.`

type ReflectionExtract struct {
	Observations    []string `json:"observations"`
	Patterns        []string `json:"patterns"`
	UserPreferences []string `json:"user_preferences"`
	UserReflections []string `json:"user_reflections"`
}

type ReflectionTurn struct {
	RunID         string
	ThreadID      string
	UserMessage   string
	AssistantText string
	ToolCalls     []ReflectionToolCall
}

type ReflectionToolCall struct {
	Name          string
	Success       bool
	ResultPreview string
}

type ReflectionHook struct {
	Client        llm.Client
	Model         string
	MaxPerSession int
	MinTurnTokens int

	sessionCount map[string]int
	mu           sync.Mutex
}

func (h *ReflectionHook) Apply(ctx context.Context, turn ReflectionTurn, store *memoryindex.Store) []error {
	if h == nil || h.Client == nil {
		return nil
	}
	if store == nil {
		return []error{fmt.Errorf("reflection_fork: memory store unavailable")}
	}
	maxPerSession := h.MaxPerSession
	if maxPerSession <= 0 {
		maxPerSession = 5
	}
	minTurnTokens := h.MinTurnTokens
	if minTurnTokens <= 0 {
		minTurnTokens = 200
	}
	if estimateReflectionTurnTokens(turn) < minTurnTokens {
		return nil
	}
	sessionKey := reflectionSessionKey(turn)
	h.mu.Lock()
	if h.sessionCount == nil {
		h.sessionCount = map[string]int{}
	}
	if h.sessionCount[sessionKey] >= maxPerSession {
		h.mu.Unlock()
		return nil
	}
	h.sessionCount[sessionKey]++
	h.mu.Unlock()

	resp, err := h.Client.Send(ctx, llm.Request{
		Model:       h.Model,
		Temperature: llm.Float64Ptr(0),
		Messages: []llm.Message{
			{Role: "system", Content: reflectionExtractionPrompt},
			{Role: "user", Content: renderReflectionTurn(turn)},
		},
	})
	if err != nil {
		return []error{fmt.Errorf("reflection_fork: extract: %w", err)}
	}
	extract, err := parseReflectionExtract(resp.Content)
	if err != nil {
		return []error{err}
	}
	if extract.empty() {
		return nil
	}
	if err := writeReflectionExtract(ctx, store, turn, extract); err != nil {
		return []error{err}
	}
	return nil
}

func parseReflectionExtract(raw string) (ReflectionExtract, error) {
	var extract ReflectionExtract
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &extract); err != nil {
		return ReflectionExtract{}, fmt.Errorf("reflection_fork: parse JSON: %w", err)
	}
	normalized, err := normalizeReflectionExtract(extract)
	if err != nil {
		return ReflectionExtract{}, err
	}
	return normalized, nil
}

func normalizeReflectionExtract(extract ReflectionExtract) (ReflectionExtract, error) {
	var err error
	if extract.Observations, err = normalizeReflectionItems("observations", extract.Observations); err != nil {
		return ReflectionExtract{}, err
	}
	if extract.Patterns, err = normalizeReflectionItems("patterns", extract.Patterns); err != nil {
		return ReflectionExtract{}, err
	}
	if extract.UserPreferences, err = normalizeReflectionItems("user_preferences", extract.UserPreferences); err != nil {
		return ReflectionExtract{}, err
	}
	if extract.UserReflections, err = normalizeReflectionItems("user_reflections", extract.UserReflections); err != nil {
		return ReflectionExtract{}, err
	}
	return extract, nil
}

func normalizeReflectionItems(field string, items []string) ([]string, error) {
	if len(items) > 3 {
		return nil, fmt.Errorf("reflection_fork: %s has %d items, max 3", field, len(items))
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len([]rune(item)) > 200 {
			return nil, fmt.Errorf("reflection_fork: %s item exceeds 200 chars", field)
		}
		out = append(out, item)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func (e ReflectionExtract) empty() bool {
	return len(e.Observations) == 0 &&
		len(e.Patterns) == 0 &&
		len(e.UserPreferences) == 0 &&
		len(e.UserReflections) == 0
}

func writeReflectionExtract(ctx context.Context, store *memoryindex.Store, turn ReflectionTurn, extract ReflectionExtract) error {
	bodyBytes, err := json.Marshal(extract)
	if err != nil {
		return fmt.Errorf("reflection_fork: marshal extract: %w", err)
	}
	id := reflectionDocumentID(turn, string(bodyBytes))
	doc := memoryindex.Document{
		ID:          id,
		Kind:        KindReflection,
		Title:       "Reflection: " + reflectionSessionKey(turn),
		Body:        string(bodyBytes),
		Handle:      id,
		Status:      "active",
		UpdatedAt:   time.Now().UTC(),
		ContentHash: memoryindex.ContentHash(KindReflection, string(bodyBytes), ""),
		Tags:        []string{"reflection"},
	}
	if err := store.Upsert(ctx, doc); err != nil {
		return fmt.Errorf("reflection_fork: persist: %w", err)
	}
	return nil
}

func reflectionDocumentID(turn ReflectionTurn, body string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		reflectionSessionKey(turn),
		strings.TrimSpace(turn.RunID),
		body,
	}, "\x00")))
	return "reflection:" + hex.EncodeToString(sum[:8])
}

func reflectionSessionKey(turn ReflectionTurn) string {
	if threadID := strings.TrimSpace(turn.ThreadID); threadID != "" {
		return threadID
	}
	if runID := strings.TrimSpace(turn.RunID); runID != "" {
		return runID
	}
	return "unknown"
}

func estimateReflectionTurnTokens(turn ReflectionTurn) int {
	var b strings.Builder
	b.WriteString(turn.UserMessage)
	b.WriteString(turn.AssistantText)
	for _, call := range turn.ToolCalls {
		b.WriteString(call.Name)
		b.WriteString(call.ResultPreview)
	}
	runes := len([]rune(b.String()))
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

func renderReflectionTurn(turn ReflectionTurn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "run_id: %s\n", strings.TrimSpace(turn.RunID))
	fmt.Fprintf(&b, "thread_id: %s\n\n", strings.TrimSpace(turn.ThreadID))
	fmt.Fprintf(&b, "user_message:\n%s\n\n", strings.TrimSpace(turn.UserMessage))
	fmt.Fprintf(&b, "assistant_text:\n%s\n\n", strings.TrimSpace(turn.AssistantText))
	if len(turn.ToolCalls) == 0 {
		b.WriteString("tool_calls: []\n")
		return b.String()
	}
	b.WriteString("tool_calls:\n")
	for _, call := range turn.ToolCalls {
		fmt.Fprintf(&b, "- name: %s\n  success: %t\n  result_preview: %s\n",
			strings.TrimSpace(call.Name),
			call.Success,
			strings.TrimSpace(call.ResultPreview),
		)
	}
	return b.String()
}
