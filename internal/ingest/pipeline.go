// Package ingest compiles immutable sources (PDR §4) into wiki pages.
//
// Slice 6 ships a deterministic auto-ingest path: when a PDF reaches
// status=ocr_complete (via Telegram upload or the ocr_source tool) the
// pipeline writes a "Source <id>" summary page that records metadata, links
// to the raw OCR file, and embeds a short preview of the OCR markdown. The
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

	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

// previewMaxChars caps the OCR-body excerpt embedded in the summary page so
// the page stays small (§8 "LLM must not dump full OCR text into a wiki
// page"). Pick a window that gives the LLM enough signal to decide whether
// to drill deeper via read_source.
const previewMaxChars = 1000

// Pipeline turns ocr_complete sources into wiki summary pages.
type Pipeline struct {
	sources *source.Store
	wiki    *wiki.Store
	search  *search.Engine // optional; nil when embeddings aren't configured
	logger  *slog.Logger
	now     func() time.Time
}

// Config wires the pipeline to existing stores.
type Config struct {
	Sources *source.Store
	Wiki    *wiki.Store
	Search  *search.Engine
	Logger  *slog.Logger
	// Now is overridable for tests so created_at/updated_at are deterministic.
	Now func() time.Time
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
		sources: cfg.Sources,
		wiki:    cfg.Wiki,
		search:  cfg.Search,
		logger:  logger,
		now:     now,
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

	if src.Status != source.StatusOCRComplete && src.Status != source.StatusIngested {
		return Result{}, fmt.Errorf("ingest: source %s status is %s, want ocr_complete", sourceID, src.Status)
	}

	// Collision-aware: when a different source already owns the candidate
	// slug, the title gets a short id suffix so the slug derived from it
	// stays unique. Slug is always wiki.Slug(title) so wiki.Store.WritePage
	// (which keys off the title) and our recorded slug never disagree.
	title := p.resolveTitle(buildTitle(src, sourceID), sourceID)
	slug := wiki.Slug(title)

	if src.Status == source.StatusIngested && len(src.WikiPages) == 1 && src.WikiPages[0] == slug {
		return Result{
			Slug:     slug,
			Created:  false,
			PageNote: fmt.Sprintf("already compiled as [[%s]]", slug),
		}, nil
	}

	mdPath := p.sources.Path(sourceID, "ocr.md")
	if mdPath == "" {
		return Result{}, fmt.Errorf("ingest: invalid path for %s", sourceID)
	}
	rawMD, err := os.ReadFile(mdPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("ingest: ocr.md missing for %s; run ocr_source first", sourceID)
		}
		return Result{}, fmt.Errorf("ingest: read ocr.md: %w", err)
	}

	preview := buildPreview(string(rawMD), previewMaxChars)
	body := buildSummaryBody(src, preview)

	now := p.now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		Category:      "sources",
		Tags:          []string{"source", string(src.Kind)},
		Sources:       []string{"source:" + sourceID},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := p.wiki.WritePage(ctx, page); err != nil {
		return Result{}, fmt.Errorf("ingest: write page: %w", err)
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
		"preview_chars", len(preview),
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
// wiki.Store.WritePage keys off the title.
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
func buildSummaryBody(src *source.Source, preview string) string {
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

	sb.WriteString("\n## Raw OCR\n\n")
	fmt.Fprintf(&sb, "Full extracted markdown lives at `wiki/raw/%s/ocr.md` (read via the `read_source` tool).\n", src.ID)

	if preview != "" {
		sb.WriteString("\n## Preview\n\n")
		sb.WriteString(preview)
		sb.WriteString("\n")
	}

	return sb.String()
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
