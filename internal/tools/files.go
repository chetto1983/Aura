package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/files"
	"github.com/aura/aura/internal/source"
)

// DocumentSender ships an arbitrary file body to a user's direct
// Telegram chat. The bot satisfies this; tests pass a stub.
//
// Kept separate from TokenSender so a misuse can't cross channels — a
// future refactor can fold them together once a third sender shows up.
type DocumentSender interface {
	SendDocumentToUser(userID, filename string, body []byte, caption string) error
}

// CreateXLSXTool generates a workbook from a structured spec, persists
// it via the source store (sha256-keyed dedup gives "show me last week's
// invoice" for free), and optionally ships the file to the calling user
// via Telegram.
//
// PDR §15a: file creation milestone — xlsx-first, sources-store
// persistence, Telegram delivery.
type CreateXLSXTool struct {
	store  *source.Store
	sender DocumentSender
}

// NewCreateXLSXTool builds the tool. Sender may be nil — when unset the
// tool still works as a pure file-generator (callers can fetch via
// dashboard), it just refuses delivery requests with a clear error.
func NewCreateXLSXTool(store *source.Store, sender DocumentSender) *CreateXLSXTool {
	if store == nil {
		return nil
	}
	return &CreateXLSXTool{store: store, sender: sender}
}

func (t *CreateXLSXTool) Name() string { return "create_xlsx" }

func (t *CreateXLSXTool) Description() string {
	return "Generate an Excel workbook (.xlsx) from structured rows and persist it as a source. Optionally deliver the file to the user's Telegram chat. Use when the user asks for a spreadsheet, table export, invoice, or report. Cells are sanitized against formula injection; pure values only — no formulas."
}

func (t *CreateXLSXTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filename": map[string]any{
				"type":        "string",
				"description": "User-visible filename. .xlsx suffix is appended if missing. Path separators are stripped.",
			},
			"sheets": map[string]any{
				"type":        "array",
				"description": "Workbook tabs. At least one is required. Empty workbooks are rejected.",
				"minItems":    1,
				"maxItems":    files.MaxSheets,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Tab name. Truncated to 31 chars; : \\ / ? * [ ] are replaced with _.",
						},
						"rows": map[string]any{
							"type":        "array",
							"description": "2-D array of cell strings. Each inner array is one row. Numbers/dates are stored as text — Excel still parses them on display.",
							"items": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
						},
					},
					"required": []string{"name", "rows"},
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
		"required": []string{"filename", "sheets"},
	}
}

func (t *CreateXLSXTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	spec, deliver, caption, err := parseCreateXLSXArgs(args)
	if err != nil {
		return "", err
	}

	body, name, err := files.BuildXLSX(spec)
	if err != nil {
		return "", err
	}

	src, dup, err := t.store.Put(ctx, source.PutInput{
		Kind:     source.KindXLSX,
		Filename: name,
		MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Bytes:    body,
	})
	if err != nil {
		return "", fmt.Errorf("create_xlsx: persist: %w", err)
	}
	// Generated files never go through OCR + ingest; mark ingested up
	// front so dashboard's source list shows them in the right bucket.
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
			return "", errors.New("create_xlsx: deliver=true but no user context (call from Telegram or set deliver=false)")
		}
		if t.sender == nil {
			return "", errors.New("create_xlsx: deliver=true but no DocumentSender configured")
		}
		if err := t.sender.SendDocumentToUser(userID, name, body, caption); err != nil {
			// Persistence already succeeded — surface the delivery failure
			// but tell the LLM the file is recoverable from sources.
			return "", fmt.Errorf("create_xlsx: persisted as %s but delivery failed: %w", src.ID, err)
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
		return "", fmt.Errorf("create_xlsx: marshal response: %w", err)
	}
	return string(out), nil
}

// parseCreateXLSXArgs lifts the LLM's loosely-typed JSON into a typed
// XLSXSpec. Errors are returned with enough specificity that the model
// can fix the call on retry.
func parseCreateXLSXArgs(args map[string]any) (files.XLSXSpec, bool, string, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return files.XLSXSpec{}, false, "", errors.New("create_xlsx: filename is required")
	}

	rawSheets, ok := args["sheets"].([]any)
	if !ok || len(rawSheets) == 0 {
		return files.XLSXSpec{}, false, "", errors.New("create_xlsx: sheets must be a non-empty array")
	}

	sheets := make([]files.XLSXSheet, 0, len(rawSheets))
	for i, rs := range rawSheets {
		obj, ok := rs.(map[string]any)
		if !ok {
			return files.XLSXSpec{}, false, "", fmt.Errorf("create_xlsx: sheet[%d] is not an object", i)
		}
		name, _ := obj["name"].(string)
		rawRows, ok := obj["rows"].([]any)
		if !ok {
			return files.XLSXSpec{}, false, "", fmt.Errorf("create_xlsx: sheet[%d].rows must be an array", i)
		}
		rows := make([][]string, 0, len(rawRows))
		for j, rr := range rawRows {
			rawCells, ok := rr.([]any)
			if !ok {
				return files.XLSXSpec{}, false, "", fmt.Errorf("create_xlsx: sheet[%d].rows[%d] must be an array", i, j)
			}
			cells := make([]string, 0, len(rawCells))
			for _, c := range rawCells {
				cells = append(cells, stringifyCell(c))
			}
			rows = append(rows, cells)
		}
		sheets = append(sheets, files.XLSXSheet{Name: name, Rows: rows})
	}

	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	caption, _ := args["caption"].(string)

	return files.XLSXSpec{Filename: filename, Sheets: sheets}, deliver, caption, nil
}

// CreateDOCXTool generates a Word document from a structured spec
// (title + heading/paragraph/bullet/table blocks), persists it via the
// source store with sha256 dedup, and optionally ships it via Telegram.
//
// PDR §15b: file creation milestone — docx as second file format.
type CreateDOCXTool struct {
	store  *source.Store
	sender DocumentSender
}

// NewCreateDOCXTool builds the tool. Same nil-tolerance as
// NewCreateXLSXTool: store is required, sender is optional.
func NewCreateDOCXTool(store *source.Store, sender DocumentSender) *CreateDOCXTool {
	if store == nil {
		return nil
	}
	return &CreateDOCXTool{store: store, sender: sender}
}

func (t *CreateDOCXTool) Name() string { return "create_docx" }

func (t *CreateDOCXTool) Description() string {
	return "Generate a Word document (.docx) from structured blocks (heading/paragraph/bullet/table) and persist it as a source. Optionally deliver the file to the user's Telegram chat. Use when the user asks for a report, memo, write-up, summary doc, or formatted note."
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

// parseCreateDOCXArgs lifts the LLM's loosely-typed JSON into a typed
// DOCXSpec. Same shape as parseCreateXLSXArgs but for DOCX blocks.
func parseCreateDOCXArgs(args map[string]any) (files.DOCXSpec, bool, string, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return files.DOCXSpec{}, false, "", errors.New("create_docx: filename is required")
	}

	title, _ := args["title"].(string)

	var blocks []files.DOCXBlock
	if raw, ok := args["blocks"].([]any); ok {
		blocks = make([]files.DOCXBlock, 0, len(raw))
		for i, rb := range raw {
			obj, ok := rb.(map[string]any)
			if !ok {
				return files.DOCXSpec{}, false, "", fmt.Errorf("create_docx: blocks[%d] is not an object", i)
			}
			block := files.DOCXBlock{}
			if v, ok := obj["kind"].(string); ok {
				block.Kind = v
			}
			if block.Kind == "" {
				return files.DOCXSpec{}, false, "", fmt.Errorf("create_docx: blocks[%d].kind is required", i)
			}
			if v, ok := obj["text"].(string); ok {
				block.Text = v
			}
			if v, ok := obj["level"].(float64); ok {
				block.Level = int(v)
			}
			if rawRows, ok := obj["rows"].([]any); ok {
				block.Rows = make([][]string, 0, len(rawRows))
				for j, rr := range rawRows {
					rawCells, ok := rr.([]any)
					if !ok {
						return files.DOCXSpec{}, false, "", fmt.Errorf("create_docx: blocks[%d].rows[%d] must be an array", i, j)
					}
					cells := make([]string, 0, len(rawCells))
					for _, c := range rawCells {
						cells = append(cells, stringifyCell(c))
					}
					block.Rows = append(block.Rows, cells)
				}
			}
			blocks = append(blocks, block)
		}
	}

	if title == "" && len(blocks) == 0 {
		return files.DOCXSpec{}, false, "", errors.New("create_docx: provide a title, at least one block, or both")
	}

	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	caption, _ := args["caption"].(string)

	return files.DOCXSpec{Filename: filename, Title: title, Blocks: blocks}, deliver, caption, nil
}

// stringifyCell coerces whatever the LLM put in a cell slot to a string.
// Numbers come through as float64 from encoding/json; we render them
// without trailing decimal noise for integer values. Booleans render as
// "true"/"false". Anything else falls through json.Marshal so structured
// objects don't crash the tool.
func stringifyCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// Integer-valued floats: drop the ".000000" tail.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
