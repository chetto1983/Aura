// Package tools — files.go holds the shared bits used by every document
// generator (create_xlsx, create_docx, create_pdf): the DocumentSender
// boundary, LLM caption sanitization, and the cell-stringifier.
//
// Per-tool implementations live next door:
//   - files_xlsx.go   — CreateXLSXTool + parseCreateXLSXArgs
//   - files_docx.go   — CreateDOCXTool + parseCreateDOCXArgs
//   - files_pdf.go    — CreatePDFTool + parseCreatePDFArgs
//   - files_blocks.go — shared blockShape + parseBlockShapes used by docx/pdf
//
// The split keeps every per-format file below 200 LOC and the helper file
// below 100 — see F-046 in the 2026-05-11 tools audit.
package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxDocumentCaptionChars matches Telegram's documented caption cap so a
// hostile LLM-fed caption can't pin the bot on a server-side rejection.
const maxDocumentCaptionChars = 1024

// sanitizeDocumentCaption trims, strips ASCII control bytes (except newline),
// and length-caps a caption before it leaves Aura for Telegram. The caption is
// LLM-controlled and can be conversation-injected from any source (e.g. a
// fetched web page that asks the model to set a caption).
func sanitizeDocumentCaption(raw string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' {
			return -1
		}
		if r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	if len(clean) > maxDocumentCaptionChars {
		clean = clean[:maxDocumentCaptionChars]
	}
	return clean
}

// DocumentSender ships an arbitrary file body to a user's direct
// Telegram chat. The bot satisfies this; tests pass a stub.
//
// Kept separate from TokenSender so a misuse can't cross channels — a
// future refactor can fold them together once a third sender shows up.
type DocumentSender interface {
	SendDocumentToUser(userID, filename string, body []byte, caption string) error
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
			// Surface unrenderable values to the user/LLM rather than silently
			// dropping them into an empty cell — empty cells are an invisible
			// data-loss bug in the deliverable (F-010).
			return fmt.Sprintf("[unrenderable %T]", t)
		}
		return string(b)
	}
}
