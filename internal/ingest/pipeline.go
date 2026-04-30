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
// already ingested), or ocr.md is missing.
func (p *Pipeline) Compile(ctx context.Context, sourceID string) (Result, error) {
	src, err := p.sources.Get(sourceID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("ingest: source %s not found", sourceID)
		}
		return Result{}, fmt.Errorf("ingest: %w", err)
	}

	if src.Status == source.StatusIngested && len(src.WikiPages) > 0 {
		slug := src.WikiPages[0]
		return Result{
			Slug:     slug,
			Created:  false,
			PageNote: fmt.Sprintf("already compiled as [[%s]]", slug),
		}, nil
	}
	if src.Status != source.StatusOCRComplete {
		return Result{}, fmt.Errorf("ingest: source %s status is %s, want ocr_complete", sourceID, src.Status)
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

	// Title is keyed off the source ID so two PDFs with the same display
	// filename can't collide. The human-readable filename lives in the body.
	title := "Source " + sourceID
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

	slug := wiki.Slug(title)

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
	)

	return Result{
		Slug:     slug,
		Created:  true,
		PageNote: fmt.Sprintf("compiled as [[%s]]", slug),
	}, nil
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
	fmt.Fprintf(&sb, "- Status: %s\n", src.Status)
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
