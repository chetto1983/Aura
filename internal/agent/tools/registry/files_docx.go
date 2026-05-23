package tools

import (
	"context"
	"errors"

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
	spec, deliver, caption, err := parseCreateDOCXArgs(args)
	if err != nil {
		return "", err
	}
	body, name, err := files.BuildDOCX(spec)
	if err != nil {
		return "", err
	}
	return persistAndDeliverFile(ctx, t.store, t.sender, "create_docx", source.PutInput{
		Kind:     source.KindDOCX,
		Filename: name,
		MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Bytes:    body,
	}, deliver, caption)
}

// parseCreateDOCXArgs converts the LLM JSON into a DOCXSpec via the shared
// blockShape intermediate.
func parseCreateDOCXArgs(args map[string]any) (files.DOCXSpec, bool, string, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return files.DOCXSpec{}, false, "", errors.New("create_docx: filename is required")
	}

	title, _ := args["title"].(string)

	shapes, err := parseBlockShapes("create_docx", args["blocks"])
	if err != nil {
		return files.DOCXSpec{}, false, "", err
	}
	if err := requireBlocksOrTitle("create_docx", title, len(shapes)); err != nil {
		return files.DOCXSpec{}, false, "", err
	}

	blocks := make([]files.DOCXBlock, 0, len(shapes))
	for _, s := range shapes {
		blocks = append(blocks, files.DOCXBlock{Kind: s.Kind, Level: s.Level, Text: s.Text, Rows: s.Rows})
	}

	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	caption := sanitizeDocumentCaption(stringArg(args, "caption"))

	return files.DOCXSpec{Filename: filename, Title: title, Blocks: blocks}, deliver, caption, nil
}
