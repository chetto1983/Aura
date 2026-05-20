package summarizer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// WikiWriter is the wiki write/read journal surface needed by AutoApplier.
// WritePage accepts an optional variadic expectedUpdatedAt for ETag-based
// optimistic concurrency (WIKI-02). Existing callers pass no variadic arg.
type WikiWriter interface {
	WritePage(ctx context.Context, page *wiki.Page, expectedUpdatedAt ...string) error
	ReadPage(slug string) (*wiki.Page, error)
	AppendLog(ctx context.Context, action, slug string)
}

// Applier applies a single Decision.
type Applier interface {
	Apply(ctx context.Context, d Decision) error
}

// ---- AutoApplier ----

// AutoApplier writes directly to the wiki store.
type AutoApplier struct {
	wiki WikiWriter
}

// NewAutoApplier returns an AutoApplier backed by the given WikiWriter.
func NewAutoApplier(w WikiWriter) *AutoApplier {
	return &AutoApplier{wiki: w}
}

func (a *AutoApplier) Apply(ctx context.Context, d Decision) error {
	switch d.Action {
	case ActionNew:
		return a.applyNew(ctx, d)
	case ActionPatch:
		return a.applyPatch(ctx, d)
	case ActionSkip:
		a.wiki.AppendLog(ctx, "proposal skip", d.TargetSlug)
		return nil
	default:
		return fmt.Errorf("auto applier: unknown action %q", d.Action)
	}
}

func (a *AutoApplier) applyNew(ctx context.Context, d Decision) error {
	title := d.Candidate.Fact
	if len(title) > 80 {
		title = title[:80]
	}
	sources := make([]string, len(d.Candidate.SourceTurnIDs))
	for i, id := range d.Candidate.SourceTurnIDs {
		sources[i] = fmt.Sprintf("turn:%d", id)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "proposal_v1",
		Title:         title,
		Category:      d.Candidate.Category,
		Related:       wiki.RelatedFromSlugs(uniqueNonEmpty(d.Candidate.RelatedSlugs)),
		Tags:          []string{"auto-added"},
		Sources:       sources,
		CreatedAt:     now,
		UpdatedAt:     now,
		Body:          fmt.Sprintf("%s\n\n*Approved from Aura's review queue.*", d.Candidate.Fact),
	}
	if err := a.wiki.WritePage(ctx, page); err != nil {
		return fmt.Errorf("auto applier new: %w", err)
	}
	a.wiki.AppendLog(ctx, "proposal new", wiki.Slug(title))
	return nil
}

func (a *AutoApplier) applyPatch(ctx context.Context, d Decision) error {
	page, err := a.wiki.ReadPage(d.TargetSlug)
	if err != nil {
		return fmt.Errorf("auto applier patch read: %w", err)
	}
	date := time.Now().UTC().Format("2006-01-02")
	block := fmt.Sprintf("\n\n> [proposal %s] %s\n", date, d.Candidate.Fact)
	page.Body = strings.TrimRight(page.Body, "\n") + block
	// Append new source turn IDs.
	for _, id := range d.Candidate.SourceTurnIDs {
		ref := fmt.Sprintf("turn:%d", id)
		if !containsStr(page.Sources, ref) {
			page.Sources = append(page.Sources, ref)
		}
	}
	for _, slug := range d.Candidate.RelatedSlugs {
		if slug != "" && !wiki.RelatedContainsSlug(page.Related, slug) {
			page.Related = append(page.Related, wiki.RelatedRef{Slug: slug, Confidence: "EXTRACTED"})
		}
	}
	page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.wiki.WritePage(ctx, page); err != nil {
		return fmt.Errorf("auto applier patch write: %w", err)
	}
	a.wiki.AppendLog(ctx, "proposal patch", d.TargetSlug)
	return nil
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func uniqueNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// ---- User-memory routing ----

// UserMemoryHit is a found user-memory entry returned by MemorySearcher.
type UserMemoryHit struct {
	Handle string
}

// MemorySearcher finds existing user-memory entries by text similarity.
// Implementations filter to Kind='user_memory', Status='active'.
type MemorySearcher interface {
	SearchUserMemory(ctx context.Context, query string, limit int) ([]UserMemoryHit, error)
}

// UserMemoryProposalWriter is the write side for user-memory proposals.
type UserMemoryProposalWriter interface {
	// CreateUserMemoryProposal inserts a pending user_memory proposal.
	// Returns (created=false, nil) if a pending row with the same handle already exists.
	CreateUserMemoryProposal(ctx context.Context, p UserMemoryProposalInput) (bool, error)
}

// UserMemoryProposalInput holds the parameters for a user_memory proposal row.
type UserMemoryProposalInput struct {
	Handle        string  // idempotency key = UserFactHandle(Candidate)
	Fact          string
	Action        string  // "new" or "patch"
	TargetHandle  string  // non-empty for patch: existing user_memory document handle
	SourceTurnIDs []int64
	Category      string
}

// RoutingApplier wraps a wiki Applier and intercepts candidates triaged to
// TargetUserMemory, routing them to proposed_updates with kind='user_memory'.
type RoutingApplier struct {
	wiki      Applier
	proposals UserMemoryProposalWriter
	memory    MemorySearcher
	questions QuestionEmitter // optional; nil disables the ambiguity gate
}

// NewRoutingApplier constructs a RoutingApplier.
func NewRoutingApplier(wiki Applier, proposals UserMemoryProposalWriter, memory MemorySearcher) *RoutingApplier {
	return &RoutingApplier{wiki: wiki, proposals: proposals, memory: memory}
}

// WithQuestionEmitter attaches an ambiguity gate emitter and returns the same
// RoutingApplier for fluent construction. When set, candidates with
// Score < AmbiguityThreshold emit a question instead of creating a proposal.
func (r *RoutingApplier) WithQuestionEmitter(e QuestionEmitter) *RoutingApplier {
	r.questions = e
	return r
}

// Apply routes the decision: user-memory candidates go to proposed_updates with
// kind='user_memory'; all other candidates use the underlying wiki applier.
func (r *RoutingApplier) Apply(ctx context.Context, d Decision) error {
	if TriageCandidate(d.Candidate) == TargetUserMemory {
		return r.createUserMemoryProposal(ctx, d.Candidate)
	}
	return r.wiki.Apply(ctx, d)
}

func (r *RoutingApplier) createUserMemoryProposal(ctx context.Context, c Candidate) error {
	if r.questions != nil && ShouldGateUserMemoryWrite(c) {
		return r.questions.EmitUserMemoryQuestion(ctx, c)
	}
	return submitUserMemoryProposal(ctx, c, r.proposals, r.memory)
}

