// Package tools — source.go holds the shared helpers used by every source-*
// tool: byte caps, the bounded file reader, the formatters for metadata /
// list / lint output, and readSourceMarkdown which falls back from ocr.md to
// the stored original. Each LLM tool lives in its own file:
//
//   - source_store.go  — store_source
//   - source_ocr.go    — ocr_source
//   - source_read.go   — read_source
//   - source_list.go   — list_sources + lint_sources
//   - source_delete.go — delete_source
//
// See F-047 in the 2026-05-11 tools audit for the split rationale.
package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/source"
)

// Output caps for source-reading tools. Bound the LLM context budget the same
// way web_search/web_fetch tools do via maxWebToolChars.
const (
	maxSourceToolChars  = 8000
	excerptDefaultBytes = 4000

	// maxOCRPDFBytes bounds the in-RAM PDF buffer fed to the OCR client. A
	// hostile uploader pushing a 500MB PDF would otherwise OOM the bot.
	maxOCRPDFBytes = 64 << 20 // 64 MiB

	// maxSourceReadBytes bounds os.Open + io.ReadAll when serving a source's
	// original/extracted body to the LLM. We overshoot the visible byte cap
	// (maxSourceToolChars) so truncation still has room to find a clean
	// boundary, but we never load an unbounded file into memory.
	maxSourceReadBytes = 1 << 20 // 1 MiB
)

// readBoundedFile opens path and reads up to maxBytes+1 to detect overflow.
// Refuses to allocate an unbounded buffer for an LLM-controlled source ID —
// the previous os.ReadFile path could OOM the bot on a malicious upload.
func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d byte cap", maxBytes)
	}
	return body, nil
}

// readSourceMarkdown returns ocr.md when present, else falls back to the
// stored original (text/url kinds) so the LLM can read non-PDF sources too.
func readSourceMarkdown(store source.FileResolver, src *source.Source, maxBytes int) (string, error) {
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
	case source.KindSandboxArtifact:
		if !isReadableSandboxArtifact(src) {
			return "", fmt.Errorf("read_source: sandbox artifact %s is not text-readable (mime=%s)", src.ID, src.MimeType)
		}
		return readOriginalContent(store, src.ID, source.OriginalFilenameForKind(src.Kind, src.Filename), maxBytes)
	}
	return "", fmt.Errorf("read_source: ocr.md not found for %s (status=%s); run ocr_source first", src.ID, src.Status)
}

func isReadableSandboxArtifact(src *source.Source) bool {
	mime := strings.ToLower(strings.TrimSpace(src.MimeType))
	if strings.HasPrefix(mime, "text/") || strings.Contains(mime, "json") || strings.Contains(mime, "xml") {
		return true
	}
	switch strings.ToLower(filepath.Ext(src.Filename)) {
	case ".txt", ".md", ".markdown", ".csv", ".json", ".py", ".html", ".xml", ".yaml", ".yml", ".log":
		return true
	default:
		return false
	}
}

func readOriginalContent(store source.FileResolver, id, name string, maxBytes int) (string, error) {
	path := store.Path(id, name)
	if path == "" {
		return "", fmt.Errorf("read_source: invalid path for %s", id)
	}
	// Cap the file read to maxSourceReadBytes — much larger than the visible
	// truncation cap but still bounded, so a multi-GB sandbox_artifact can't
	// OOM the bot when an LLM asks to read it.
	raw, err := readBoundedFile(path, int64(maxSourceReadBytes))
	if err != nil {
		return "", fmt.Errorf("read_source: %w", err)
	}
	return truncateForToolContext(string(raw), maxBytes), nil
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
