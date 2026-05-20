// Package ingest compiles immutable sources (PDR §4) into wiki pages.
//
// Slice 6 ships a deterministic auto-ingest path: when a source reaches
// ocr_complete or extract_complete, the pipeline writes a compact source
// page that records metadata and links
// to raw extracted markdown without duplicating it into wiki embeddings. The
// summary page becomes the durable wiki anchor for that source — the LLM
// can later compile richer entity/concept pages with the ingest_source tool
// (or a future compile_source tool) and link them via [[source-src-...]].
//
// What this package does NOT do:
//   - LLM-driven compilation of entity/concept pages from OCR (PDR §6
//     "Update relevant entity/concept pages") — left for a later slice.
//   - Annotation-based extraction (PDR §3 v2).
//   - Ingest of non-PDF sources — text/url kinds skip the OCR step entirely
//     and need a different ingestion path.
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
//
// Wave 2.4 adds an optional Extractor that lets the pipeline expand a
// single source into a multi-page graph patch: the source summary
// itself + per-entity / per-concept wiki pages, all cross-linked.
// When Extractor is nil the pipeline falls back to the single-page summary
// compatibility behavior so deployments without an LLM extractor configured
// keep working unchanged.
type Pipeline struct {
	sources   source.Repository
	wiki      wiki.Repository
	search    search.WikiPageReindexer // optional; nil when embeddings aren't configured
	extractor Extractor                // optional; nil disables multi-page touch
	logger    *slog.Logger
	now       func() time.Time
}

// Config wires the pipeline to existing stores.
type Config struct {
	Sources source.Repository
	Wiki    wiki.Repository
	Search  search.WikiPageReindexer
	Logger  *slog.Logger
	// Now is overridable for tests so created_at/updated_at are deterministic.
	Now func() time.Time
	// Extractor turns a source body into a structured graph patch (entities,
	// concepts, links). When nil, Compile writes only the source summary
	// page — backwards-compatible with the pre-Wave-2.4 deterministic
	// behavior. Production deployments that have an LLM client configured
	// for ingest should set this; tests typically leave it nil.
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
	Slug     string // wiki slug of the summary page, always set on success
	Created  bool   // true on first compile, false when the source was already ingested
	PageNote string // user-facing one-liner suitable for Telegram progress UX
}

// Compile writes (or refreshes) the source summary page for sourceID and
// flips the source status to ingested. It is idempotent: a second call on
// an already-ingested source returns the existing slug and Created=false
// without rewriting the page.
//
// Errors out when the source is missing, status != ocr_complete (and not
// already ingested at the same slug), or ocr.md is missing. When the
// source is already ingested but the freshly-computed slug differs from
// what's stored (e.g. the renderer slug rule changed, or the source was
// renamed), the page is rewritten at the new slug and the old slug is
// best-effort deleted so the wiki doesn't accumulate dead pages.
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

	// Collision-aware: when a different source already owns the candidate
	// slug, the title gets a short id suffix so the slug derived from it
	// stays unique. Slug is always wiki.Slug(title) so the wiki repository
	// WritePage implementation and our recorded slug never disagree.
	title := p.resolveTitle(buildTitle(src, sourceID), sourceID)
	slug := wiki.Slug(title)

	if src.Status == source.StatusIngested && len(src.WikiPages) == 1 && src.WikiPages[0] == slug {
		return Result{
			Slug:     slug,
			Created:  false,
			PageNote: fmt.Sprintf("already compiled as [[%s]]", slug),
		}, nil
	}

	markdownName := sourceMarkdownName(src)
	mdPath := p.sources.Path(sourceID, markdownName)
	if mdPath == "" {
		return Result{}, fmt.Errorf("ingest: invalid path for %s", sourceID)
	}
	_, err = os.ReadFile(mdPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if markdownName == "ocr.md" {
				return Result{}, fmt.Errorf("ingest: ocr.md missing for %s; run ocr_source first", sourceID)
			}
			return Result{}, fmt.Errorf("ingest: %s missing for %s; normalize the source first", markdownName, sourceID)
		}
		return Result{}, fmt.Errorf("ingest: read %s: %w", markdownName, err)
	}

	// Wave 2.4 — multi-page touch: when an Extractor is configured,
	// run a single LLM call to derive entities + concepts + links from
	// the source body. The resulting delta enriches the source summary
	// page (entity/concept refs appear in its `related:` array and
	// body) and drives per-entity / per-concept page upserts after
	// the source summary is written. On extractor failure the pipeline
	// degrades to the single-page compatibility behavior — better one page
	// than no ingest at all.
	body, _ := os.ReadFile(mdPath)
	delta := p.extractGraphPatch(ctx, sourceID, string(body))

	pageBody := buildSummaryBody(src, markdownName, delta)
	pagePromptVersion := "ingest_v1"
	if delta.TotalItems() > 0 {
		pagePromptVersion = PromptVersion()
	}

	now := p.now().UTC().Format(time.RFC3339)
	related := derivedRelatedSlugs(delta, slug)
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

	// Wave 2.4 — multi-page touch: write or merge each extracted
	// entity/concept page after the source summary lands. Failures on
	// individual pages are logged and skipped: a partial graph patch
	// is more useful than aborting the whole compile because one
	// downstream page failed validation.
	touchedSlugs := p.applyExtractionDelta(ctx, sourceID, delta)
	if len(touchedSlugs) > 0 {
		p.logger.Info("ingest: multi-page touch",
			"source_id", sourceID,
			"summary_slug", slug,
			"touched", len(touchedSlugs),
			"slugs", touchedSlugs,
		)
	}

	staleSlugs := staleSlugsToDelete(src.WikiPages, slug)

	if _, err := p.sources.Update(sourceID, func(s *source.Source) error {
		s.Status = source.StatusIngested
		s.WikiPages = []string{slug}
		s.Error = ""
		return nil
	}); err != nil {
		// Page is on disk; status didn't flip. Surface the failure so the
		// caller can retry — Compile is idempotent so a retry just rewrites
		// the same page.
		return Result{Slug: slug, Created: true}, fmt.Errorf("ingest: update source status: %w", err)
	}

	for _, old := range staleSlugs {
		if err := p.wiki.DeletePage(ctx, old); err != nil {
			p.logger.Warn("ingest: deleting stale wiki page failed", "slug", old, "err", err)
		}
	}

	if p.search != nil {
		if err := p.search.ReindexWikiPage(ctx, slug); err != nil {
			p.logger.Warn("ingest: reindex failed; page is still readable", "slug", slug, "err", err)
		}
	}

	p.logger.Info("source compiled",
		"source_id", sourceID,
		"slug", slug,
		"page_count", src.PageCount,
		"stale_slugs_removed", len(staleSlugs),
	)

	return Result{
		Slug:     slug,
		Created:  true,
		PageNote: fmt.Sprintf("compiled as [[%s]]", slug),
	}, nil
}

// resolveTitle returns the title to use for this source's wiki page,
// disambiguating with a short id suffix when the candidate slug is
// already owned by a different source. Returning a title (rather than a
// slug) keeps page.Title and the on-disk filename in sync since
// the wiki repository WritePage implementation keys off the title.
func (p *Pipeline) resolveTitle(candidate, sourceID string) string {
	existing, err := p.wiki.ReadPage(wiki.Slug(candidate))
	if err != nil {
		// Not found / unreadable / parse error → slug is free. Read errors
		// are rare and the WritePage call later will surface real problems.
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

// pageBelongsTo reports whether a wiki page's frontmatter sources list
// already references sourceID. Used so re-running Compile on the same
// source reuses the existing slug instead of generating a new one.
func pageBelongsTo(page *wiki.Page, sourceID string) bool {
	if page == nil {
		return false
	}
	return slices.Contains(page.Sources, "source:"+sourceID)
}

// shortID returns the first 6 hex chars after the "src_" prefix so the
// disambiguating suffix stays human-readable. Empty when the id doesn't
// fit the expected shape.
func shortID(sourceID string) string {
	const prefix = "src_"
	if !strings.HasPrefix(sourceID, prefix) {
		return ""
	}
	rest := sourceID[len(prefix):]
	if len(rest) < 6 {
		return rest
	}
	return rest[:6]
}

// staleSlugsToDelete returns the previously-recorded slugs that no longer
// match the freshly-computed slug. Used to clean up after a slug-rule
// change or filename rename so the wiki doesn't accumulate dead pages.
func staleSlugsToDelete(prev []string, current string) []string {
	out := make([]string, 0, len(prev))
	for _, s := range prev {
		if s != "" && s != current {
			out = append(out, s)
		}
	}
	return out
}

// buildTitle returns a human-readable wiki title derived from the source's
// display filename, falling back to the source ID when no filename is
// available. The "Source: " prefix keeps source pages clustered under a
// predictable namespace in graph and search views.
func buildTitle(src *source.Source, sourceID string) string {
	name := strings.TrimSpace(src.Filename)
	if ext := strings.LastIndex(name, "."); ext > 0 {
		name = strings.TrimSpace(name[:ext])
	}
	if name == "" {
		return "Source: " + sourceID
	}
	return "Source: " + name
}

// AfterOCR adapts Compile to the telegram.AfterOCRHook signature so the bot
// can plug the pipeline directly into docHandlerConfig.AfterOCR. The
// returned note is appended to the final Telegram progress message.
func (p *Pipeline) AfterOCR(ctx context.Context, src *source.Source) (string, error) {
	res, err := p.Compile(ctx, src.ID)
	if err != nil {
		return "", err
	}
	return res.PageNote, nil
}

// buildSummaryBody emits the deterministic body for a source summary page.
// Body schema is intentionally simple (PDR §8: "LLM must not dump full OCR
// text"): metadata block, raw-OCR pointer, optional preview block. Future
// LLM-driven enrichment can add sections without breaking this layout.
func sourceMarkdownName(src *source.Source) string {
	if src != nil && src.Kind == source.KindPDF {
		return "ocr.md"
	}
	return source.ExtractMarkdownFile
}

func buildSummaryBody(src *source.Source, markdownName string, delta ExtractionDelta) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Source: %s\n\n", displayFilename(src))
	sb.WriteString("Auto-generated by the ingest pipeline. Edit freely; rerunning ingest_source on this source will refresh this page.\n\n")
	sb.WriteString("## Metadata\n\n")
	fmt.Fprintf(&sb, "- Source ID: `%s`\n", src.ID)
	fmt.Fprintf(&sb, "- Filename: %s\n", src.Filename)
	fmt.Fprintf(&sb, "- Kind: %s\n", src.Kind)
	// Compile only renders on success and the status flip happens right after
	// WritePage; render the post-flip value so the page never disagrees with
	// source.json.
	fmt.Fprintf(&sb, "- Status: %s\n", source.StatusIngested)
	fmt.Fprintf(&sb, "- Size: %d bytes\n", src.SizeBytes)
	fmt.Fprintf(&sb, "- SHA256: `%s`\n", src.SHA256)
	if src.OCRModel != "" {
		fmt.Fprintf(&sb, "- OCR model: %s\n", src.OCRModel)
	}
	if src.PageCount > 0 {
		fmt.Fprintf(&sb, "- Pages: %d\n", src.PageCount)
	}
	fmt.Fprintf(&sb, "- Stored: %s\n", src.CreatedAt.UTC().Format(time.RFC3339))

	// Wave 2.4: when extraction produced entities or concepts, list them
	// here as [[wiki-links]] so the source summary becomes a real hub
	// page in the knowledge graph rather than a metadata-only stub.
	// Wave 2.1 backlink maintenance will mirror these references into
	// the targets' `related:` arrays on write.
	if len(delta.Entities) > 0 {
		sb.WriteString("\n## Extracted Entities\n\n")
		for _, ent := range delta.Entities {
			fmt.Fprintf(&sb, "- [[%s]] — %s (%s) — %s\n", ent.Slug, ent.Title, ent.Type, ent.ShortDescription)
		}
	}
	if len(delta.Concepts) > 0 {
		sb.WriteString("\n## Extracted Concepts\n\n")
		for _, c := range delta.Concepts {
			fmt.Fprintf(&sb, "- [[%s]] — %s — %s\n", c.Slug, c.Title, c.Summary)
		}
	}

	sb.WriteString("\n## Extracted Markdown\n\n")
	fmt.Fprintf(&sb, "Full extracted markdown lives at `wiki/raw/%s/%s` (read via the `read_source` tool).\n", src.ID, markdownName)

	return sb.String()
}

// extractGraphPatch runs the optional LLM extractor on the source body.
// Returns an empty delta when no extractor is configured, when the
// extractor errors, or when the body is empty. The pipeline degrades
// gracefully to single-page-summary behavior in all of those cases.
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

// derivedRelatedSlugs collects all entity + concept slugs from the
// delta and removes the source summary's own slug so the page doesn't
// list itself. Order is stable (entities first, then concepts) for
// deterministic git diffs across re-ingests.
func derivedRelatedSlugs(delta ExtractionDelta, ownSlug string) []string {
	out := make([]string, 0, delta.TotalItems())
	for _, ent := range delta.Entities {
		if ent.Slug != ownSlug {
			out = append(out, ent.Slug)
		}
	}
	for _, c := range delta.Concepts {
		if c.Slug != ownSlug {
			out = append(out, c.Slug)
		}
	}
	return out
}

// applyExtractionDelta upserts each entity and concept page from the
// delta. New pages are created; existing pages get a new "From source:..."
// section appended to their body and the source ID added to their
// `Sources` array. Per-page failures are logged and skipped so a
// single bad entity doesn't abort the whole multi-page-touch pass.
// Returns the slugs that were successfully written for caller diagnostics.
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

// upsertEntityPage creates a fresh entity page or merges a new
// "From source:..." citation block into an existing one. The
// idempotency check `sources contains "source:" + sourceID` short-
// circuits re-ingests of the same source so a Compile rerun is a
// no-op for unchanged extractions.
func (p *Pipeline) upsertEntityPage(ctx context.Context, ent ExtractedEntity, sourceID string) error {
	sourceTag := "source:" + sourceID
	now := p.now().UTC().Format(time.RFC3339)

	existing, err := p.wiki.ReadPage(ent.Slug)
	if err != nil {
		// Page doesn't exist — create.
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

	// Page exists — merge if this source hasn't been cited yet.
	if containsString(existing.Sources, sourceTag) {
		return nil // idempotent re-ingest, nothing to do
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

// upsertConceptPage mirrors upsertEntityPage for concept pages. The
// key_claims block, when present, is appended as a "## Key Claims"
// section with each claim as a verbatim quote so provenance is
// preserved even after the source body is no longer in context.
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

// buildEntityBody renders the markdown body for a freshly-created
// entity page. The single source citation block is the seed; later
// ingests from other sources add additional "From source:..."
// sections via the merge path in upsertEntityPage. Every extracted
// claim ends with a Pandoc-style provenance marker `^[src_xxx]`
// (Wave 2.5) so when claims from multiple sources get interleaved
// during merges, each line still says where it came from.
func buildEntityBody(ent ExtractedEntity, sourceID string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", ent.Title)
	fmt.Fprintf(&sb, "_Type: %s — extracted from sources._\n\n", ent.Type)
	sb.WriteString("## From source:")
	sb.WriteString(sourceID)
	sb.WriteString("\n\n")
	sb.WriteString(annotateProvenance(ent.ShortDescription, sourceID))
	sb.WriteString("\n")
	return sb.String()
}

// buildConceptBody renders the markdown body for a freshly-created
// concept page. Each statement (summary + every key claim) is tagged
// with a `^[src_xxx]` provenance marker so retrieval and downstream
// lint can trace any claim back to its source without re-reading the
// raw markdown.
func buildConceptBody(c ExtractedConcept, sourceID string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", c.Title)
	sb.WriteString("_Theme extracted from sources._\n\n")
	sb.WriteString("## From source:")
	sb.WriteString(sourceID)
	sb.WriteString("\n\n")
	sb.WriteString(annotateProvenance(c.Summary, sourceID))
	sb.WriteString("\n")
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

// annotateProvenance is a thin alias for wiki.AnnotateProvenance kept
// here so existing call sites in this file don't churn. The shared
// implementation lives in internal/wiki/provenance.go so the
// wiki_page tool (Wave 2.6) and the ingest pipeline emit the same
// marker syntax. Re-exporting at the function level keeps the diff
// to call sites minimal.
func annotateProvenance(text, sourceID string) string {
	return wiki.AnnotateProvenance(text, sourceID)
}

// provenanceMarker is the same thin-alias treatment for the marker
// helper. See annotateProvenance above.
func provenanceMarker(sourceID string) string {
	return wiki.ProvenanceMarker(sourceID)
}

// cleanedRelated returns a copy of related slugs with the owner's
// own slug removed (self-references in `related:` confuse downstream
// graph traversal) and obvious empties dropped.
func cleanedRelated(related []string, ownSlug string) []string {
	out := make([]string, 0, len(related))
	for _, r := range related {
		r = strings.TrimSpace(r)
		if r == "" || r == ownSlug {
			continue
		}
		out = append(out, r)
	}
	return out
}

// containsString is a tiny helper kept local to avoid importing
// `slices` just for this — and to keep the linter from suggesting
// `slices.Contains` everywhere on Go 1.21 build targets.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// displayFilename returns a human label for the source. Falls back to the
// source ID when the filename is empty or visually noisy.
func displayFilename(src *source.Source) string {
	name := strings.TrimSpace(src.Filename)
	if name == "" {
		return src.ID
	}
	return name
}

// buildPreview extracts the first chunk of substantive OCR markdown by
// skipping the rendered header lines (`# Source OCR: ...`, `Source ID:`,
// `Model:`, `## Page 1`) emitted by internal/ocr/render.go.
func buildPreview(rawMD string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	lines := strings.Split(rawMD, "\n")
	contentStart := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "## Page") {
			contentStart = i + 1
			break
		}
	}
	if contentStart == 0 {
		// No page header → assume the whole file is body.
		contentStart = 0
	}
	rest := strings.TrimSpace(strings.Join(lines[contentStart:], "\n"))
	if rest == "" {
		return ""
	}
	if len(rest) <= maxChars {
		return rest
	}
	// Cut at a UTF-8 boundary so we don't slice a multibyte rune in half.
	cut := maxChars
	for cut > 0 && (rest[cut]&0xC0) == 0x80 {
		cut--
	}
	return strings.TrimRight(rest[:cut], " \n\t") + "…"
}
