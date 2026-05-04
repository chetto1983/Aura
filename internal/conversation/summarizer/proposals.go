package summarizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrProposalNotFound is returned when a proposed_update row doesn't exist.
var ErrProposalNotFound = errors.New("summarizer: proposal not found")

// ErrProposalConflict is returned when trying to approve/reject an already-decided proposal.
var ErrProposalConflict = errors.New("summarizer: proposal already decided")

// ProposedUpdate is one row from proposed_updates.
type ProposedUpdate struct {
	ID            int64      `json:"id"`
	ChatID        int64      `json:"chat_id"`
	Fact          string     `json:"fact"`
	Action        string     `json:"action"`
	TargetSlug    string     `json:"target_slug,omitempty"`
	Similarity    float64    `json:"similarity"`
	SourceTurnIDs []int64    `json:"source_turn_ids"`
	Category      string     `json:"category,omitempty"`
	RelatedSlugs  []string   `json:"related_slugs"`
	Provenance    Provenance `json:"provenance,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
}

// EvidenceRef points to one compact source of evidence behind a proposal.
type EvidenceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Page    int    `json:"page,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Provenance explains why and from where a proposal was created.
type Provenance struct {
	OriginTool   string         `json:"origin_tool,omitempty"`
	OriginReason string         `json:"origin_reason,omitempty"`
	ProposalKind string         `json:"proposal_kind,omitempty"`
	Evidence     []EvidenceRef  `json:"evidence,omitempty"`
	Skill        *SkillProposal `json:"skill,omitempty"`
	AgentJobID   string         `json:"agent_job_id,omitempty"`
	SwarmRunID   string         `json:"swarm_run_id,omitempty"`
	SwarmTaskID  string         `json:"swarm_task_id,omitempty"`
}

// SkillProposal is a review-gated procedural-memory draft. Approval in the
// generic summaries queue marks it reviewed; installation/smoke-test workflows
// stay in the skills/admin layer.
type SkillProposal struct {
	Action       string   `json:"action"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	SmokePrompt  string   `json:"smoke_prompt,omitempty"`
	Content      string   `json:"content,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// SummariesStore provides CRUD over the proposed_updates table.
type SummariesStore struct {
	db *sql.DB
}

// ProposalInput is a manually-created wiki proposal. It uses the same
// proposed_updates queue as review-mode summarization so dashboard approval
// remains the single mutation gate.
type ProposalInput struct {
	ChatID        int64
	Fact          string
	Action        string
	TargetSlug    string
	Similarity    float64
	SourceTurnIDs []int64
	Category      string
	RelatedSlugs  []string
	Provenance    Provenance
}

// NewSummariesStore wraps an existing *sql.DB. The migration must already
// have been applied (scheduler.OpenStore handles this).
func NewSummariesStore(db *sql.DB) *SummariesStore {
	return &SummariesStore{db: db}
}

// Propose inserts a pending wiki update without mutating the wiki.
func (s *SummariesStore) Propose(ctx context.Context, in ProposalInput) (ProposedUpdate, error) {
	fact := strings.TrimSpace(in.Fact)
	if fact == "" {
		return ProposedUpdate{}, errors.New("summaries propose: fact is required")
	}
	action := strings.TrimSpace(in.Action)
	switch action {
	case string(ActionNew):
		in.TargetSlug = ""
	case string(ActionPatch):
		in.TargetSlug = strings.TrimSpace(in.TargetSlug)
		if in.TargetSlug == "" {
			return ProposedUpdate{}, errors.New("summaries propose: target_slug is required for patch")
		}
	case string(ActionSkillCreate), string(ActionSkillUpdate), string(ActionSkillDelete):
		in.TargetSlug = ""
	default:
		return ProposedUpdate{}, fmt.Errorf("summaries propose: unsupported action %q", action)
	}
	similarity := in.Similarity
	if similarity <= 0 {
		similarity = 1
	}
	if similarity > 1 {
		similarity = 1
	}
	ids, _ := json.Marshal(in.SourceTurnIDs)
	related, _ := json.Marshal(cleanProposalStrings(in.RelatedSlugs))
	provenance, _ := json.Marshal(cleanProvenance(in.Provenance))

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
		in.ChatID,
		fact,
		action,
		strings.TrimSpace(in.TargetSlug),
		similarity,
		string(ids),
		strings.TrimSpace(in.Category),
		string(related),
		string(provenance),
	)
	if err != nil {
		return ProposedUpdate{}, fmt.Errorf("summaries propose: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ProposedUpdate{}, fmt.Errorf("summaries propose last id: %w", err)
	}
	return s.Get(ctx, id)
}

// List returns proposed updates, optionally filtered by status (empty = all).
// Ordered by created_at DESC; capped by limit (0 = default 100).
func (s *SummariesStore) List(ctx context.Context, status string, limit int) ([]ProposedUpdate, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status, created_at
			 FROM proposed_updates WHERE status = ? ORDER BY created_at DESC LIMIT ?`,
			status, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status, created_at
			 FROM proposed_updates ORDER BY created_at DESC LIMIT ?`,
			limit)
	}
	if err != nil {
		return nil, fmt.Errorf("summaries list: %w", err)
	}
	defer rows.Close()

	out := []ProposedUpdate{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns a single proposal by ID, or ErrProposalNotFound.
func (s *SummariesStore) Get(ctx context.Context, id int64) (ProposedUpdate, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status, created_at
		 FROM proposed_updates WHERE id = ?`, id)
	p, err := scanProposal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProposedUpdate{}, ErrProposalNotFound
		}
		return ProposedUpdate{}, fmt.Errorf("summaries get: %w", err)
	}
	return p, nil
}

// SetStatus flips the status of a proposal. Returns ErrProposalNotFound if
// no row exists, ErrProposalConflict if already approved or rejected.
func (s *SummariesStore) SetStatus(ctx context.Context, id int64, newStatus string) (ProposedUpdate, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE proposed_updates SET status = ? WHERE id = ? AND status = 'pending'`, newStatus, id)
	if err != nil {
		return ProposedUpdate{}, fmt.Errorf("summaries set status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ProposedUpdate{}, fmt.Errorf("summaries set status rows affected: %w", err)
	}
	if affected == 1 {
		return s.Get(ctx, id)
	}
	if _, err := s.Get(ctx, id); err != nil {
		return ProposedUpdate{}, err
	}
	return ProposedUpdate{}, ErrProposalConflict
}

type proposalScanner interface {
	Scan(dest ...any) error
}

func scanProposal(r proposalScanner) (ProposedUpdate, error) {
	var p ProposedUpdate
	var idsJSON string
	var relatedJSON string
	var provenanceJSON string
	var createdAt string
	if err := r.Scan(
		&p.ID, &p.ChatID, &p.Fact, &p.Action, &p.TargetSlug,
		&p.Similarity, &idsJSON, &p.Category, &relatedJSON, &provenanceJSON, &p.Status, &createdAt,
	); err != nil {
		return ProposedUpdate{}, err
	}
	if idsJSON != "" && idsJSON != "null" {
		_ = json.Unmarshal([]byte(idsJSON), &p.SourceTurnIDs)
	}
	if p.SourceTurnIDs == nil {
		p.SourceTurnIDs = []int64{}
	}
	if relatedJSON != "" && relatedJSON != "null" {
		_ = json.Unmarshal([]byte(relatedJSON), &p.RelatedSlugs)
	}
	if p.RelatedSlugs == nil {
		p.RelatedSlugs = []string{}
	}
	if provenanceJSON != "" && provenanceJSON != "null" {
		_ = json.Unmarshal([]byte(provenanceJSON), &p.Provenance)
	}
	p.Provenance = cleanProvenance(p.Provenance)
	ts, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return ProposedUpdate{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
		}
	}
	p.CreatedAt = ts.UTC()
	return p, nil
}

func cleanProvenance(p Provenance) Provenance {
	p.OriginTool = strings.TrimSpace(p.OriginTool)
	p.OriginReason = strings.TrimSpace(p.OriginReason)
	p.ProposalKind = strings.TrimSpace(p.ProposalKind)
	p.AgentJobID = strings.TrimSpace(p.AgentJobID)
	p.SwarmRunID = strings.TrimSpace(p.SwarmRunID)
	p.SwarmTaskID = strings.TrimSpace(p.SwarmTaskID)
	p.Skill = cleanSkillProposal(p.Skill)
	if len(p.Evidence) == 0 {
		p.Evidence = []EvidenceRef{}
		return p
	}
	out := make([]EvidenceRef, 0, len(p.Evidence))
	seen := map[string]struct{}{}
	for _, ref := range p.Evidence {
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Title = strings.TrimSpace(ref.Title)
		ref.Snippet = strings.TrimSpace(ref.Snippet)
		if ref.Kind == "" || ref.ID == "" {
			continue
		}
		key := ref.Kind + "\x00" + ref.ID
		if ref.Page > 0 {
			key += fmt.Sprintf("\x00%d", ref.Page)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
		if len(out) >= 12 {
			break
		}
	}
	p.Evidence = out
	return p
}

func cleanSkillProposal(in *SkillProposal) *SkillProposal {
	if in == nil {
		return nil
	}
	out := *in
	out.Action = strings.TrimSpace(out.Action)
	out.Name = strings.TrimSpace(out.Name)
	out.Description = strings.TrimSpace(out.Description)
	out.SmokePrompt = strings.TrimSpace(out.SmokePrompt)
	out.Content = strings.TrimSpace(out.Content)
	out.Reason = strings.TrimSpace(out.Reason)
	out.AllowedTools = cleanProposalStrings(out.AllowedTools)
	if out.Action == "" && out.Name == "" && out.Description == "" && out.Content == "" && out.Reason == "" {
		return nil
	}
	return &out
}

func cleanProposalStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
