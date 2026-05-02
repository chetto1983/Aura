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
