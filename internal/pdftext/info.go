package pdftext

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// InfoTool is poppler's metadata reader, shipped in the same poppler-utils package as Tool.
const InfoTool = "pdfinfo"

// Info is what pdfinfo reports about a PDF that a reader of it has to know first.
type Info struct {
	Pages     int
	Encrypted bool
}

// Inspect reads data's page count and encryption with pdfinfo. pdfinfo takes no standard
// input, so data goes through a private temporary file. A file pdfinfo cannot open, or a
// report without a page count, is an error: a caller deciding on the count must never guess it.
func Inspect(ctx context.Context, data []byte) (Info, error) {
	tool, err := exec.LookPath(InfoTool)
	if err != nil {
		return Info{}, fmt.Errorf("%s is not installed: %w", InfoTool, err)
	}
	file, err := os.CreateTemp("", "aura-pdfinfo-*.pdf")
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", InfoTool, err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.Write(data)
	if closeErr := file.Close(); writeErr != nil || closeErr != nil {
		return Info{}, fmt.Errorf("%s: write the PDF: %w", InfoTool, errors.Join(writeErr, closeErr))
	}
	// CreateTemp returns an absolute path, which poppler can never parse as an option.
	//nolint:gosec // G204: `tool` is LookPath of a package constant, the path a temp file.
	out, err := exec.CommandContext(ctx, tool, file.Name()).Output()
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", InfoTool, err)
	}
	return parseInfo(out)
}

func parseInfo(out []byte) (Info, error) {
	var info Info
	pages := false
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		field, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch field {
		case "Pages":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return Info{}, fmt.Errorf("%s: unreadable page count %q", InfoTool, value)
			}
			info.Pages, pages = n, true
		case "Encrypted":
			info.Encrypted = strings.HasPrefix(value, "yes")
		}
	}
	if err := scanner.Err(); err != nil {
		return Info{}, fmt.Errorf("%s: %w", InfoTool, err)
	}
	if !pages {
		return Info{}, fmt.Errorf("%s reported no page count", InfoTool)
	}
	return info, nil
}
