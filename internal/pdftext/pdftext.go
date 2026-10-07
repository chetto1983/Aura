// Package pdftext reads a PDF's words with poppler's pdftotext.
//
// Reading a PDF is a PDF engine's job, and the runtime image already carries one:
// poppler-utils is installed in docker/aura/Dockerfile, so this is a subprocess and
// not a service call. It resolves the ToUnicode CMaps that turn a Type0/CID font's
// glyph indices back into characters, which no hand-written parser here could do.
// Ingest's file card and web_fetch's PDF lane both read through it.
package pdftext

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Tool is the extractor binary. Where it is absent (a developer host, a runner
// without poppler-utils) Extract fails with an error wrapping exec.ErrNotFound and
// each caller states that in its own terms.
const Tool = "pdftotext"

// Extract returns the text of path's first pages pages (every page when pages is 0)
// and reports whether maxBytes cut it short. A run that ended badly returns an error
// and no text: output nobody can vouch for is never handed back as if it were whole.
// Hitting the byte cap is not a bad ending: it is this function stopping a run whose
// output it already has all it wants of. ctx bounds a pathological or hostile file.
func Extract(ctx context.Context, path string, pages, maxBytes int) (text string, capped bool, err error) {
	tool, err := exec.LookPath(Tool)
	if err != nil {
		return "", false, fmt.Errorf("%s is not installed: %w", Tool, err)
	}
	// An absolute path can never be mistaken for an option: poppler's argument
	// parser has no "--" terminator, so a file named "-layout.pdf" would be one.
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", path, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := []string{"-q", "-enc", "UTF-8", "-eol", "unix", "-nopgbrk", "-f", "1"}
	if pages > 0 {
		args = append(args, "-l", strconv.Itoa(pages))
	}
	//nolint:gosec // G204: `tool` is LookPath of a package constant and every other
	// argument is a literal but the path, which is made absolute above so it cannot
	// be parsed as an option.
	cmd := exec.CommandContext(ctx, tool, append(args, absolute, "-")...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", Tool, err)
	}
	if err := cmd.Start(); err != nil {
		return "", false, fmt.Errorf("%s: %w", Tool, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(stdout, int64(maxBytes)+1))
	capped = len(body) > maxBytes
	if capped {
		body = body[:maxBytes]
		// Nothing will read the rest, and an extractor writing into a pipe that
		// nobody drains would sit there until the deadline.
		cancel()
	}
	waitErr := cmd.Wait()
	switch {
	case capped:
		return string(body), true, nil
	case readErr != nil:
		return "", false, fmt.Errorf("read %s output: %w", Tool, readErr)
	case waitErr != nil:
		return "", false, fmt.Errorf("%s: %w", Tool, waitErr)
	}
	return string(body), false, nil
}
