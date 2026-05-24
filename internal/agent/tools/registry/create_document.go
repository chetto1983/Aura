package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	source "github.com/aura/aura/internal/storage/sources/store"
)

// CreateDocumentTool is the single LLM-facing facade over Aura's existing
// document builders. The per-format tools stay as library code so the manifest
// exposes one action-enum surface instead of three near-identical tools.
type CreateDocumentTool struct {
	pdf  *CreatePDFTool
	xlsx *CreateXLSXTool
	docx *CreateDOCXTool
}

// NewCreateDocumentTool builds the consolidated document tool. The source
// store is required; sender is optional and only needed when deliver=true.
func NewCreateDocumentTool(store source.Writer, sender DocumentSender) *CreateDocumentTool {
	if store == nil {
		return nil
	}
	return &CreateDocumentTool{
		pdf:  NewCreatePDFTool(store, sender),
		xlsx: NewCreateXLSXTool(store, sender),
		docx: NewCreateDOCXTool(store, sender),
	}
}

func (t *CreateDocumentTool) Name() string { return "create_document" }

func (t *CreateDocumentTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // creates source-store artifacts and may deliver files
		Examples: []ToolCallExample{
			{
				Description: "Create a PDF artifact without delivery.",
				Arguments: map[string]any{
					"format": "pdf",
					"spec": map[string]any{
						"filename": "example-report",
						"title":    "Test",
						"blocks": []any{
							map[string]any{"kind": "bullet", "text": "First point"},
						},
						"deliver": false,
					},
				},
			},
			{
				Description: "Create an XLSX artifact with one sheet.",
				Arguments: map[string]any{
					"format": "xlsx",
					"spec": map[string]any{
						"filename": "people",
						"sheets": []any{
							map[string]any{
								"name": "Data",
								"rows": []any{
									[]any{"Nome", "Eta"},
									[]any{"Ada", "37"},
								},
							},
						},
						"deliver": false,
					},
				},
			},
			{
				Description: "Create a DOCX artifact with structured blocks.",
				Arguments: map[string]any{
					"format": "docx",
					"spec": map[string]any{
						"filename": "brief",
						"title":    "Test",
						"blocks": []any{
							map[string]any{"kind": "paragraph", "text": "Document body."},
						},
						"deliver": false,
					},
				},
			},
		},
	}
}

func (t *CreateDocumentTool) Description() string {
	return "Create PDF, XLSX, or DOCX source artifacts from structured specs. Use instead of execute_code. After success, report source_id; do not call source to verify."
}

func (t *CreateDocumentTool) Parameters() map[string]any {
	blockSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"kind"},
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{"heading", "paragraph", "bullet", "table"},
				"description": "PDF/DOCX block type.",
			},
			"level": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     6,
				"description": "Heading level, only for kind=heading.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text for heading, paragraph, or bullet blocks.",
			},
			"rows": tableRowsSchema("Table rows for kind=table."),
		},
	}
	specSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"filename": map[string]any{
				"type":        "string",
				"description": "Required. File name; extension is added or normalized.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "PDF/DOCX title. Provide title, blocks, or both.",
			},
			"blocks": map[string]any{
				"type":        "array",
				"description": "PDF/DOCX blocks.",
				"items":       blockSchema,
			},
			"sheets": map[string]any{
				"type":        "array",
				"description": "XLSX sheets. Required when format=xlsx.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"rows"},
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Optional worksheet name.",
						},
						"rows": tableRowsSchema("2-D row/cell values."),
					},
				},
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "Default true. Set false to persist only.",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional delivery caption.",
			},
		},
	}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"format", "spec"},
		"properties": map[string]any{
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"pdf", "xlsx", "docx"},
				"description": "Document format to create.",
			},
			"spec": specSchema,
		},
		"oneOf": []map[string]any{
			{
				"properties": map[string]any{"format": map[string]any{"const": "pdf"}},
				"required":   []string{"format", "spec"},
			},
			{
				"properties": map[string]any{"format": map[string]any{"const": "xlsx"}},
				"required":   []string{"format", "spec"},
			},
			{
				"properties": map[string]any{"format": map[string]any{"const": "docx"}},
				"required":   []string{"format", "spec"},
			},
		},
	}
}

func (t *CreateDocumentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	format := strings.ToLower(strings.TrimSpace(stringArg(args, "format")))
	spec, err := documentSpecArgs(args)
	if err != nil {
		return "", err
	}
	switch format {
	case "pdf":
		if t.pdf == nil {
			return "", errors.New("create_document: pdf builder unavailable")
		}
		return t.pdf.Execute(ctx, spec)
	case "xlsx":
		if t.xlsx == nil {
			return "", errors.New("create_document: xlsx builder unavailable")
		}
		return t.xlsx.Execute(ctx, spec)
	case "docx":
		if t.docx == nil {
			return "", errors.New("create_document: docx builder unavailable")
		}
		return t.docx.Execute(ctx, spec)
	default:
		return "", fmt.Errorf("create_document: unsupported format %q (want pdf, xlsx, or docx)", format)
	}
}

func documentSpecArgs(args map[string]any) (map[string]any, error) {
	raw, ok := args["spec"]
	if !ok {
		return nil, errors.New("create_document: spec object is required")
	}
	spec, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("create_document: spec must be an object")
	}
	out := make(map[string]any, len(spec))
	for k, v := range spec {
		out[k] = v
	}
	return out, nil
}

func tableRowsSchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "string",
			},
		},
	}
}
