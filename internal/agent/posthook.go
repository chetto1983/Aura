package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

const (
	DefaultPostTurnFailureThreshold = 2
	DefaultPostTurnRecentTurns      = 10
)

// PostTurnConfig wires optional post-turn hooks into one agent invocation.
// Hooks are intentionally best-effort: they may learn from the turn, but must
// never affect delivery of the final answer.
type PostTurnConfig struct {
	Hooks  []PostTurnHook
	Store  *memoryindex.Store
	Record TurnRecord
}

type PostTurnHook interface {
	Apply(ctx context.Context, turn TurnRecord, store *memoryindex.Store) []error
}

// TurnRecord is the privacy-preserving summary a post-turn hook receives.
// It carries stable IDs and tool metadata, not full tool arguments or outputs.
type TurnRecord struct {
	RunID                   string
	ThreadID                string
	UserMessage             string
	AssistantText           string
	ToolCalls               []TurnToolCall
	OperationalWrites       []Lesson
	PreviousTurnHadToolCall bool
	PreviousToolNames       []string
}

type TurnToolCall struct {
	ID            string
	Name          string
	Success       bool
	Elapsed       time.Duration
	ResultPreview string
}

// PostTurnFailureReader is implemented by attempt stores that can read recent
// failures from the current thread. It returns metadata rows only; argument
// values and raw outputs stay out of the learning hook.
type PostTurnFailureReader interface {
	RecentThreadFailures(ctx context.Context, threadID, runID string, limit int) ([]attempts.ToolAttempt, error)
}

// HeuristicPostTurnHook implements US-OP07. It writes operational lessons when
// a repeated failure pattern or explicit user-negative signal is backed by
// durable turn/tool evidence. Always-on since 2026-05-24 (the AURA_OP07_HEURISTIC_ENABLED
// flag was removed); a nil memory store is the only thing that disables it.
type HeuristicPostTurnHook struct {
	FailureReader  PostTurnFailureReader
	NFailThreshold int
	RecentTurns    int
	Logger         *slog.Logger
}

func NewHeuristicPostTurnConfig(store *memoryindex.Store, reader PostTurnFailureReader, threshold, recentTurns int, logger *slog.Logger, record TurnRecord) PostTurnConfig {
	if store == nil {
		return PostTurnConfig{}
	}
	return PostTurnConfig{
		Store:  store,
		Record: record,
		Hooks: []PostTurnHook{
			HeuristicPostTurnHook{
				FailureReader:  reader,
				NFailThreshold: threshold,
				RecentTurns:    recentTurns,
				Logger:         logger,
			},
		},
	}
}

type reflectionPostTurnHook struct {
	hook *learning.ReflectionHook
}

func NewReflectionPostTurnHook(client llm.Client, model string) PostTurnHook {
	if client == nil {
		return nil
	}
	return reflectionPostTurnHook{
		hook: &learning.ReflectionHook{
			Client: client,
			Model:  model,
		},
	}
}

func (h reflectionPostTurnHook) Apply(ctx context.Context, turn TurnRecord, store *memoryindex.Store) []error {
	if h.hook == nil {
		return nil
	}
	return h.hook.Apply(ctx, learning.ReflectionTurn{
		RunID:         turn.RunID,
		ThreadID:      turn.ThreadID,
		UserMessage:   turn.UserMessage,
		AssistantText: turn.AssistantText,
		ToolCalls:     reflectionToolCalls(turn.ToolCalls),
	}, store)
}

func reflectionToolCalls(calls []TurnToolCall) []learning.ReflectionToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]learning.ReflectionToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, learning.ReflectionToolCall{
			Name:          call.Name,
			Success:       call.Success,
			ResultPreview: call.ResultPreview,
		})
	}
	return out
}

type postTurnLessonPayload struct {
	Tool                 string   `json:"tool"`
	Cause                string   `json:"cause"`
	WouldHavePreventedBy string   `json:"would_have_prevented_by"`
	Scope                string   `json:"scope"`
	EvidenceRunIDs       []string `json:"evidence_run_ids"`
}

func (h HeuristicPostTurnHook) Apply(ctx context.Context, turn TurnRecord, store *memoryindex.Store) []error {
	if store == nil {
		return []error{fmt.Errorf("posthook: memory store unavailable")}
	}
	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}
	var errs []error
	written := map[string]struct{}{}
	threshold := h.NFailThreshold
	if threshold <= 0 {
		threshold = DefaultPostTurnFailureThreshold
	}
	recentTurns := h.RecentTurns
	if recentTurns <= 0 {
		recentTurns = DefaultPostTurnRecentTurns
	}

	if h.FailureReader != nil {
		failures, err := h.FailureReader.RecentThreadFailures(ctx, turn.ThreadID, turn.RunID, recentTurns)
		if err != nil {
			errs = append(errs, fmt.Errorf("posthook: read recent failures: %w", err))
		} else {
			for _, group := range groupedFailures(failures) {
				if len(group) < threshold {
					continue
				}
				first := group[0]
				if first.ToolName == "" || strings.TrimSpace(first.Class) == "" {
					continue
				}
				payload := postTurnLessonPayload{
					Tool:                 first.ToolName,
					Cause:                "error_class:" + first.Class,
					WouldHavePreventedBy: "Check recent failures for this tool and adjust the next call before retrying.",
					Scope:                "tool:" + first.ToolName,
					EvidenceRunIDs:       evidenceRunIDs(group),
				}
				signature := postTurnSignature("n_failure", first.ToolName, first.Class)
				if _, ok := written[signature]; ok {
					continue
				}
				if err := writePostTurnLesson(ctx, store, signature, first.ToolName, first.Class, payload); err != nil {
					errs = append(errs, err)
					continue
				}
				written[signature] = struct{}{}
				logger.Info("posthook: operational lesson written", "trigger", "n_failure", "tool", first.ToolName, "class", first.Class, "evidence_runs", len(payload.EvidenceRunIDs))
			}
		}
	}

	// 2026-05-25: removed the userNegativePattern regex (matched
	// "no|non|stop|smetti|sbagliato|wrong|fermati" at start of message).
	// Per feedback_no_regex_for_nlp: hardcoded NL keyword sets are the
	// same fragile anti-pattern as regex on prose — IT+EN coverage only,
	// brittle on paraphrase ("non è quello che volevo"), no signal on
	// sarcasm or implicit correction. The n_failure trigger above is
	// the structurally-grounded path for learning from user feedback
	// (operational lesson written when ≥ N tool calls in the same class
	// fail). Explicit user negation should re-enter this hook only when
	// surfaced via a structured channel (e.g. a thumbs-down reaction
	// emoji or a /stop slash command) — not by parsing message prose.

	return errs
}

func ApplyPostTurnHooks(ctx context.Context, hooks []PostTurnHook, turn TurnRecord, store *memoryindex.Store, logger *slog.Logger) []error {
	if len(hooks) == 0 || store == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	var errs []error
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					errs = append(errs, fmt.Errorf("posthook: panic recovered: %v", r))
				}
			}()
			errs = append(errs, hook.Apply(ctx, turn, store)...)
		}()
	}
	for _, err := range errs {
		logger.Warn("posthook: hook failed", "error", err)
	}
	return errs
}

func FirePostTurnHooks(ctx context.Context, cfg PostTurnConfig, logger *slog.Logger) {
	if len(cfg.Hooks) == 0 || cfg.Store == nil {
		return
	}
	go ApplyPostTurnHooks(ctx, cfg.Hooks, cfg.Record, cfg.Store, logger)
}

func postTurnRecordFromState(base TurnRecord, state State, fallbackRunID string) TurnRecord {
	turn := base
	if turn.RunID == "" {
		turn.RunID = fallbackRunID
	}
	if state == nil {
		return turn
	}
	messages := state.Messages()
	latestUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			latestUserIdx = i
			if strings.TrimSpace(turn.UserMessage) == "" {
				turn.UserMessage = messages[i].Content
			}
			break
		}
	}
	if latestUserIdx <= 0 || turn.PreviousTurnHadToolCall {
		turn.OperationalWrites = append(turn.OperationalWrites, operationalWritesFromMessages(messages, latestUserIdx)...)
		return turn
	}
	turn.OperationalWrites = append(turn.OperationalWrites, operationalWritesFromMessages(messages, latestUserIdx)...)
	seen := map[string]struct{}{}
	for i := latestUserIdx - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			break
		}
		if messages[i].Role != "assistant" || len(messages[i].ToolCalls) == 0 {
			continue
		}
		turn.PreviousTurnHadToolCall = true
		for _, call := range messages[i].ToolCalls {
			name := strings.TrimSpace(call.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			turn.PreviousToolNames = append(turn.PreviousToolNames, name)
		}
	}
	return turn
}

func operationalWritesFromMessages(messages []llm.Message, latestUserIdx int) []Lesson {
	if latestUserIdx < 0 || latestUserIdx >= len(messages) {
		return nil
	}
	acceptedResults := map[string]struct{}{}
	for i := latestUserIdx + 1; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}
		if strings.Contains(msg.Content, "accepted and stored immediately") {
			acceptedResults[msg.ToolCallID] = struct{}{}
		}
	}
	if len(acceptedResults) == 0 {
		return nil
	}
	var out []Lesson
	seen := map[string]struct{}{}
	for i := latestUserIdx + 1; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.Name != "propose_patch" {
				continue
			}
			if _, ok := acceptedResults[call.ID]; !ok {
				continue
			}
			if strings.TrimSpace(stringToolArg(call.Arguments, "action")) != "operational" {
				continue
			}
			toolName := stringToolArg(call.Arguments, "tool_name")
			errorClass := stringToolArg(call.Arguments, "error_class")
			text := stringToolArg(call.Arguments, "lesson")
			if toolName == "" || errorClass == "" || text == "" {
				continue
			}
			id := operationalLessonID(toolName, errorClass, text)
			if _, ok := seen[id]; ok {
				continue
			}
			priority := memoryindex.NormalizePriority(stringToolArg(call.Arguments, "priority"))
			out = append(out, Lesson{
				ID:         id,
				ToolName:   toolName,
				ErrorClass: errorClass,
				Text:       text,
				Priority:   priority,
			})
			seen[id] = struct{}{}
		}
	}
	return out
}

func stringToolArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch v := args[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func operationalLessonID(toolName, errorClass, lesson string) string {
	return "operational:" + postTurnSignature("operational", toolName, errorClass, lesson)
}

func groupedFailures(failures []attempts.ToolAttempt) [][]attempts.ToolAttempt {
	groupsByKey := map[string][]attempts.ToolAttempt{}
	keys := make([]string, 0)
	for _, failure := range failures {
		tool := strings.TrimSpace(failure.ToolName)
		class := strings.TrimSpace(failure.Class)
		if tool == "" || class == "" {
			continue
		}
		key := tool + "\x00" + class
		if _, ok := groupsByKey[key]; !ok {
			keys = append(keys, key)
		}
		groupsByKey[key] = append(groupsByKey[key], failure)
	}
	sort.Strings(keys)
	out := make([][]attempts.ToolAttempt, 0, len(keys))
	for _, key := range keys {
		out = append(out, groupsByKey[key])
	}
	return out
}

func evidenceRunIDs(group []attempts.ToolAttempt) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(group))
	for _, attempt := range group {
		runID := strings.TrimSpace(attempt.RunID)
		if runID == "" {
			continue
		}
		if _, ok := seen[runID]; ok {
			continue
		}
		seen[runID] = struct{}{}
		out = append(out, runID)
	}
	sort.Strings(out)
	return out
}

func writePostTurnLesson(ctx context.Context, store *memoryindex.Store, signature, toolName, errorClass string, payload postTurnLessonPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("posthook: marshal lesson payload: %w", err)
	}
	return learning.WriteApprovedLesson(ctx, store, learning.Proposal{
		Kind:          "operational_memory",
		Fact:          string(body),
		SignatureHash: signature,
		ToolName:      toolName,
		ErrorClass:    errorClass,
	})
}

func postTurnSignature(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}
