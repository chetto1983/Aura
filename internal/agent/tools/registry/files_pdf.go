package tools

import (
	"context"

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
	deps documentBuilderDeps
}

// NewCreatePDFTool builds the tool. Same nil-tolerance as
// NewCreateXLSXTool / NewCreateDOCXTool.
func NewCreatePDFTool(store source.Writer, sender DocumentSender) *CreatePDFTool {
	if store == nil {
		return nil
	}
	return &CreatePDFTool{
		deps: documentBuilderDeps{
			store:  store,
			sender: sender,
		},
	}
}

func (t *CreatePDFTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return executeGeneratedDocument(ctx, args, t.deps.store, t.deps.sender, "create_pdf", source.KindPDFGen, "application/pdf", parseCreatePDFArgs, files.BuildPDF)
}

// parseCreatePDFArgs converts the LLM JSON into a PDFSpec via the shared
// blockShape intermediate. DOCX and PDF share the same block grammar; the
// only per-format work is the spec struct conversion below.
func parseCreatePDFArgs(args map[string]any) (files.PDFSpec, bool, string, error) {
	return parseBlockDocumentSpec("create_pdf", args, pdfBlockFromShape, pdfSpecFromBlocks)
}

func pdfBlockFromShape(s blockShape) files.PDFBlock {
	return files.PDFBlock{Kind: s.Kind, Level: s.Level, Text: s.Text, Rows: s.Rows}
}

func pdfSpecFromBlocks(parsed blockDocumentArgs, blocks []files.PDFBlock) files.PDFSpec {
	return files.PDFSpec{Filename: parsed.Filename, Title: parsed.Title, Blocks: blocks}
}
