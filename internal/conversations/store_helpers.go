package conversations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pgnumeric"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// conversationFromRow projects a generated conversations row onto the domain type,
// converting the pgtype wrappers (Pitfall 5) at the boundary: pgtype.Text title ->
// (TitleSet, Title), pgtype.Numeric total_cost_usd -> float64.
func conversationFromRow(r sqlc.AuraConversations) Conversation {
	c := Conversation{
		ID:                uuid.UUID(r.ID.Bytes).String(),
		IdentityID:        uuid.UUID(r.IdentityID.Bytes).String(),
		Status:            r.Status,
		Model:             r.Model,
		TotalInputTokens:  r.TotalInputTokens,
		TotalOutputTokens: r.TotalOutputTokens,
		TotalCachedTokens: r.TotalCachedTokens,
		TotalCostUSD:      pgnumeric.FloatFromNumeric(r.TotalCostUsd),
		SnapshotVersion:   r.SnapshotVersion,
	}
	if r.Title.Valid {
		c.Title = r.Title.String
		c.TitleSet = true
	}
	if r.CreatedAt.Valid {
		c.CreatedAt = r.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	c.ReasoningEffort = reasoningEffortFromMetadata(r.Metadata)
	return c
}

// reasoningEffortFromMetadata defensively extracts the per-conversation effort symbol
// from the aura.conversations.metadata jsonb (Phase 37E / D-06). nil/empty metadata,
// malformed JSON, a non-object, or an object missing (or with a non-string)
// reasoning_effort key all yield "" — which the frontend hydrates as auto (D-07). The
// column is otherwise DROPPED by the projection, so a poisoned metadata value can never
// panic the read path (T-37E-03-XSS: the value is surfaced as a controlled selector
// symbol, never raw HTML; it is written only via parameterized jsonb_set upstream).
func reasoningEffortFromMetadata(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var meta struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	return meta.ReasoningEffort
}

// DisplayTitle renders the title for the CLI list: the set title, or the SPEC
// "(untitled <created_at>)" fallback when the DB title is still NULL (Req#9).
func (c Conversation) DisplayTitle() string {
	if c.TitleSet && c.Title != "" {
		return c.Title
	}
	return fmt.Sprintf("(untitled %s)", c.CreatedAt)
}

// turnFromRow projects a generated turn row onto the domain Turn. content +
// content_sidecar_path are pgtype.Text (nullable); the empty string stands in for
// NULL (loadTurns rehydrates a non-empty sidecar path from disk), and so does 0 for a
// NULL parent_seq. It takes the ListTurnsBySeq row (a query-specific struct since 0017
// added branch_id to the table model that this SELECT omits); branchPathRowAsSeqRow
// (store_branch.go) adapts the field-identical branch-path row onto the same projection.
func turnFromRow(r sqlc.ListTurnsBySeqRow) Turn {
	return Turn{
		Seq:                int(r.Seq),
		ParentSeq:          int(r.ParentSeq.Int32),
		Role:               r.Role,
		Content:            r.Content.String,
		ContentSidecarPath: r.ContentSidecarPath.String,
		ToolCallID:         r.ToolCallID.String,
		ToolCalls:          r.ToolCalls,
		InputTokens:        int(r.InputTokens),
		OutputTokens:       int(r.OutputTokens),
		CachedTokens:       int(r.CachedTokens),
	}
}

// turnToMessage rehydrates one Turn into the llm.Message the loop consumes. The
// projection is pure (deterministic) so LoadHistory is byte-identical across
// calls (Req#8). tool_calls jsonb deserializes into []llm.ToolCall for assistant
// turns; tool_call_id populates only tool-role turns.
func turnToMessage(t Turn) (llm.Message, error) {
	msg := llm.Message{Role: t.Role, Content: t.Content, ToolCallID: t.ToolCallID}
	if len(t.ToolCalls) > 0 {
		calls, err := decodeToolCalls(t.ToolCalls)
		if err != nil {
			return llm.Message{}, fmt.Errorf("decode tool_calls: %w", err)
		}
		msg.ToolCalls = calls
	}
	return msg, nil
}

// maybeSpill returns the (content, content_sidecar_path) pgtype pair for a turn:
// content <= turnCapBytes stays inline (content set, path NULL); content over the
// cap writes the FULL bytes to a sidecar file and stores content=NULL + the path
// (SPEC Req#7). The sidecar layout mirrors tools/result.go
// ($AURA_RUN_DIR/conversations/<id>/<seq>.content), validated against traversal.
//
// LOOP-10 / D-10: a spilled turn stores content=NULL, so SearchConversationTurns never
// finds it. Since the search compares a query with the best-matching part of a message
// (word_similarity), a long body would be found if its text were indexed: this is a real
// gap, bounded by the cap and not addressed here (prd.md §7).
func (s *Store) maybeSpill(conversationID string, seq int, content string) (pgtype.Text, pgtype.Text, error) {
	if len(content) <= s.turnCapBytes {
		return pgtype.Text{String: content, Valid: true}, pgtype.Text{}, nil
	}
	path, err := s.turnSidecarPath(conversationID, seq)
	if err != nil {
		return pgtype.Text{}, pgtype.Text{}, err
	}
	if err := writeTurnSidecar(path, content); err != nil {
		return pgtype.Text{}, pgtype.Text{}, fmt.Errorf("write turn sidecar %q: %w", path, err)
	}
	return pgtype.Text{}, pgtype.Text{String: path, Valid: true}, nil
}

// turnSidecarPath builds the validated <run_dir>/conversations/<id>/<seq>.content
// path, rejecting traversal-shaped ids BEFORE filepath.Join (T-04-13, mirrors
// tools/result.go sidecarPath).
func (s *Store) turnSidecarPath(conversationID string, seq int) (string, error) {
	dir, err := s.sidecarDir(conversationID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%d.content", seq)), nil
}

// writeTurnSidecar persists the full turn content, creating the per-conversation
// dir lazily on first spill.
func writeTurnSidecar(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// optionalInt8 maps a zero int64 to a NULL pgtype.Int8 (mirrors optionalText:
// reasoning_duration_ms is only set on reasoning-bearing assistant answer turns).
func optionalInt8(v int64) pgtype.Int8 {
	if v == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: v, Valid: true}
}

// validateConversationID rejects any id that could escape the fixed
// <run_dir>/conversations/ prefix once joined into a path: `..` traversal or an
// embedded separator (T-04-13, mirrors tools/result.go validateID). The only
// model/agent-supplied segment is the conversation_id (D-26).
func validateConversationID(id string) error {
	if id == "" {
		return errors.New("conversation_id is empty")
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("conversation_id %q contains %q", id, "..")
	}
	for i := 0; i < len(id); i++ {
		if id[i] == '/' || id[i] == '\\' || os.IsPathSeparator(id[i]) {
			return fmt.Errorf("conversation_id %q contains a path separator", id)
		}
	}
	return nil
}

// decodeToolCalls unmarshals the tool_calls jsonb into []llm.ToolCall. A separate
// helper so turnToMessage stays small.
func decodeToolCalls(raw []byte) ([]llm.ToolCall, error) {
	var calls []llm.ToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, err
	}
	return calls, nil
}

// repairToolMessagePairs removes persisted tool-call fragments that would be
// rejected by OpenAI-compatible providers on reload. It is intentionally
// conservative: a valid assistant(tool_calls)->tool... group is preserved
// byte-for-byte; orphan RoleTool turns are dropped from the model-visible history.
// A dangling assistant tool-call group is repaired with synthetic RoleTool markers
// so a crash after a mutating tool executed cannot make the model blindly re-issue
// the same call on resume.
func repairToolMessagePairs(in []llm.Message) []llm.Message {
	return repairToolPairs(in, messagePairs)
}

// repairManagedToolMessagePairs is the LoadManagedHistory variant. L1 rewrites old
// tool turns to compact read_tool_output pointers; preserving those pointers as
// plain assistant memory keeps the context contract without emitting orphan tool
// messages to providers.
func repairManagedToolMessagePairs(in []llm.Message) []llm.Message {
	return repairToolPairs(in, managedMessagePairs)
}

// toolPairs says how repairToolPairs reads and builds what it repairs: a bare message for
// the model context, or a message with its place in the conversation tree for the history
// snapshot. One repair serves both, so the snapshot draws exactly the pairs the model sees.
type toolPairs[T any] struct {
	message func(T) llm.Message
	// recovered stands in for the result a call of assistant never got.
	recovered func(assistant T, call llm.ToolCall) T
	// pointer keeps a compacted tool turn as plain memory. Nil drops it with the orphans.
	pointer func(T) T
}

var (
	messagePairs        = toolPairs[llm.Message]{message: bareMessage, recovered: recoveredToolResult}
	managedMessagePairs = toolPairs[llm.Message]{message: bareMessage, recovered: recoveredToolResult, pointer: managedToolPointerMessage}
)

func bareMessage(m llm.Message) llm.Message { return m }

func recoveredToolResult(_ llm.Message, call llm.ToolCall) llm.Message {
	return llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: RecoveryToolResultContent(call)}
}

func repairToolPairs[T any](in []T, p toolPairs[T]) []T {
	if len(in) == 0 {
		return nil
	}
	msgs := make([]llm.Message, len(in))
	for i, item := range in {
		msgs[i] = p.message(item)
	}
	out := make([]T, 0, len(in))
	for i := 0; i < len(in); {
		m := msgs[i]
		if m.Role == llm.RoleTool {
			out = p.keepPointer(out, in[i], m)
			i++
			continue
		}
		if m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			out = append(out, in[i])
			i++
			continue
		}

		if validToolResultGroup(msgs, i) {
			end := i + 1 + len(m.ToolCalls)
			out = append(out, in[i:end]...)
			i = end
			continue
		}

		if !validToolCallIDs(m.ToolCalls) {
			for i++; i < len(in) && msgs[i].Role == llm.RoleTool; i++ {
				out = p.keepPointer(out, in[i], msgs[i])
			}
			continue
		}

		out, i = appendRecoveredToolResultGroup(out, in, msgs, i, p)
	}
	return out
}

func (p toolPairs[T]) keepPointer(out []T, item T, m llm.Message) []T {
	if p.pointer == nil || !compactedToolPointer(m) {
		return out
	}
	return append(out, p.pointer(item))
}

func appendRecoveredToolResultGroup[T any](out, in []T, msgs []llm.Message, assistantIdx int, p toolPairs[T]) ([]T, int) {
	calls := msgs[assistantIdx].ToolCalls
	out = append(out, in[assistantIdx])
	seen := make(map[string]int, len(calls))
	i := assistantIdx + 1
	for ; i < len(in) && msgs[i].Role == llm.RoleTool; i++ {
		id := msgs[i].ToolCallID
		if _, ok := seen[id]; !ok && toolCallHasID(calls, id) {
			seen[id] = i
		}
	}
	for _, call := range calls {
		if result, ok := seen[call.ID]; ok {
			out = append(out, in[result])
			continue
		}
		out = append(out, p.recovered(in[assistantIdx], call))
	}
	return out, i
}

// RecoveryToolResultContent is shared with display projection so an unknown
// recovered result cannot be mistaken for a successful tool execution.
func RecoveryToolResultContent(call llm.ToolCall) string {
	name := strings.TrimSpace(call.Function.Name)
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("error: previous result unknown after crash recovery for tool %q; verify before re-running this tool call.", name)
}

func validToolCallIDs(calls []llm.ToolCall) bool {
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if call.ID == "" {
			return false
		}
		if _, ok := seen[call.ID]; ok {
			return false
		}
		seen[call.ID] = struct{}{}
	}
	return true
}

func toolCallHasID(calls []llm.ToolCall, id string) bool {
	if id == "" {
		return false
	}
	for _, call := range calls {
		if call.ID == id {
			return true
		}
	}
	return false
}

// compactedToolPointer recognises an L1 eviction pointer in either wording: the marker the
// producer uses today, and the fixed prefix written before 2026-08-16 -- history is durable,
// so those rows still load. Matching the marker rather than a full phrase is what stops the
// two from drifting again.
func compactedToolPointer(m llm.Message) bool {
	return strings.HasPrefix(m.Content, legacyEvictedPointerPrefix) ||
		(strings.HasPrefix(m.Content, "[") && strings.Contains(m.Content, evictedResultMarker))
}

func managedToolPointerMessage(m llm.Message) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: m.Content}
}

func validToolResultGroup(in []llm.Message, assistantIdx int) bool {
	calls := in[assistantIdx].ToolCalls
	if assistantIdx+len(calls) >= len(in) {
		return false
	}
	want := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.ID == "" || want[call.ID] {
			return false
		}
		want[call.ID] = false
	}
	for j := range calls {
		msg := in[assistantIdx+1+j]
		if msg.Role != llm.RoleTool {
			return false
		}
		seen, ok := want[msg.ToolCallID]
		if !ok || seen {
			return false
		}
		want[msg.ToolCallID] = true
	}
	for _, seen := range want {
		if !seen {
			return false
		}
	}
	return true
}
