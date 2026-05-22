package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/files"
	"github.com/aura/aura/internal/storage/sources/store"
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
