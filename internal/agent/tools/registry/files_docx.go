package tools

import (
	"context"

	"github.com/aura/aura/internal/files"
	source "github.com/aura/aura/internal/storage/sources/store"
)

// CreateDOCXTool generates a Word document from a structured spec
// (title + heading/paragraph/bullet/table blocks), persists it via the
// source store with sha256 dedup, and optionally ships it via Telegram.
//
// PDR §15b: file creation milestone — docx as second file format.
type CreateDOCXTool struct {
	store  source.Writer
	sender DocumentSender
}

// NewCreateDOCXTool builds the tool. Same nil-tolerance as
// NewCreateXLSXTool: store is required, sender is optional.
func NewCreateDOCXTool(store source.Writer, sender DocumentSender) *CreateDOCXTool {
	if store == nil {
		return nil
	}
	return &CreateDOCXTool{store: store, sender: sender}
}

func (t *CreateDOCXTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return executeGeneratedDocument(ctx, args, t.store, t.sender, "create_docx", source.KindDOCX, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", parseCreateDOCXArgs, files.BuildDOCX)
}

// parseCreateDOCXArgs converts the LLM JSON into a DOCXSpec via the shared
// blockShape intermediate.
func parseCreateDOCXArgs(args map[string]any) (files.DOCXSpec, bool, string, error) {
	return parseBlockDocumentSpec("create_docx", args, docxBlockFromShape, docxSpecFromBlocks)
}

func docxBlockFromShape(s blockShape) files.DOCXBlock {
	return files.DOCXBlock{Kind: s.Kind, Level: s.Level, Text: s.Text, Rows: s.Rows}
}

func docxSpecFromBlocks(parsed blockDocumentArgs, blocks []files.DOCXBlock) files.DOCXSpec {
	return files.DOCXSpec{Filename: parsed.Filename, Title: parsed.Title, Blocks: blocks}
}
