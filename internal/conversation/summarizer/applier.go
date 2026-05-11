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
		Related:       uniqueNonEmpty(d.Candidate.RelatedSlugs),
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
		if slug != "" && !containsStr(page.Related, slug) {
			page.Related = append(page.Related, slug)
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

func turnEvidenceRefs(ids []int64) []EvidenceRef {
	if len(ids) == 0 {
		return []EvidenceRef{}
	}
	out := make([]EvidenceRef, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		out = append(out, EvidenceRef{Kind: "archive", ID: fmt.Sprintf("conversation:%d", id)})
	}
	return out
}
