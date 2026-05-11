package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/files"
	"github.com/aura/aura/internal/source"
)

// CreatePDFTool generates a PDF document from a structured spec
// (title + heading/paragraph/bullet/table blocks), persists it via the
// source store with sha256 dedup, and optionally ships it via Telegram.
//
// PDR §15c: file creation milestone — pdf as third file format. Same
// block grammar as create_docx so the LLM only has to learn one DSL.
type CreatePDFTool struct {
	store  source.Writer
	sender DocumentSender
}

// NewCreatePDFTool builds the tool. Same nil-tolerance as
// NewCreateXLSXTool / NewCreateDOCXTool.
func NewCreatePDFTool(store source.Writer, sender DocumentSender) *CreatePDFTool {
	if store == nil {
		return nil
	}
	return &CreatePDFTool{store: store, sender: sender}
}

func (t *CreatePDFTool) Name() string { return "create_pdf" }

func (t *CreatePDFTool) Description() string {
	return "Generate a PDF document (.pdf) from structured blocks (heading/paragraph/bullet/table) and persist it as a source. Optionally deliver the file to the user's Telegram chat. Prefer this over execute_code for ordinary PDFs, printable reports, invoices, contract drafts, or documents that should be PDF rather than editable Word format."
}

func (t *CreatePDFTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filename": map[string]any{
				"type":        "string",
				"description": "User-visible filename. .pdf suffix is appended if missing. Path separators are stripped.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional H1 rendered at the top of the PDF. Leave empty to start with the first block.",
			},
			"blocks": map[string]any{
				"type":        "array",
				"description": "Body blocks in order. At least one block (or a non-empty title) is required.",
				"maxItems":    files.MaxPDFBlocks,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind": map[string]any{
							"type":        "string",
							"enum":        []string{"heading", "paragraph", "bullet", "table"},
							"description": "Block type. Same grammar as create_docx.",
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
							"description": "Table rows: array of arrays of strings. Required for kind=table. Cap is 20 cols/row (narrower than docx).",
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

func (t *CreatePDFTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	spec, deliver, caption, err := parseCreatePDFArgs(args)
	if err != nil {
		return "", err
	}

	body, name, err := files.BuildPDF(spec)
	if err != nil {
		return "", err
	}

	src, dup, err := t.store.Put(ctx, source.PutInput{
		Kind:     source.KindPDFGen,
		Filename: name,
		MimeType: "application/pdf",
		Bytes:    body,
	})
	if err != nil {
		return "", fmt.Errorf("create_pdf: persist: %w", err)
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
			return "", errors.New("create_pdf: deliver=true but no user context (call from Telegram or set deliver=false)")
		}
		if t.sender == nil {
			return "", errors.New("create_pdf: deliver=true but no DocumentSender configured")
		}
		if err := t.sender.SendDocumentToUser(userID, name, body, caption); err != nil {
			return "", fmt.Errorf("create_pdf: persisted as %s but delivery failed: %w", src.ID, err)
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
		return "", fmt.Errorf("create_pdf: marshal response: %w", err)
	}
	return string(out), nil
}

// parseCreatePDFArgs converts the LLM JSON into a PDFSpec via the shared
// blockShape intermediate. DOCX and PDF share the same block grammar; the
// only per-format work is the spec struct conversion below.
func parseCreatePDFArgs(args map[string]any) (files.PDFSpec, bool, string, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return files.PDFSpec{}, false, "", errors.New("create_pdf: filename is required")
	}

	title, _ := args["title"].(string)

	shapes, err := parseBlockShapes("create_pdf", args["blocks"])
	if err != nil {
		return files.PDFSpec{}, false, "", err
	}
	if err := requireBlocksOrTitle("create_pdf", title, len(shapes)); err != nil {
		return files.PDFSpec{}, false, "", err
	}

	blocks := make([]files.PDFBlock, 0, len(shapes))
	for _, s := range shapes {
		blocks = append(blocks, files.PDFBlock{Kind: s.Kind, Level: s.Level, Text: s.Text, Rows: s.Rows})
	}

	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	caption := sanitizeDocumentCaption(stringArg(args, "caption"))

	return files.PDFSpec{Filename: filename, Title: title, Blocks: blocks}, deliver, caption, nil
}
