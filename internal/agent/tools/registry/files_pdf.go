package tools

import (
	"context"
	"errors"

	"github.com/aura/aura/internal/files"
	source "github.com/aura/aura/internal/storage/sources/store"
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
	return persistAndDeliverFile(ctx, t.store, t.sender, "create_pdf", source.PutInput{
		Kind:     source.KindPDFGen,
		Filename: name,
		MimeType: "application/pdf",
		Bytes:    body,
	}, deliver, caption)
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
