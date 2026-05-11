package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/files"
	"github.com/aura/aura/internal/source"
)

// CreateDOCXTool generates a Word document from a structured spec
// (title + heading/paragraph/bullet/table blocks), persists it via the
// source store with sha256 dedup, and optionally ships it via Telegram.
//
// PDR §15b: file creation milestone — docx as second file format.
type CreateDOCXTool struct {
	store  source.Writer
	sender DocumentSender
}

// NewCreateDOCXTool builds the tool. Same nil-tolerance as
// NewCreateXLSXTool: store is required, sender is optional.
func NewCreateDOCXTool(store source.Writer, sender DocumentSender) *CreateDOCXTool {
	if store == nil {
		return nil
	}
	return &CreateDOCXTool{store: store, sender: sender}
}

func (t *CreateDOCXTool) Name() string { return "create_docx" }

func (t *CreateDOCXTool) Description() string {
	return "Generate a Word document (.docx) from structured blocks (heading/paragraph/bullet/table) and persist it as a source. Optionally deliver the file to the user's Telegram chat. Prefer this over execute_code for ordinary Word documents, reports, memos, write-ups, summary docs, or formatted notes."
}

func (t *CreateDOCXTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filename": map[string]any{
				"type":        "string",
				"description": "User-visible filename. .docx suffix is appended if missing. Path separators are stripped.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional H1 rendered at the top of the document. Leave empty to start with the first block.",
			},
			"blocks": map[string]any{
				"type":        "array",
				"description": "Body blocks in order. At least one block (or a non-empty title) is required.",
				"maxItems":    files.MaxDOCXBlocks,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind": map[string]any{
							"type":        "string",
							"enum":        []string{"heading", "paragraph", "bullet", "table"},
							"description": "Block type. heading: H1-H6 via level. paragraph: plain text. bullet: prefixed with '• '. table: 2-D rows.",
						},
						"level": map[string]any{
							"type":        "integer",
							"description": "Heading level 1..6 (clamped). Ignored for non-heading kinds.",
							"minimum":     1,
							"maximum":     6,
						},
						"text": map[string]any{
							"type":        "string",
							"description": "Text content for heading/paragraph/bullet. Ignored for table.",
						},
						"rows": map[string]any{
							"type":        "array",
							"description": "Table rows: array of arrays of strings. Required for kind=table.",
							"items": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
						},
					},
					"required": []string{"kind"},
				},
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "If true (default), also send the generated file to the user's Telegram chat. Set false to persist without delivery.",
				"default":     true,
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional one-line caption sent with the document on delivery. Ignored when deliver=false.",
			},
		},
		"required": []string{"filename"},
	}
}

func (t *CreateDOCXTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	spec, deliver, caption, err := parseCreateDOCXArgs(args)
	if err != nil {
		return "", err
	}

	body, name, err := files.BuildDOCX(spec)
	if err != nil {
		return "", err
	}

	src, dup, err := t.store.Put(ctx, source.PutInput{
		Kind:     source.KindDOCX,
		Filename: name,
		MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Bytes:    body,
	})
	if err != nil {
		return "", fmt.Errorf("create_docx: persist: %w", err)
	}
	if src.Status != source.StatusIngested {
		updated, err := t.store.Update(src.ID, func(s *source.Source) error {
			s.Status = source.StatusIngested
			return nil
		})
		if err == nil {
			src = updated
		}
	}

	if deliver {
		userID := UserIDFromContext(ctx)
		if userID == "" {
			return "", errors.New("create_docx: deliver=true but no user context (call from Telegram or set deliver=false)")
		}
		if t.sender == nil {
			return "", errors.New("create_docx: deliver=true but no DocumentSender configured")
		}
		if err := t.sender.SendDocumentToUser(userID, name, body, caption); err != nil {
			return "", fmt.Errorf("create_docx: persisted as %s but delivery failed: %w", src.ID, err)
		}
	}

	resp := map[string]any{
		"source_id":  src.ID,
		"filename":   name,
		"size_bytes": src.SizeBytes,
		"sha256":     src.SHA256,
		"duplicate":  dup,
		"delivered":  deliver,
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("create_docx: marshal response: %w", err)
	}
	return string(out), nil
}

// parseCreateDOCXArgs converts the LLM JSON into a DOCXSpec via the shared
// blockShape intermediate.
func parseCreateDOCXArgs(args map[string]any) (files.DOCXSpec, bool, string, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return files.DOCXSpec{}, false, "", errors.New("create_docx: filename is required")
	}

	title, _ := args["title"].(string)

	shapes, err := parseBlockShapes("create_docx", args["blocks"])
	if err != nil {
		return files.DOCXSpec{}, false, "", err
	}
	if err := requireBlocksOrTitle("create_docx", title, len(shapes)); err != nil {
		return files.DOCXSpec{}, false, "", err
	}

	blocks := make([]files.DOCXBlock, 0, len(shapes))
	for _, s := range shapes {
		blocks = append(blocks, files.DOCXBlock{Kind: s.Kind, Level: s.Level, Text: s.Text, Rows: s.Rows})
	}

	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	caption := sanitizeDocumentCaption(stringArg(args, "caption"))

	return files.DOCXSpec{Filename: filename, Title: title, Blocks: blocks}, deliver, caption, nil
}
