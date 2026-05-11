package tools

import (
	"errors"
	"fmt"
)

// blockShape is the format-neutral intermediate the LLM-emitted DOCX/PDF
// blocks decode into. Per-format parsers (DOCX, PDF) walk this slice and
// adapt each entry to their concrete files.DOCXBlock / files.PDFBlock type.
//
// Centralizing the shape removes the ~60 LOC of duplication that previously
// lived in parseCreateDOCXArgs and parseCreatePDFArgs (F-046 cleanup).
type blockShape struct {
	Kind  string
	Level int
	Text  string
	Rows  [][]string
}

// parseBlockShapes lifts the LLM's loosely-typed "blocks" array into a
// neutral slice. tool is the LLM-facing tool name so error messages stay
// recognisable to the model.
func parseBlockShapes(tool string, raw any) ([]blockShape, error) {
	if raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: blocks must be an array", tool)
	}
	out := make([]blockShape, 0, len(arr))
	for i, rb := range arr {
		obj, ok := rb.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: blocks[%d] is not an object", tool, i)
		}
		block := blockShape{}
		if v, ok := obj["kind"].(string); ok {
			block.Kind = v
		}
		if block.Kind == "" {
			return nil, fmt.Errorf("%s: blocks[%d].kind is required", tool, i)
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
					return nil, fmt.Errorf("%s: blocks[%d].rows[%d] must be an array", tool, i, j)
				}
				cells := make([]string, 0, len(rawCells))
				for _, c := range rawCells {
					cells = append(cells, stringifyCell(c))
				}
				block.Rows = append(block.Rows, cells)
			}
		}
		out = append(out, block)
	}
	return out, nil
}

// requireBlocksOrTitle is the post-parse sanity check shared by create_docx
// and create_pdf: at least one of (non-empty title, non-empty blocks) must
// be supplied or the generated document would be empty.
func requireBlocksOrTitle(tool, title string, blocks int) error {
	if title == "" && blocks == 0 {
		return fmt.Errorf("%s: provide a title, at least one block, or both", tool)
	}
	return nil
}

// errBlocksRequired is a sentinel for callers that want a typed empty-doc
// check; not currently used by either tool but documented as the contract.
var errBlocksRequired = errors.New("blocks or title required")
