// Package opsfile projects data/operational_lessons.md into
// compact_memory_documents kind=operational so the recall_operational tool can
// surface lessons that Aura wrote via the file tool (not via propose_patch).
//
// The file watcher path (US-OP02) complements the propose_patch auto-accept
// path (US-OP01): both eventually write to the same store so recall_operational
// sees a unified view regardless of which write path was used.
package opsfile

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// OpsFilePath is the path relative to the workspace root where Aura stores
// operational lessons written directly via the file tool.
const OpsFilePath = "data/operational_lessons.md"

// LessonsWriter is the write side for operational memory. Satisfied by
// *memoryindex.Store in production; tests inject a fake.
type LessonsWriter interface {
	Upsert(ctx context.Context, doc memoryindex.Document) error
}

// AbsPath returns the absolute filesystem path for the operational lessons
// file given the workspace root directory.
func AbsPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, OpsFilePath)
}

// IngestFromPath reads the file at absPath and upserts each lesson section into
// store. Returns (0, nil) when the file does not exist (not an error — Aura may
// not have created it yet). Returns (n, nil) for n > 0 lessons written.
func IngestFromPath(ctx context.Context, absPath string, store LessonsWriter) (int, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("opsfile: read %s: %w", absPath, err)
	}
	return IngestFileContent(ctx, string(data), store)
}

// IngestFileContent parses content as Markdown and upserts each ## section
// as a kind=operational document. The document ID is derived from a content
// hash so re-ingesting an unchanged file is idempotent (Upsert overwrites).
func IngestFileContent(ctx context.Context, content string, store LessonsWriter) (int, error) {
	lessons := parseMarkdownLessons(content)
	count := 0
	for _, l := range lessons {
		sig := lessonSig(l.title, l.body)
		doc := memoryindex.Document{
			ID:        "opsfile:" + sig,
			Kind:      memoryindex.KindOperational,
			Title:     l.title,
			Body:      l.body,
			Handle:    "opsfile:" + sig,
			Status:    "active",
			UpdatedAt: time.Now().UTC(),
		}
		if err := store.Upsert(ctx, doc); err != nil {
			return count, fmt.Errorf("opsfile: upsert %q: %w", l.title, err)
		}
		count++
	}
	return count, nil
}

type lesson struct {
	title string
	body  string
}

// parseMarkdownLessons splits content into lessons on ## headers.
// Each ## header line becomes the lesson title; the text below it (up to the
// next ## or end-of-file) is the lesson body. Sections without a body are
// skipped. The top-level # heading (if any) and any text before the first ##
// are ignored.
func parseMarkdownLessons(content string) []lesson {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	var lessons []lesson
	var currentTitle string
	var bodyLines []string

	flush := func() {
		body := strings.TrimSpace(strings.Join(bodyLines, "\n"))
		title := strings.TrimSpace(currentTitle)
		if title != "" && body != "" {
			lessons = append(lessons, lesson{title: title, body: body})
		}
		bodyLines = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentTitle = strings.TrimPrefix(line, "## ")
		} else if currentTitle != "" {
			// Only accumulate body lines once we're inside a ## section.
			bodyLines = append(bodyLines, line)
		}
		// Lines before the first ## (including a possible # title) are ignored.
	}
	flush()
	return lessons
}

// lessonSig returns a 16-char hex content hash used as the document ID suffix.
// Identical title+body → identical ID → Upsert is idempotent.
func lessonSig(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + body))
	return fmt.Sprintf("%x", sum[:])[:16]
}
