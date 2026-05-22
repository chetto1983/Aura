package ingest

import (
	"context"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// upsertEntityPage creates a fresh entity page or merges a new citation block
// into an existing one. The sources-contains check short-circuits re-ingests of
// the same source so a Compile rerun is a no-op for unchanged extractions.
func (p *Pipeline) upsertEntityPage(ctx context.Context, ent ExtractedEntity, sourceID string) error {
	sourceTag := "source:" + sourceID
	now := p.now().UTC().Format(time.RFC3339)

	existing, err := p.wiki.ReadPage(ent.Slug)
	if err != nil {
		page := &wiki.Page{
			Title:         ent.Title,
			Body:          buildEntityBody(ent, sourceID),
			Category:      "entity",
			Tags:          []string{"entity", ent.Type},
			Related:       wiki.RelatedFromSlugs(cleanedRelated(ent.RelatesTo, ent.Slug)),
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
		annotateProvenance(ent.ShortDescription, sourceID) + "\n"
	for _, rel := range cleanedRelated(ent.RelatesTo, ent.Slug) {
		if !wiki.RelatedContainsSlug(existing.Related, rel) {
			existing.Related = append(existing.Related, wiki.RelatedRef{Slug: rel, Confidence: "EXTRACTED"})
		}
	}
	existing.UpdatedAt = now
	return p.wiki.WritePage(ctx, existing)
}

// buildEntityBody renders the initial markdown body for a new entity page.
func buildEntityBody(ent ExtractedEntity, sourceID string) string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(ent.Title)
	sb.WriteString("\n\n_Type: ")
	sb.WriteString(ent.Type)
	sb.WriteString(" — extracted from sources._\n\n## From source:")
	sb.WriteString(sourceID)
	sb.WriteString("\n\n")
	sb.WriteString(annotateProvenance(ent.ShortDescription, sourceID))
	sb.WriteByte('\n')
	return sb.String()
}
