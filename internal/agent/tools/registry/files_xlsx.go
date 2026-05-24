package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/files"
	source "github.com/aura/aura/internal/storage/sources/store"
)

// CreateXLSXTool generates a workbook from a structured spec, persists
// it via the source store (sha256-keyed dedup gives "show me last week's
// invoice" for free), and optionally ships the file to the calling user
// via Telegram.
//
// PDR §15a: file creation milestone — xlsx-first, sources-store
// persistence, Telegram delivery.
type CreateXLSXTool struct {
	store  source.Writer
	sender DocumentSender
}

// NewCreateXLSXTool builds the tool. Sender may be nil — when unset the
// tool still works as a pure file-generator (callers can fetch via
// dashboard), it just refuses delivery requests with a clear error.
func NewCreateXLSXTool(store source.Writer, sender DocumentSender) *CreateXLSXTool {
	if store == nil {
		return nil
	}
	return &CreateXLSXTool{store: store, sender: sender}
}

func (t *CreateXLSXTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return executeGeneratedDocument(ctx, args, t.store, t.sender, "create_xlsx", source.KindXLSX, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", parseCreateXLSXArgs, files.BuildXLSX)
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
	caption := sanitizeDocumentCaption(stringArg(args, "caption"))

	return files.XLSXSpec{Filename: filename, Sheets: sheets}, deliver, caption, nil
}
