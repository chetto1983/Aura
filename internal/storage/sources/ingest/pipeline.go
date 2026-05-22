// Package ingest compiles completed sources into wiki summary pages.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/wiki"
)

// Pipeline turns completed sources into compact wiki summary pages.
type Pipeline struct {
	sources   source.Repository
	wiki      wiki.Repository
	search    search.WikiPageReindexer
	extractor Extractor
	logger    *slog.Logger
	now       func() time.Time
}

// Config wires the pipeline to existing stores.
type Config struct {
	Sources   source.Repository
	Wiki      wiki.Repository
	Search    search.WikiPageReindexer
	Logger    *slog.Logger
	// Now is overridable for tests so created_at/updated_at are deterministic.
	Now func() time.Time
	// Extractor enables multi-page touch (Wave 2.4). Nil = single-page summary only.
	Extractor Extractor
}

// New builds a Pipeline. Sources and Wiki are required.
func New(cfg Config) (*Pipeline, error) {
	if cfg.Sources == nil {
		return nil, errors.New("ingest: source store is required")
	}
	if cfg.Wiki == nil {
		return nil, errors.New("ingest: wiki store is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Pipeline{
		sources:   cfg.Sources,
		wiki:      cfg.Wiki,
		search:    cfg.Search,
		extractor: cfg.Extractor,
		logger:    logger,
		now:       now,
	}, nil
}

// Result captures the outcome of a Compile call.
type Result struct {
	Slug              string   // wiki slug of the summary page, always set on success
	Created           bool     // true on first compile, false when already ingested
	MaterializedPages []string // structured sub-pages written after the summary
	PageNote          string   // user-facing one-liner for Telegram progress UX
}

// Compile writes (or refreshes) the source summary page and flips status to ingested.
// Idempotent: a second call on an already-ingested source with unchanged slugs returns Created=false.
func (p *Pipeline) Compile(ctx context.Context, sourceID string) (Result, error) {
	src, err := p.sources.Get(sourceID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("ingest: source %s not found", sourceID)
		}
		return Result{}, fmt.Errorf("ingest: %w", err)
	}
	if src.Status != source.StatusOCRComplete && src.Status != source.StatusExtractComplete && src.Status != source.StatusIngested {
		return Result{}, fmt.Errorf("ingest: source %s status is %s, want ocr_complete or extract_complete", sourceID, src.Status)
	}

	// Collision-aware slug: when another source already owns the candidate slug,
	// add a short id suffix to keep slugs unique.
	title := p.resolveTitle(buildTitle(src, sourceID), sourceID)
	slug := wiki.Slug(title)

	markdownName := sourceMarkdownName(src)
	mdPath := p.sources.Path(sourceID, markdownName)
	if mdPath == "" {
		return Result{}, fmt.Errorf("ingest: invalid path for %s", sourceID)
	}
	body, err := os.ReadFile(mdPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if markdownName == "ocr.md" {
				return Result{}, fmt.Errorf("ingest: ocr.md missing for %s; run source(action=\"reprocess\", source_id=\"%s\", stages=[\"ocr\", \"ingest\"])", sourceID, sourceID)
			}
			return Result{}, fmt.Errorf("ingest: %s missing for %s; normalize the source first", markdownName, sourceID)
		}
		return Result{}, fmt.Errorf("ingest: read %s: %w", markdownName, err)
	}

	structured := parseStructuredExtract(string(body), title, slug)
	currentSlugs := []string{slug}
	if structured != nil {
		for _, section := range structured.Sections {
			currentSlugs = append(currentSlugs, section.Slug)
		}
	}

	if src.Status == source.StatusIngested && slices.Equal(src.WikiPages, currentSlugs) {
		materialized := currentSlugs[1:]
		return Result{
			Slug:              slug,
			Created:           false,
			MaterializedPages: materialized,
			PageNote:          compiledPageNote(slug, materialized, true),
		}, nil
	}

	// Wave 2.4: run extractor for graph patch; degrades to empty delta on nil extractor,
	// empty body, or extractor error.
	delta := ExtractionDelta{}
	if structured == nil {
		delta = p.extractGraphPatch(ctx, sourceID, string(body))
	}

	pageBody := buildSummaryBody(src, markdownName, delta)
	pagePromptVersion := "ingest_v1"
	if delta.TotalItems() > 0 {
		pagePromptVersion = PromptVersion()
	}
	if structured != nil {
		pageBody = buildStructuredSummaryBody(src, markdownName, delta, structured.Sections)
		pagePromptVersion = structuredPromptVersion
	}

	now := p.now().UTC().Format(time.RFC3339)
	related := derivedRelatedSlugs(delta, slug)
	if structured != nil {
		for _, section := range structured.Sections {
			related = append(related, section.Slug)
		}
	}
	page := &wiki.Page{
		Title:         title,
		Body:          pageBody,
		Category:      "sources",
		Tags:          []string{"source", string(src.Kind)},
		Sources:       []string{"source:" + sourceID},
		Related:       wiki.RelatedFromSlugs(related),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: pagePromptVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := p.wiki.WritePage(ctx, page); err != nil {
		return Result{}, fmt.Errorf("ingest: write page: %w", err)
	}

	writtenSlugs := []string{slug}
	if structured != nil {
		for _, section := range structured.Sections {
			sectionPage := buildStructuredSectionPage(src, slug, section, now)
			if err := p.wiki.WritePage(ctx, sectionPage); err != nil {
				for _, written := range writtenSlugs {
					if delErr := p.wiki.DeletePage(ctx, written); delErr != nil {
						p.logger.Warn("ingest: structured rollback delete failed", "slug", written, "err", delErr)
					}
				}
				return Result{}, fmt.Errorf("ingest: write structured section %s: %w", section.Slug, err)
			}
			writtenSlugs = append(writtenSlugs, section.Slug)
		}
	}

	touchedSlugs := p.applyExtractionDelta(ctx, sourceID, delta)
	if len(touchedSlugs) > 0 {
		p.logger.Info("ingest: multi-page touch",
			"source_id", sourceID,
			"summary_slug", slug,
			"touched", len(touchedSlugs),
			"slugs", touchedSlugs,
		)
	}

	staleSlugs := staleSlugsToDeleteMany(src.WikiPages, currentSlugs)
	if _, err := p.sources.Update(sourceID, func(s *source.Source) error {
		s.Status = source.StatusIngested
		s.WikiPages = currentSlugs
		s.Error = ""
		return nil
	}); err != nil {
		// Page is on disk; status didn't flip. Surface so the caller can retry
		// (Compile is idempotent so retry just rewrites the same page).
		return Result{Slug: slug, Created: true}, fmt.Errorf("ingest: update source status: %w", err)
	}

	for _, old := range staleSlugs {
		if err := p.wiki.DeletePage(ctx, old); err != nil {
			p.logger.Warn("ingest: deleting stale wiki page failed", "slug", old, "err", err)
		}
	}
	if p.search != nil {
		for _, pageSlug := range currentSlugs {
			if err := p.search.ReindexWikiPage(ctx, pageSlug); err != nil {
				p.logger.Warn("ingest: reindex failed; page is still readable", "slug", pageSlug, "err", err)
			}
		}
	}
	p.logger.Info("source compiled",
		"source_id", sourceID,
		"slug", slug,
		"page_count", src.PageCount,
		"stale_slugs_removed", len(staleSlugs),
	)
	return Result{
		Slug:              slug,
		Created:           true,
		MaterializedPages: currentSlugs[1:],
		PageNote:          compiledPageNote(slug, currentSlugs[1:], false),
	}, nil
}

// resolveTitle adds a short id suffix when the candidate slug is already owned
// by a different source, keeping page titles and on-disk filenames unique.
func (p *Pipeline) resolveTitle(candidate, sourceID string) string {
	existing, err := p.wiki.ReadPage(wiki.Slug(candidate))
	if err != nil {
		return candidate
	}
	if pageBelongsTo(existing, sourceID) {
		return candidate
	}
	suffix := shortID(sourceID)
	if suffix == "" {
		return candidate
	}
	return candidate + " " + suffix
}

// AfterOCR adapts Compile to the telegram.AfterOCRHook signature.
func (p *Pipeline) AfterOCR(ctx context.Context, src *source.Source) (string, error) {
	res, err := p.Compile(ctx, src.ID)
	if err != nil {
		return "", err
	}
	return res.PageNote, nil
}

// extractGraphPatch runs the optional LLM extractor; degrades to empty delta on
// nil extractor, empty body, or extractor error.
func (p *Pipeline) extractGraphPatch(ctx context.Context, sourceID, body string) ExtractionDelta {
	if p.extractor == nil || strings.TrimSpace(body) == "" {
		return ExtractionDelta{}
	}
	existing, _ := p.wiki.ListPages()
	delta, err := p.extractor.Extract(ctx, ExtractionRequest{
		SourceID:      sourceID,
		Body:          body,
		ExistingSlugs: existing,
	})
	if err != nil {
		p.logger.Warn("ingest: extractor failed, falling back to single-page summary",
			"source_id", sourceID, "err", err)
		return ExtractionDelta{}
	}
	return delta
}

// applyExtractionDelta upserts each entity and concept page from the delta.
// Per-page failures are logged and skipped (partial patch > aborting whole compile).
func (p *Pipeline) applyExtractionDelta(ctx context.Context, sourceID string, delta ExtractionDelta) []string {
	var touched []string
	for _, ent := range delta.Entities {
		if err := p.upsertEntityPage(ctx, ent, sourceID); err != nil {
			p.logger.Warn("ingest: upsert entity page failed",
				"slug", ent.Slug, "source_id", sourceID, "err", err)
			continue
		}
		touched = append(touched, ent.Slug)
	}
	for _, c := range delta.Concepts {
		if err := p.upsertConceptPage(ctx, c, sourceID); err != nil {
			p.logger.Warn("ingest: upsert concept page failed",
				"slug", c.Slug, "source_id", sourceID, "err", err)
			continue
		}
		touched = append(touched, c.Slug)
	}
	return touched
}
