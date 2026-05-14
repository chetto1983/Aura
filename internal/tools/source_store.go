package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aura/aura/internal/storage/sources/store"
)

// StoreSourceTool stores text or a URL as an immutable source.
//
// PDFs are stored automatically by the Telegram document handler (slice 4);
// the LLM cannot stream binary content through tool calls, so we deliberately
// do not expose a "pdf" mode here.
type StoreSourceTool struct {
	store source.Writer
}

func NewStoreSourceTool(store source.Writer) *StoreSourceTool {
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
		trimmed := strings.TrimSpace(content)
		u, err := url.Parse(trimmed)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf("store_source: kind=url requires an absolute http(s) URL")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("store_source: kind=url only accepts http or https schemes")
		}
		content = trimmed
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
