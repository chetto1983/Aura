package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/source"
)

// Output caps for source-reading tools. Bound the LLM context budget the same
// way web tools do (see ollama_web.go: maxWebToolChars=8000).
const (
	maxSourceToolChars  = 8000
	excerptDefaultBytes = 4000
)

// StoreSourceTool stores text or a URL as an immutable source.
//
// PDFs are stored automatically by the Telegram document handler (slice 4);
// the LLM cannot stream binary content through tool calls, so we deliberately
// do not expose a "pdf" mode here.
type StoreSourceTool struct {
	store *source.Store
}

func NewStoreSourceTool(store *source.Store) *StoreSourceTool {
	return &StoreSourceTool{store: store}
}

func (t *StoreSourceTool) Name() string { return "store_source" }

func (t *StoreSourceTool) Description() string {
	return "Store text or a URL as an immutable source for later ingest. PDFs are stored automatically when uploaded via Telegram."
}

func (t *StoreSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"description": "Source kind: 'text' or 'url'.",
				"enum":        []string{"text", "url"},
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Display filename or short label (e.g. 'meeting-notes.txt' or 'arxiv-paper').",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "For kind='text': the text body. For kind='url': the absolute URL.",
			},
		},
		"required": []string{"kind", "filename", "content"},
	}
}

func (t *StoreSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("store_source: source store unavailable")
	}
	kindArg, err := requiredString(args, "kind")
	if err != nil {
		return "", err
	}
	filename, err := requiredString(args, "filename")
	if err != nil {
		return "", err
	}
	content, err := requiredString(args, "content")
	if err != nil {
		return "", err
	}

	var (
		kind source.Kind
		mime string
	)
	switch kindArg {
	case "text":
		kind = source.KindText
		mime = "text/plain; charset=utf-8"
	case "url":
		kind = source.KindURL
		mime = "text/x-uri"
	default:
		return "", fmt.Errorf("store_source: unsupported kind %q", kindArg)
	}

	src, dup, err := t.store.Put(ctx, source.PutInput{
		Kind:     kind,
		Filename: filename,
		MimeType: mime,
		Bytes:    []byte(content),
	})
	if err != nil {
		return "", fmt.Errorf("store_source: %w", err)
	}

	verb := "Stored"
	if dup {
		verb = "Already stored"
	}
	return fmt.Sprintf("%s source %s · kind=%s · status=%s · sha256=%s",
		verb, src.ID, src.Kind, src.Status, src.SHA256[:16]), nil
}

// OCRSourceTool runs Mistral OCR over a stored PDF source. Mirrors the
// pipeline in internal/telegram/documents.go but is callable by the LLM —
// useful when an upload was queued before OCR was enabled, or to retry a
// failed source.
type OCRSourceTool struct {
	store *source.Store
	ocr   *ocr.Client
}

func NewOCRSourceTool(store *source.Store, client *ocr.Client) *OCRSourceTool {
	return &OCRSourceTool{store: store, ocr: client}
}

func (t *OCRSourceTool) Name() string { return "ocr_source" }

func (t *OCRSourceTool) Description() string {
	return "Run Mistral OCR over a stored PDF source by source_id. Writes ocr.md and ocr.json next to the source and updates status to ocr_complete."
}

func (t *OCRSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "string",
				"description": "Source ID (e.g. src_<16hex>).",
			},
		},
		"required": []string{"source_id"},
	}
}

func (t *OCRSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("ocr_source: source store unavailable")
	}
	if t.ocr == nil {
		return "", errors.New("ocr_source: OCR is disabled (set OCR_ENABLED=true and MISTRAL_API_KEY)")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}
	src, err := t.store.Get(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("ocr_source: source %s not found", id)
		}
		return "", fmt.Errorf("ocr_source: %w", err)
	}
	if src.Kind != source.KindPDF {
		return "", fmt.Errorf("ocr_source: only PDF sources can be OCRed (got %s)", src.Kind)
	}

	pdfPath := t.store.Path(id, "original.pdf")
	if pdfPath == "" {
		return "", fmt.Errorf("ocr_source: invalid source path for %s", id)
	}
	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("ocr_source: read pdf: %w", err)
	}

	start := time.Now()
	res, err := t.ocr.Process(ctx, ocr.ProcessInput{PDFBytes: pdfBytes})
	if err != nil {
		_, _ = t.store.Update(id, func(s *source.Source) error {
			s.Status = source.StatusFailed
			s.Error = err.Error()
			return nil
		})
		return "", fmt.Errorf("ocr_source: %w", err)
	}
	elapsed := time.Since(start)

	mdPath := t.store.Path(id, "ocr.md")
	jsonPath := t.store.Path(id, "ocr.json")
	if mdPath == "" || jsonPath == "" {
		return "", fmt.Errorf("ocr_source: invalid output path for %s", id)
	}

	md := ocr.RenderMarkdown(ocr.RenderMeta{
		SourceID: id,
		Filename: src.Filename,
		Model:    res.Response.Model,
	}, res.Response)

	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("ocr_source: write ocr.md: %w", err)
	}
	if err := os.WriteFile(jsonPath, res.RawJSON, 0o644); err != nil {
		return "", fmt.Errorf("ocr_source: write ocr.json: %w", err)
	}

	pageCount := len(res.Response.Pages)
	if res.Response.UsageInfo != nil && res.Response.UsageInfo.PagesProcessed > 0 {
		pageCount = res.Response.UsageInfo.PagesProcessed
	}

	if _, err := t.store.Update(id, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.OCRModel = res.Response.Model
		s.PageCount = pageCount
		s.Error = ""
		return nil
	}); err != nil {
		return "", fmt.Errorf("ocr_source: update metadata: %w", err)
	}

	return fmt.Sprintf("OCR complete · %s · %d page(s) · %s · model=%s",
		id, pageCount, formatToolDuration(elapsed), res.Response.Model), nil
}

// ReadSourceTool reads source metadata or extracted markdown.
type ReadSourceTool struct {
	store *source.Store
}

func NewReadSourceTool(store *source.Store) *ReadSourceTool {
	return &ReadSourceTool{store: store}
}

func (t *ReadSourceTool) Name() string { return "read_source" }

func (t *ReadSourceTool) Description() string {
	return "Read source metadata or extracted markdown by source ID. Modes: metadata, ocr (full ocr.md, capped at 8000 chars), excerpt (first ~4000 chars)."
}

func (t *ReadSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "string",
				"description": "Source ID (e.g. src_<16hex>).",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "metadata, ocr, or excerpt. Defaults to excerpt.",
				"enum":        []string{"metadata", "ocr", "excerpt"},
			},
		},
		"required": []string{"source_id"},
	}
}

func (t *ReadSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("read_source: source store unavailable")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(stringArg(args, "mode"))
	if mode == "" {
		mode = "excerpt"
	}

	src, err := t.store.Get(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read_source: source %s not found", id)
		}
		return "", fmt.Errorf("read_source: %w", err)
	}

	switch mode {
	case "metadata":
		return formatSourceMetadata(src), nil
	case "ocr":
		return readSourceMarkdown(t.store, src, maxSourceToolChars)
	case "excerpt":
		return readSourceMarkdown(t.store, src, excerptDefaultBytes)
	default:
		return "", fmt.Errorf("read_source: unsupported mode %q", mode)
	}
}

// readSourceMarkdown returns ocr.md when present, else falls back to the
// stored original (text/url kinds) so the LLM can read non-PDF sources too.
func readSourceMarkdown(store *source.Store, src *source.Source, maxBytes int) (string, error) {
	mdPath := store.Path(src.ID, "ocr.md")
	if mdPath == "" {
		return "", fmt.Errorf("read_source: invalid path for %s", src.ID)
	}
	raw, err := os.ReadFile(mdPath)
	if err == nil {
		return truncateForToolContext(string(raw), maxBytes), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read_source: %w", err)
	}

	switch src.Kind {
	case source.KindText:
		return readOriginalContent(store, src.ID, "original.txt", maxBytes)
	case source.KindURL:
		return readOriginalContent(store, src.ID, "original.url", maxBytes)
	}
	return "", fmt.Errorf("read_source: ocr.md not found for %s (status=%s); run ocr_source first", src.ID, src.Status)
}

func readOriginalContent(store *source.Store, id, name string, maxBytes int) (string, error) {
	path := store.Path(id, name)
	if path == "" {
		return "", fmt.Errorf("read_source: invalid path for %s", id)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_source: %w", err)
	}
	return truncateForToolContext(string(raw), maxBytes), nil
}

// ListSourcesTool lists sources matching optional kind/status filters.
type ListSourcesTool struct {
	store *source.Store
}

func NewListSourcesTool(store *source.Store) *ListSourcesTool {
	return &ListSourcesTool{store: store}
}

func (t *ListSourcesTool) Name() string { return "list_sources" }

func (t *ListSourcesTool) Description() string {
	return "List stored sources, newest first. Optional filters: kind (pdf/text/url), status (stored/ocr_complete/ingested/failed)."
}

func (t *ListSourcesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"description": "Filter by kind.",
				"enum":        []string{"pdf", "text", "url"},
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Filter by status.",
				"enum":        []string{"stored", "ocr_complete", "ingested", "failed"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum sources to return (default 20, max 100).",
				"minimum":     1,
				"maximum":     100,
			},
		},
	}
}

func (t *ListSourcesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("list_sources: source store unavailable")
	}
	filter := source.ListFilter{
		Kind:   source.Kind(strings.TrimSpace(stringArg(args, "kind"))),
		Status: source.Status(strings.TrimSpace(stringArg(args, "status"))),
	}
	limit := intArg(args, "limit", 20, 1, 100)

	rows, err := t.store.List(filter)
	if err != nil {
		return "", fmt.Errorf("list_sources: %w", err)
	}
	truncated := false
	if len(rows) > limit {
		rows = rows[:limit]
		truncated = true
	}
	return formatSourceList(rows, filter, truncated), nil
}

// LintSourcesTool reports sources that need attention.
type LintSourcesTool struct {
	store *source.Store
}

func NewLintSourcesTool(store *source.Store) *LintSourcesTool {
	return &LintSourcesTool{store: store}
}

func (t *LintSourcesTool) Name() string { return "lint_sources" }

func (t *LintSourcesTool) Description() string {
	return "Report sources needing attention: stored but not OCRed, OCRed but not ingested, and failed sources."
}

func (t *LintSourcesTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *LintSourcesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("lint_sources: source store unavailable")
	}
	rows, err := t.store.List(source.ListFilter{})
	if err != nil {
		return "", fmt.Errorf("lint_sources: %w", err)
	}
	return formatSourceLint(rows), nil
}

func formatSourceMetadata(s *source.Source) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Source %s\n\n", s.ID)
	fmt.Fprintf(&sb, "Filename: %s\n", s.Filename)
	fmt.Fprintf(&sb, "Kind: %s\n", s.Kind)
	fmt.Fprintf(&sb, "Status: %s\n", s.Status)
	fmt.Fprintf(&sb, "MIME: %s\n", s.MimeType)
	fmt.Fprintf(&sb, "Size: %d bytes\n", s.SizeBytes)
	fmt.Fprintf(&sb, "SHA256: %s\n", s.SHA256)
	fmt.Fprintf(&sb, "Created: %s\n", s.CreatedAt.UTC().Format(time.RFC3339))
	if s.OCRModel != "" {
		fmt.Fprintf(&sb, "OCR model: %s\n", s.OCRModel)
	}
	if s.PageCount > 0 {
		fmt.Fprintf(&sb, "Pages: %d\n", s.PageCount)
	}
	if len(s.WikiPages) > 0 {
		fmt.Fprintf(&sb, "Wiki pages: %s\n", strings.Join(s.WikiPages, ", "))
	}
	if s.Error != "" {
		fmt.Fprintf(&sb, "Last error: %s\n", s.Error)
	}
	return sb.String()
}

func formatSourceList(rows []*source.Source, filter source.ListFilter, truncated bool) string {
	if len(rows) == 0 {
		return "No sources match the filter."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Sources (%d", len(rows))
	var crit []string
	if filter.Kind != "" {
		crit = append(crit, "kind="+string(filter.Kind))
	}
	if filter.Status != "" {
		crit = append(crit, "status="+string(filter.Status))
	}
	if len(crit) > 0 {
		fmt.Fprintf(&sb, "; %s", strings.Join(crit, ", "))
	}
	if truncated {
		sb.WriteString("; truncated")
	}
	sb.WriteString("):\n")

	for _, s := range rows {
		fmt.Fprintf(&sb, "- %s · %s · %s · %s · %s",
			s.ID,
			s.Kind,
			s.Status,
			s.CreatedAt.UTC().Format(time.RFC3339),
			s.Filename,
		)
		if s.PageCount > 0 {
			fmt.Fprintf(&sb, " · %d page(s)", s.PageCount)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatSourceLint(rows []*source.Source) string {
	type bucket struct {
		title string
		items []*source.Source
	}
	order := []source.Status{source.StatusStored, source.StatusOCRComplete, source.StatusFailed}
	titles := map[source.Status]string{
		source.StatusStored:      "Stored, awaiting OCR",
		source.StatusOCRComplete: "OCR complete, awaiting ingest",
		source.StatusFailed:      "Failed",
	}
	buckets := make(map[source.Status]*bucket, len(order))
	for _, status := range order {
		buckets[status] = &bucket{title: titles[status]}
	}
	for _, s := range rows {
		if b, ok := buckets[s.Status]; ok {
			b.items = append(b.items, s)
		}
	}

	var sb strings.Builder
	total := 0
	for _, status := range order {
		b := buckets[status]
		if len(b.items) == 0 {
			continue
		}
		if total > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "## %s (%d)\n", b.title, len(b.items))
		sort.Slice(b.items, func(i, j int) bool {
			return b.items[i].CreatedAt.After(b.items[j].CreatedAt)
		})
		for _, s := range b.items {
			fmt.Fprintf(&sb, "- %s · %s · %s", s.ID, s.Kind, s.Filename)
			if s.Error != "" {
				fmt.Fprintf(&sb, " · error: %s", s.Error)
			}
			sb.WriteString("\n")
		}
		total += len(b.items)
	}
	if total == 0 {
		return "No sources need attention."
	}
	return sb.String()
}

func formatToolDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
