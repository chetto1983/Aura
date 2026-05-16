package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

// patchProposalInserter is the persistence side of propose_patch.
// Tests inject a fake; production wires SQLPatchProposalStore.
type patchProposalInserter interface {
	// Insert writes a pending proposal row. Returns (true, nil) when the row
	// was created, (false, nil) when a row with the same signature_hash
	// already exists (idempotent), and (false, err) on storage failure.
	Insert(ctx context.Context, row patchProposalRow) (bool, error)
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
}

// ProposePatchTool lets the LLM submit structured patch proposals for operator
// review. Proposals land in proposed_updates with status=pending and are NEVER
// applied without explicit operator approval.
//
// This is the only mutation-adjacent tool available to write_proposal subagents.
// ALL writes are review-gated — no direct mutations.
type ProposePatchTool struct {
	store patchProposalInserter
}

// NewProposePatchTool returns a ProposePatchTool backed by store.
// Returns nil when store is nil so the caller can nil-gate registration.
func NewProposePatchTool(store patchProposalInserter) *ProposePatchTool {
	if store == nil {
		return nil
	}
	return &ProposePatchTool{store: store}
}

func (t *ProposePatchTool) Name() string { return "propose_patch" }

func (t *ProposePatchTool) Description() string {
	return `Submit a structured patch proposal for operator review. Proposals land in proposed_updates with status=pending and are visible in the dashboard /proposals review queue. Use action=wiki to suggest a wiki page edit, action=user_memory to surface a candidate user fact, action=operational to record an operational lesson. ALL writes are review-gated — no direct mutations.

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
	row := patchProposalRow{
		Kind:          "operational_memory",
		Fact:          lesson,
		Action:        "new",
		TargetSlug:    toolName,
		Category:      errorClass,
		SignatureHash: proposePatchHash("operational", toolName, errorClass, lesson),
		SourceRunID:   identity.RunIDFromContext(ctx),
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
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO proposed_updates
		  (chat_id, fact, action, target_slug, similarity,
		   source_turn_ids, category, related_slugs, provenance_json,
		   status, kind, signature_hash, actor_id, created_at)
		VALUES (0, ?, ?, ?, 1.0, '[]', ?, '[]', ?, 'pending', ?, ?, ?, ?)`,
		row.Fact, row.Action, row.TargetSlug, row.Category, provenanceJSON,
		row.Kind, row.SignatureHash, row.ActorID, now,
	)
	if err != nil {
		return false, fmt.Errorf("patchProposalStore: insert: %w", err)
	}
	return true, nil
}
