// Package tools — files.go holds the shared bits used by every document
// generator (create_xlsx, create_docx, create_pdf): the DocumentSender
// boundary, LLM caption sanitization, the cell-stringifier, and the
// persist→deliver→marshal helper that all three Execute methods share.
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
	"context"
	"encoding/json"
	"fmt"
	"strings"

	source "github.com/aura/aura/internal/storage/sources/store"
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

// persistAndDeliverFile handles the shared persist → status-update → deliver →
// marshal sequence that all file-creation tools (xlsx, docx, pdf) execute after
// building the file body. Generated files are marked ingested immediately so
// the dashboard shows them in the right bucket without a separate OCR step.
func persistAndDeliverFile(
	ctx context.Context,
	st source.Writer,
	sender DocumentSender,
	toolName string,
	input source.PutInput,
	deliver bool,
	caption string,
) (string, error) {
	src, dup, err := st.Put(ctx, input)
	if err != nil {
		return "", fmt.Errorf("%s: persist: %w", toolName, err)
	}
	if src.Status != source.StatusIngested {
		updated, err := st.Update(src.ID, func(s *source.Source) error {
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
			return "", fmt.Errorf("%s: deliver=true but no user context (call from Telegram or set deliver=false)", toolName)
		}
		if sender == nil {
			return "", fmt.Errorf("%s: deliver=true but no DocumentSender configured", toolName)
		}
		if err := sender.SendDocumentToUser(userID, input.Filename, input.Bytes, caption); err != nil {
			return "", fmt.Errorf("%s: persisted as %s but delivery failed: %w", toolName, src.ID, err)
		}
	}
	resp := map[string]any{
		"source_id":  src.ID,
		"filename":   input.Filename,
		"size_bytes": src.SizeBytes,
		"sha256":     src.SHA256,
		"duplicate":  dup,
		"delivered":  deliver,
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("%s: marshal response: %w", toolName, err)
	}
	return string(out), nil
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
