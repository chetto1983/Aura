package ingest

import (
	"context"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// upsertConceptPage creates a fresh concept page or merges a new citation block
// into an existing one. The sources-contains check short-circuits re-ingests of
// the same source so a Compile rerun is a no-op for unchanged extractions.
func (p *Pipeline) upsertConceptPage(ctx context.Context, c ExtractedConcept, sourceID string) error {
	sourceTag := "source:" + sourceID
	now := p.now().UTC().Format(time.RFC3339)

	existing, err := p.wiki.ReadPage(c.Slug)
	if err != nil {
		page := &wiki.Page{
			Title:         c.Title,
			Body:          buildConceptBody(c, sourceID),
			Category:      "concept",
			Tags:          []string{"concept"},
			Sources:       []string{sourceTag},
			SchemaVersion: wiki.CurrentSchemaVersion,
			PromptVersion: PromptVersion(),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		return p.wiki.WritePage(ctx, page)
	}

	if containsString(existing.Sources, sourceTag) {
		return nil
	}
	existing.Sources = append(existing.Sources, sourceTag)
	existing.Body = strings.TrimRight(existing.Body, "\n") +
		"\n\n## From source:" + sourceID + "\n\n" +
		annotateProvenance(c.Summary, sourceID) + "\n"
	if len(c.KeyClaims) > 0 {
		existing.Body += "\n### Key claims\n\n"
		for _, claim := range c.KeyClaims {
			existing.Body += "> " + annotateProvenance(claim, sourceID) + "\n"
		}
	}
	existing.UpdatedAt = now
	return p.wiki.WritePage(ctx, existing)
}

// buildConceptBody renders the initial markdown body for a new concept page.
func buildConceptBody(c ExtractedConcept, sourceID string) string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(c.Title)
	sb.WriteString("\n\n_Theme extracted from sources._\n\n## From source:")
	sb.WriteString(sourceID)
	sb.WriteString("\n\n")
	sb.WriteString(annotateProvenance(c.Summary, sourceID))
	sb.WriteByte('\n')
	if len(c.KeyClaims) > 0 {
		sb.WriteString("\n### Key claims\n\n")
		for _, claim := range c.KeyClaims {
			sb.WriteString("> ")
			sb.WriteString(annotateProvenance(claim, sourceID))
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
