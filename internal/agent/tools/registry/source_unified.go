package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/sources/ingest"
	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/sources/store"
)

// SourceTool consolidates the seven verb-tools (list_sources, read_source,
// store_source, ingest_source, ocr_source, delete_source, lint_sources)
// into a single action-enum surface. Same picobot pattern as wiki_page /
// dev_tool / task / web / doc / file.
//
//	action=list      — enumerate stored sources with optional filters
//	action=read      — fetch ocr.md / extract.md by source_id
//	action=store     — persist text or a URL (PDFs come via Telegram upload, not LLM)
//	action=reprocess — re-run extraction pipeline; stages=["ocr"] / ["ingest"] / ["ocr","ingest"]
//	action=delete    — remove source + memoryindex rows (irreversible)
//	action=lint      — corpus audit (broken refs, orphans, stale OCR)
//
// Wave 2.7f net: 7 LLM tools → 1. The per-format extractor backend stays
// as-is for now; Wave 2.9 replaces ExtractGo / sandbox-Python runners with
// markitdown.
type SourceTool struct {
	list   *ListSourcesTool
	read   *ReadSourceTool
	store  *StoreSourceTool
	ocr    *OCRSourceTool    // may be nil when OCR is not configured
	ingest *IngestSourceTool // may be nil when pipeline is not wired
	delete *DeleteSourceTool // may be nil when memoryindex is not wired
	lint   *LintSourcesTool
}

// NewSourceTool wires the consolidated tool. The four read/store/list/lint
// helpers are always wired (they only need source.Reader / source.Writer).
// OCR, ingest, and delete are optional — passing nil disables the
// corresponding action with a clear error message.
//
//	reader   — list + lint (source.Reader)
//	writer   — store + delete file ops (source.Writer)
//	full     — read + ocr (source.Repository — same concrete *source.Store)
//	ocrClient — OCR re-run; nil disables action=reprocess stages=[ocr]
//	pipeline  — LLM ingest; nil disables action=reprocess stages=[ingest]
//	purger    — memoryindex purger for delete; nil leaves files-only delete
func NewSourceTool(
	reader source.Reader,
	writer source.Writer,
	full source.Repository,
	store *source.Store,
	ocrClient *ocr.Client,
	pipeline *ingest.Pipeline,
	purger sourcePurger,
) *SourceTool {
	if reader == nil || writer == nil || full == nil {
		return nil
	}
	t := &SourceTool{
		list:  NewListSourcesTool(reader),
		read:  NewReadSourceTool(full),
		store: NewStoreSourceTool(writer),
		lint:  NewLintSourcesTool(reader),
	}
	if ocrClient != nil {
		t.ocr = NewOCRSourceTool(full, ocrClient)
	}
	if pipeline != nil {
		t.ingest = NewIngestSourceTool(pipeline)
	}
	if store != nil {
		t.delete = NewDeleteSourceTool(store, purger)
	}
	return t
}

func (t *SourceTool) Name() string { return "source" }

func (t *SourceTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // delete action permanently removes source + memoryindex rows
		VisibilityTier:  VisibilityActiveTurn,
	}
}

func (t *SourceTool) Description() string {
	return `Manage uploaded sources (PDFs, text, URLs, DOCX, XLSX). action=read Returns source archive bytes, 16384-byte cap.

EXAMPLES — copy the shape exactly:

  source({"action":"list"})
  source({"action":"list","status":"ingested","limit":50})
  source({"action":"read","source_id":"src_0ec1b02e112f0ca4"})
  source({"action":"store","kind":"text","filename":"note.txt","content":"plain text body"})
  source({"action":"store","kind":"url","filename":"article","content":"https://example.com/page"})
  source({"action":"reprocess","source_id":"src_0ec1b02e112f0ca4","stages":["ingest"]})
  source({"action":"delete","source_id":"src_0ec1b02e112f0ca4"})
  source({"action":"lint"})

The "action" field is REQUIRED. Valid values: "list", "read", "store", "reprocess", "delete", "lint".

Per-action required fields:
  • list      → nothing
  • read      → source_id
  • store     → kind ("text" or "url") AND filename AND content
  • reprocess → source_id (stages optional, defaults to ["ingest"])
  • delete    → source_id
  • lint      → nothing

PDFs are uploaded via Telegram, not via this tool. Read returns ocr.md / extract.md text (default 4000-byte cap, max 8000).`
}

func (t *SourceTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read", "store", "reprocess", "delete", "lint"},
				"description": `REQUIRED. "list", "read", "store", "reprocess", "delete", or "lint".`,
			},
			"source_id": map[string]any{
				"type":        "string",
				"description": `Required when action="read"/"reprocess"/"delete". Pattern: src_<16hex>.`,
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"", "stored", "extracting", "ocr_complete", "extract_complete", "ingested", "failed"},
				"description": `Optional, action="list" only. Filter by lifecycle status.`,
			},
			"kind": map[string]any{
				"type":        "string",
				"description": `Required when action="store" ("text" or "url"). Optional filter when action="list".`,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": `Optional, action="list" only. Max results (default 20, max 100).`,
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": `Optional, action="read" only. Max bytes to return (default 4000, max 8000).`,
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"metadata", "ocr", "excerpt"},
				"description": `Optional, action="read" only. "metadata", "ocr" (full extracted text), or "excerpt" (first chunk).`,
			},
			"byte_start": map[string]any{
				"type":        "integer",
				"description": `Optional, action="read" only. Zero-based byte start in the source text artifact.`,
			},
			"byte_end": map[string]any{
				"type":        "integer",
				"description": `Optional, action="read" only. Exclusive byte end in the source text artifact.`,
			},
			"filename": map[string]any{
				"type":        "string",
				"description": `Required when action="store". Display filename / short label.`,
			},
			"content": map[string]any{
				"type":        "string",
				"description": `Required when action="store". For kind="text" the body text; for kind="url" the absolute http(s) URL.`,
			},
			"stages": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []string{"ocr", "ingest"}},
				"description": `Optional, action="reprocess" only. Which stages to re-run (default ["ingest"]). "ocr" requires KindPDF + OCR backend.`,
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "list", RequiredKeys: nil},
			{Name: "read", RequiredKeys: []string{"source_id"}},
			{Name: "store", RequiredKeys: []string{"kind", "filename", "content"}},
			{Name: "reprocess", RequiredKeys: []string{"source_id"}},
			{Name: "delete", RequiredKeys: []string{"source_id"}},
			{Name: "lint", RequiredKeys: nil},
		}),
		// JSON Schema "examples" — concrete shapes models read before
		// the description.
		"examples": []any{
			map[string]any{"action": "list"},
			map[string]any{"action": "read", "source_id": "src_0ec1b02e112f0ca4"},
			map[string]any{"action": "store", "kind": "text", "filename": "note.txt", "content": "plain text body"},
			map[string]any{"action": "store", "kind": "url", "filename": "article", "content": "https://example.com/page"},
			map[string]any{"action": "reprocess", "source_id": "src_0ec1b02e112f0ca4", "stages": []string{"ingest"}},
			map[string]any{"action": "delete", "source_id": "src_0ec1b02e112f0ca4"},
			map[string]any{"action": "lint"},
		},
	}
}

func (t *SourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if rewritten, ok := RewriteVerbKeyAsAction(args, sourceValidActions, sourceActionHints); ok {
		args = rewritten
	}
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "list":
		return t.list.Execute(ctx, args)
	case "read":
		// read_source takes id|source_id; pass through with a fallback.
		if _, ok := args["id"]; !ok {
			if sid, ok := args["source_id"].(string); ok {
				args["id"] = sid
			}
		}
		return t.read.Execute(ctx, args)
	case "store":
		return t.store.Execute(ctx, args)
	case "reprocess":
		return t.doReprocess(ctx, args)
	case "delete":
		if t.delete == nil {
			return "", fmt.Errorf("source delete: tool not wired (no memoryindex)")
		}
		return t.delete.Execute(ctx, args)
	case "lint":
		return t.lint.Execute(ctx, args)
	case "":
		return "", ActionRequiredError("source", sourceValidActions, args, sourceActionHints, "list")
	default:
		return "", UnknownActionError("source", action, sourceValidActions, args)
	}
}

var (
	sourceValidActions = []string{"list", "read", "store", "reprocess", "delete", "lint"}
	sourceActionHints  = []ActionHint{
		// Order most-specific first so the scorer prefers store over read
		// when both content + kind appear, etc.
		{Name: "store", RequiredKeys: []string{"kind", "content"}},
		{Name: "reprocess", RequiredKeys: []string{"stages"}},
		{Name: "delete", RequiredKeys: []string{"confirm"}},
		{Name: "read", RequiredKeys: []string{"source_id"}},
	}
)

// doReprocess dispatches to the OCR + ingest helpers based on the stages
// array. Default is ["ingest"] (most common — re-run LLM extraction on an
// already-OCR'd PDF or extracted text source). Both stages can be requested
// in one call; ocr fires first so ingest reads the freshly-updated
// ocr.md / extract.md.
func (t *SourceTool) doReprocess(ctx context.Context, args map[string]any) (string, error) {
	stages := stringSliceArg(args, "stages")
	if len(stages) == 0 {
		stages = []string{"ingest"}
	}
	var ranOCR bool
	var outputs []string
	for _, stage := range stages {
		switch strings.TrimSpace(stage) {
		case "ocr":
			if t.ocr == nil {
				return "", fmt.Errorf("source reprocess: stage 'ocr' requires OCR backend (not configured)")
			}
			out, err := t.ocr.Execute(ctx, args)
			if err != nil {
				return "", fmt.Errorf("source reprocess: ocr: %w", err)
			}
			outputs = append(outputs, "[ocr] "+out)
			ranOCR = true
		case "ingest":
			if t.ingest == nil {
				return "", fmt.Errorf("source reprocess: stage 'ingest' requires pipeline (not configured)")
			}
			out, err := t.ingest.Execute(ctx, args)
			if err != nil {
				return "", fmt.Errorf("source reprocess: ingest: %w", err)
			}
			outputs = append(outputs, "[ingest] "+out)
		default:
			return "", fmt.Errorf("source reprocess: unknown stage %q (allowed: ocr, ingest): %w", stage, llm.ErrSchemaValidation)
		}
	}
	_ = ranOCR
	return strings.Join(outputs, "\n\n"), nil
}
