package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/source"
)

// DocTool consolidates the legacy verb-tools (create_xlsx, create_docx,
// create_pdf) into a single action-enum surface. Picobot pattern: one
// tool, one action enum acting as the format. The shared block grammar
// (heading/paragraph/bullet/table) already lives in files_blocks.go;
// DocTool just dispatches to the existing per-format implementations
// so we don't duplicate spec validation, persistence, or delivery.
//
//	action=xlsx — Excel workbook from sheets[] (rows of cells)
//	action=docx — Word document from title + blocks[]
//	action=pdf  — PDF document from title + blocks[] (same grammar as docx)
//
// Shared optional params across all actions: filename (required),
// deliver (default true), caption.
type DocTool struct {
	xlsx *CreateXLSXTool
	docx *CreateDOCXTool
	pdf  *CreatePDFTool
}

// NewDocTool builds the unified document tool. store is required;
// sender may be nil (file-generator-only mode).
func NewDocTool(store source.Writer, sender DocumentSender) *DocTool {
	if store == nil {
		return nil
	}
	return &DocTool{
		xlsx: NewCreateXLSXTool(store, sender),
		docx: NewCreateDOCXTool(store, sender),
		pdf:  NewCreatePDFTool(store, sender),
	}
}

func (t *DocTool) Name() string { return "doc" }

func (t *DocTool) Description() string {
	return `Generate a document (.xlsx / .docx / .pdf) from a structured spec and persist it as a source.
Optionally deliver to the user's Telegram chat.

Actions (pick one via the "action" field):

  • xlsx — Excel workbook. Required: filename, sheets (array of {name, rows}).
    Each row is an array of cell values (strings, numbers, booleans). Cells
    are sanitized against formula injection (no leading =/+/-/@). Prefer this
    over execute_code for ordinary spreadsheets, table exports, invoices.

  • docx — Word document. Required: filename. Provide title and/or blocks
    (heading | paragraph | bullet | table). At least one of (title, blocks)
    is required. Prefer this over execute_code for reports, memos, write-ups.

  • pdf — PDF document with the same block grammar as docx. Required:
    filename. Provide title and/or blocks. Prefer this over execute_code
    when the deliverable should be print-final (contracts, invoices).

Shared optional params across all actions: deliver (default true) sends to
the caller's Telegram chat; caption is a one-line annotation on delivery.`
}

func (t *DocTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"xlsx", "docx", "pdf"},
				"description": "Which format to generate: xlsx (workbook), docx (Word), pdf (PDF).",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "User-visible filename. Format suffix is appended if missing. Path separators stripped.",
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "When true (default), also send the file to the caller's Telegram chat. Set false to persist only.",
				"default":     true,
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional one-line caption used on Telegram delivery. Ignored when deliver=false.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "docx/pdf only, optional: H1 rendered at the top of the document.",
			},
			"sheets": map[string]any{
				"type":        "array",
				"description": "xlsx only: workbook tabs. Each is {name, rows} where rows is an array of cell-value arrays.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"rows": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
					},
					"required": []string{"name", "rows"},
				},
			},
			"blocks": map[string]any{
				"type":        "array",
				"description": "docx/pdf only: body blocks. Each is {kind, ...}. kind=heading needs text+level. kind=paragraph/bullet needs text. kind=table needs rows.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":  map[string]any{"type": "string", "enum": []string{"heading", "paragraph", "bullet", "table"}},
						"level": map[string]any{"type": "integer", "minimum": 1, "maximum": 6},
						"text":  map[string]any{"type": "string"},
						"rows": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
					},
					"required": []string{"kind"},
				},
			},
		},
	}
}

func (t *DocTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "xlsx":
		return t.xlsx.Execute(ctx, args)
	case "docx":
		return t.docx.Execute(ctx, args)
	case "pdf":
		return t.pdf.Execute(ctx, args)
	case "":
		return "", fmt.Errorf("doc: action is required: %w", llm.ErrSchemaValidation)
	default:
		return "", fmt.Errorf("doc: unknown action %q: %w", action, llm.ErrSchemaValidation)
	}
}
