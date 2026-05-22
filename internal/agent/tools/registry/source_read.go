package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/storage/sources/store"
)

// ReadSourceTool reads source metadata or extracted markdown.
type ReadSourceTool struct {
	store source.Repository
}

func NewReadSourceTool(store source.Repository) *ReadSourceTool {
	return &ReadSourceTool{store: store}
}

func (t *ReadSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("read_source: source store unavailable")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}
	mode := stringArg(args, "mode")
	if mode == "" {
		mode = "excerpt"
	}
	byteStart, hasStart, err := optionalIntParam(args, "byte_start")
	if err != nil {
		return "", err
	}
	byteEnd, hasEnd, err := optionalIntParam(args, "byte_end")
	if err != nil {
		return "", err
	}
	if hasStart != hasEnd {
		return "", errors.New("read_source: byte_start and byte_end must be provided together")
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
		if hasStart {
			return "", errors.New("read_source: byte ranges are not supported for metadata mode")
		}
		return formatSourceMetadata(src), nil
	case "ocr":
		return readSourceMarkdownRange(t.store, src, maxSourceToolChars, byteStart, byteEnd, hasStart)
	case "excerpt":
		return readSourceMarkdownRange(t.store, src, excerptDefaultBytes, byteStart, byteEnd, hasStart)
	default:
		return "", fmt.Errorf("read_source: unsupported mode %q", mode)
	}
}

func optionalIntParam(args map[string]any, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "" {
		return 0, false, fmt.Errorf("%s must be an integer", key)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be an integer", key)
	}
	return n, true, nil
}
