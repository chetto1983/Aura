package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

// operationalLessonWriter is the compact-memory write side for operational
// auto-accept. Tests inject a fake; production wires *memoryindex.Store.
type operationalLessonWriter interface {
	Upsert(ctx context.Context, doc memoryindex.Document) error
}

// patchProposalInserter is the persistence side of propose_patch.
// Tests inject a fake; production wires SQLPatchProposalStore.
type patchProposalInserter interface {
	// Insert writes a pending proposal row. Returns (true, nil) when the row
	// was created, (false, nil) when a row with the same signature_hash
	// already exists (idempotent), and (false, err) on storage failure.
	Insert(ctx context.Context, row patchProposalRow) (bool, error)
}

type runOriginReader interface {
	RunOrigin(ctx context.Context, runID string) (string, bool, error)
}

// patchProposalRow holds the fields written to proposed_updates.
type patchProposalRow struct {
	Kind          string // 'wiki' | 'user_memory' | 'operational_memory'
	Fact          string // body / fact / lesson text
	Action        string // always 'new' for propose_patch
	TargetSlug    string // wiki slug | category | tool_name
	Category      string // user_memory category | operational error_class
	SignatureHash string // sha256(action+fields)[:16] for idempotency
	SourceRunID   string // identity.RunIDFromContext
	ActorID       string // identity.ActorIDFromContext
	Status        string // defaults to 'pending'; quarantine rows set 'quarantine'
}

// ProposePatchTool lets the LLM submit structured patch proposals. Operational
// lessons auto-accept (write directly to compact_memory_documents, no review).
// Wiki and user_memory proposals land in proposed_updates with status=pending.
//
// This is the only mutation-adjacent tool available to write_proposal subagents.
type ProposePatchTool struct {
	store        patchProposalInserter
	opWriter     operationalLessonWriter // non-nil -> operational auto-accept (no review)
	originReader runOriginReader
}

// NewProposePatchTool returns a ProposePatchTool backed by store.
// Returns nil when store is nil so the caller can nil-gate registration.
func NewProposePatchTool(store patchProposalInserter) *ProposePatchTool {
	if store == nil {
		return nil
	}
	tool := &ProposePatchTool{store: store}
	if reader, ok := store.(runOriginReader); ok {
		tool.originReader = reader
	}
	return tool
}

// SetOperationalWriter injects a compact-memory writer so action=operational
// proposals auto-accept into compact_memory_documents (no review queue).
// Safe to call before or after registration.
func (t *ProposePatchTool) SetOperationalWriter(w operationalLessonWriter) {
	if t != nil {
		t.opWriter = w
	}
}

func (t *ProposePatchTool) SetRunOriginReader(reader runOriginReader) {
	if t != nil {
		t.originReader = reader
	}
}

func (t *ProposePatchTool) Name() string { return "propose_patch" }

func (t *ProposePatchTool) Description() string {
	return `Submit a structured patch proposal. action=operational auto-accepts into operational memory only when provenance and content-safety gates pass; suspicious content is quarantined for operator review. action=wiki and action=user_memory land in the /proposals review queue pending operator approval.

REQUIRED PARAMETERS BY ACTION (you MUST send all listed fields):
  • action="wiki":         target_slug, body, change_summary
  • action="user_memory":  fact, category, change_summary
  • action="operational":  tool_name, error_class, lesson, change_summary`
}

var proposePatchActions = []string{"wiki", "user_memory", "operational"}

var proposePatchHints = []ActionHint{
	{Name: "wiki", RequiredKeys: []string{"target_slug", "body"}},
	{Name: "user_memory", RequiredKeys: []string{"fact", "category"}},
	{Name: "operational", RequiredKeys: []string{"tool_name", "error_class", "lesson"}},
}

func (t *ProposePatchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        proposePatchActions,
				"description": "Proposal type: wiki (page edit), user_memory (user fact), operational (tool lesson).",
			},
			"target_slug": map[string]any{
				"type":        "string",
				"description": "Wiki page slug to propose editing (action=wiki only).",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Full proposed page body in Markdown (action=wiki only).",
			},
			"fact": map[string]any{
				"type":        "string",
				"description": "User fact or preference to surface for review (action=user_memory only).",
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"preference", "fact", "todo", "person"},
				"description": "Category of the user fact (action=user_memory only).",
			},
			"tool_name": map[string]any{
				"type":        "string",
				"description": "Name of the tool this lesson is about (action=operational only).",
			},
			"error_class": map[string]any{
				"type":        "string",
				"description": "Error class this lesson addresses (action=operational only).",
			},
			"lesson": map[string]any{
				"type":        "string",
				"description": "Lesson text describing the operational finding (action=operational only).",
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{memoryindex.PriorityNormal, memoryindex.PriorityHigh, memoryindex.PriorityCritical},
				"description": "Operational lesson priority. Defaults to normal. High/Critical lessons are pinned in the system prompt after review/security gates.",
			},
			"change_summary": map[string]any{
				"type":        "string",
				"description": "One-sentence explanation of why this change is proposed (required for all actions).",
			},
		},
		"required": []string{"action", "change_summary"},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "wiki", RequiredKeys: []string{"target_slug", "body", "change_summary"}},
			{Name: "user_memory", RequiredKeys: []string{"fact", "category", "change_summary"}},
			{Name: "operational", RequiredKeys: []string{"tool_name", "error_class", "lesson", "change_summary"}},
		}),
	}
}

func (t *ProposePatchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("propose_patch: tool unavailable")
	}
	action := stringArg(args, "action")
	if action == "" {
		return "", ActionRequiredError("propose_patch", proposePatchActions, args, proposePatchHints, "wiki")
	}
	changeSummary := stringArg(args, "change_summary")
	if changeSummary == "" {
		return "", fmt.Errorf("propose_patch: change_summary is required: %w", llm.ErrSchemaValidation)
	}
	switch action {
	case "wiki":
		return t.executeWiki(ctx, args)
	case "user_memory":
		return t.executeUserMemory(ctx, args)
	case "operational":
		return t.executeOperational(ctx, args)
	default:
		return "", UnknownActionError("propose_patch", action, proposePatchActions, args)
	}
}

func (t *ProposePatchTool) executeWiki(ctx context.Context, args map[string]any) (string, error) {
	targetSlug := stringArg(args, "target_slug")
	if targetSlug == "" {
		return "", fmt.Errorf("propose_patch wiki: target_slug is required: %w", llm.ErrSchemaValidation)
	}
	body := stringArg(args, "body")
	if body == "" {
		return "", fmt.Errorf("propose_patch wiki: body is required: %w", llm.ErrSchemaValidation)
	}
	row := patchProposalRow{
		Kind:          "wiki",
		Fact:          body,
		Action:        "new",
		TargetSlug:    targetSlug,
		Category:      "",
		SignatureHash: proposePatchHash("wiki", targetSlug, body),
		SourceRunID:   identity.RunIDFromContext(ctx),
		ActorID:       identity.ActorIDFromContext(ctx),
	}
	created, err := t.store.Insert(ctx, row)
	if err != nil {
		return "", fmt.Errorf("propose_patch wiki: %w", err)
	}
	if !created {
		return fmt.Sprintf("propose_patch: wiki proposal for %q already pending (idempotent skip)", targetSlug), nil
	}
	return fmt.Sprintf("propose_patch: wiki proposal for %q submitted (pending operator review)", targetSlug), nil
}

func (t *ProposePatchTool) executeUserMemory(ctx context.Context, args map[string]any) (string, error) {
	fact := stringArg(args, "fact")
	if fact == "" {
		return "", fmt.Errorf("propose_patch user_memory: fact is required: %w", llm.ErrSchemaValidation)
	}
	category := stringArg(args, "category")
	if category == "" {
		return "", fmt.Errorf("propose_patch user_memory: category is required: %w", llm.ErrSchemaValidation)
	}
	row := patchProposalRow{
		Kind:          "user_memory",
		Fact:          fact,
		Action:        "new",
		TargetSlug:    category,
		Category:      category,
		SignatureHash: proposePatchHash("user_memory", fact, category),
		SourceRunID:   identity.RunIDFromContext(ctx),
		ActorID:       identity.ActorIDFromContext(ctx),
	}
	created, err := t.store.Insert(ctx, row)
	if err != nil {
		return "", fmt.Errorf("propose_patch user_memory: %w", err)
	}
	if !created {
		return "propose_patch: user_memory proposal already pending (idempotent skip)", nil
	}
	return "propose_patch: user_memory proposal submitted (pending operator review)", nil
}

func (t *ProposePatchTool) executeOperational(ctx context.Context, args map[string]any) (string, error) {
	toolName := stringArg(args, "tool_name")
	if toolName == "" {
		return "", fmt.Errorf("propose_patch operational: tool_name is required: %w", llm.ErrSchemaValidation)
	}
	errorClass := stringArg(args, "error_class")
	if errorClass == "" {
		return "", fmt.Errorf("propose_patch operational: error_class is required: %w", llm.ErrSchemaValidation)
	}
	lesson := stringArg(args, "lesson")
	if lesson == "" {
		return "", fmt.Errorf("propose_patch operational: lesson is required: %w", llm.ErrSchemaValidation)
	}
	priority, ok := memoryindex.ParsePriority(stringArg(args, "priority"))
	if !ok {
		return "", fmt.Errorf("propose_patch operational: priority must be one of normal, high, critical: %w", llm.ErrSchemaValidation)
	}
	sig := proposePatchHash("operational", toolName, errorClass, lesson)
	sourceRunID := identity.RunIDFromContext(ctx)

	allowed, reason, err := t.operationalProvenanceAllowed(ctx, sourceRunID)
	if err != nil {
		return "", fmt.Errorf("propose_patch operational: %w", err)
	}
	if !allowed {
		return proposePatchDecisionJSON(false, "provenance: "+reason), nil
	}

	contentDecision := inspectOperationalLessonContent(lesson, stringArg(args, "change_summary"))
	if contentDecision.Action == operationalContentHardReject {
		return proposePatchDecisionJSON(false, "content: "+contentDecision.Reason), nil
	}
	if contentDecision.Action == operationalContentQuarantine {
		row := patchProposalRow{
			Kind:          "operational_memory",
			Fact:          lesson,
			Action:        "new",
			TargetSlug:    toolName,
			Category:      errorClass,
			SignatureHash: sig,
			SourceRunID:   sourceRunID,
			ActorID:       identity.ActorIDFromContext(ctx),
			Status:        "quarantine",
		}
		created, err := t.store.Insert(ctx, row)
		if err != nil {
			return "", fmt.Errorf("propose_patch operational: quarantine: %w", err)
		}
		if !created {
			return proposePatchDecisionJSON(false, "content: already quarantined"), nil
		}
		return proposePatchDecisionJSON(false, "content: quarantined: "+contentDecision.Reason), nil
	}

	// Auto-accept path: write directly to compact_memory_documents when a writer
	// is wired. Skips the review queue so Aura can recall lessons immediately.
	if t.opWriter != nil {
		doc := memoryindex.Document{
			ID:        "operational:" + sig,
			Kind:      memoryindex.KindOperational,
			Title:     fmt.Sprintf("Lesson: %s %s", toolName, errorClass),
			Body:      lesson,
			Handle:    "operational:" + sig,
			Status:    "active",
			Priority:  priority,
			UpdatedAt: time.Now().UTC(),
		}
		if err := t.opWriter.Upsert(ctx, doc); err != nil {
			return "", fmt.Errorf("propose_patch operational: %w", err)
		}
		return fmt.Sprintf("propose_patch: operational lesson for %q accepted and stored immediately", toolName), nil
	}

	// Fallback: write to proposed_updates review queue (opWriter not configured).
	row := patchProposalRow{
		Kind:          "operational_memory",
		Fact:          lesson,
		Action:        "new",
		TargetSlug:    toolName,
		Category:      errorClass,
		SignatureHash: sig,
		SourceRunID:   sourceRunID,
		ActorID:       identity.ActorIDFromContext(ctx),
	}
	created, err := t.store.Insert(ctx, row)
	if err != nil {
		return "", fmt.Errorf("propose_patch operational: %w", err)
	}
	if !created {
		return "propose_patch: operational proposal already pending (idempotent skip)", nil
	}
	return fmt.Sprintf("propose_patch: operational proposal for %q submitted (pending operator review)", toolName), nil
}

// proposePatchHash computes sha256(action\x00field1\x00field2...)[:16].
func proposePatchHash(action string, fields ...string) string {
	parts := make([]string, 0, 1+len(fields))
	parts = append(parts, action)
	parts = append(parts, fields...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum[:])[:16]
}

func (t *ProposePatchTool) operationalProvenanceAllowed(ctx context.Context, runID string) (bool, string, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return false, "missing run_id", nil
	}
	if t.originReader == nil {
		return false, "run_origin reader unavailable", nil
	}
	origin, ok, err := t.originReader.RunOrigin(ctx, runID)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "run_origin missing for run_id " + runID, nil
	}
	origin = strings.TrimSpace(origin)
	if origin == "user" {
		return true, "", nil
	}
	if origin == "" {
		origin = "unknown"
	}
	return false, "run_origin=" + origin, nil
}

func proposePatchDecisionJSON(accepted bool, reason string) string {
	payload := map[string]any{"accepted": accepted}
	if reason != "" {
		payload["reason"] = reason
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"accepted":%t}`, accepted)
	}
	return string(encoded)
}

// SQLPatchProposalStore is the production patchProposalInserter backed by SQLite.
// It writes to the proposed_updates table (migrations v14 + v16 required).
type SQLPatchProposalStore struct {
	db *sql.DB
}

// NewSQLPatchProposalStore wraps an existing *sql.DB.
func NewSQLPatchProposalStore(db *sql.DB) *SQLPatchProposalStore {
	if db == nil {
		return nil
	}
	return &SQLPatchProposalStore{db: db}
}

func (s *SQLPatchProposalStore) Insert(ctx context.Context, row patchProposalRow) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM proposed_updates WHERE signature_hash = ?`,
		row.SignatureHash,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("patchProposalStore: check existing: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	provenanceJSON := fmt.Sprintf(`{"source_run_id":%q}`, row.SourceRunID)
	status := strings.TrimSpace(row.Status)
	if status == "" {
		status = "pending"
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO proposed_updates
		  (chat_id, fact, action, target_slug, similarity,
		   source_turn_ids, category, related_slugs, provenance_json,
		   status, kind, signature_hash, actor_id, created_at)
		VALUES (0, ?, ?, ?, 1.0, '[]', ?, '[]', ?, ?, ?, ?, ?, ?)`,
		row.Fact, row.Action, row.TargetSlug, row.Category, provenanceJSON,
		status, row.Kind, row.SignatureHash, row.ActorID, now,
	)
	if err != nil {
		return false, fmt.Errorf("patchProposalStore: insert: %w", err)
	}
	return true, nil
}

func (s *SQLPatchProposalStore) RunOrigin(ctx context.Context, runID string) (string, bool, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return "", false, nil
	}
	var origin string
	err := s.db.QueryRowContext(ctx,
		`SELECT run_origin FROM run_events WHERE run_id = ? ORDER BY seq ASC LIMIT 1`,
		runID,
	).Scan(&origin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("patchProposalStore: run origin: %w", err)
	}
	return origin, true, nil
}
