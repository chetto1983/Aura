package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// The document half of the surface. It is separate from the memory half on
// purpose: a fact is something the agent concluded and may later have to
// retract, a passage is something a source said and never changes. They share a
// database and nothing else.
//
// These three read and write the shape internal/documents already writes, so a
// file ingested by Aura's own pipeline is searchable here without a second copy,
// and one ingested here is a first-class document to Aura.

// DocumentIngestInput carries a document whose text is already plain.
// Conversion from PDF, xlsx and the rest is the extractor sidecar's job, not
// this server's -- it has no parsers and should not grow any.
type DocumentIngestInput struct {
	ID         string `json:"id" jsonschema:"stable id; re-ingesting the same id replaces the document"`
	Title      string `json:"title" jsonschema:"human-readable name, used when citing a passage"`
	Text       string `json:"text" jsonschema:"the document's text, already converted to markdown or plain text"`
	Source     string `json:"source,omitempty" jsonschema:"where it came from, e.g. the original filename"`
	ChunkRunes int    `json:"chunk_runes,omitempty" jsonschema:"target chunk size in characters; defaults to 1200"`
}

// DocumentIngestOutput reports what was stored.
type DocumentIngestOutput struct {
	ID     string `json:"id"`
	Chunks int    `json:"chunks"`
}

func addDocumentIngestTool(server *mcp.Server, client *arcadedb.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "document_ingest",
		Title: "Ingest a document",
		Description: "Store a document as a chain of passages. Re-ingesting the same id " +
			"replaces it, so a corrected file lands cleanly instead of accumulating.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: false},
	}, documentIngestHandler(client))
}

func documentIngestHandler(
	client *arcadedb.Client,
) mcp.ToolHandlerFor[DocumentIngestInput, DocumentIngestOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in DocumentIngestInput,
	) (*mcp.CallToolResult, DocumentIngestOutput, error) {
		chunks, err := client.IngestDocument(ctx, arcadedb.Document{
			ID:         in.ID,
			Title:      in.Title,
			Text:       in.Text,
			Source:     in.Source,
			ChunkRunes: in.ChunkRunes,
		})
		if err != nil {
			return nil, DocumentIngestOutput{}, fmt.Errorf("document_ingest: %w", err)
		}
		return nil, DocumentIngestOutput{ID: in.ID, Chunks: chunks}, nil
	}
}

// DocumentSearchInput asks the corpus a question.
type DocumentSearchInput struct {
	Query string `json:"query" jsonschema:"the words to look for"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many passages to return; defaults to 5"`
	// NoContext is negative because context is the useful default: a passage
	// without its neighbours frequently starts mid-argument and cannot be quoted.
	NoContext bool `json:"no_context,omitempty" jsonschema:"omit the neighbouring passages"`
}

// DocumentSearchHit is one passage, with enough around it to be read and cited.
type DocumentSearchHit struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title,omitempty"`
	Ordinal    int    `json:"ordinal"`
	Text       string `json:"text"`
	Before     string `json:"before,omitempty" jsonschema:"the preceding passage"`
	After      string `json:"after,omitempty" jsonschema:"the following passage"`
}

// DocumentSearchOutput carries the hits.
type DocumentSearchOutput struct {
	Passages []DocumentSearchHit `json:"passages"`
}

func addDocumentSearchTool(server *mcp.Server, client *arcadedb.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "document_search",
		Title: "Search documents",
		Description: "Find passages by their words, with the neighbouring passages so the " +
			"match can be read and quoted in context.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, documentSearchHandler(client))
}

func documentSearchHandler(
	client *arcadedb.Client,
) mcp.ToolHandlerFor[DocumentSearchInput, DocumentSearchOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in DocumentSearchInput,
	) (*mcp.CallToolResult, DocumentSearchOutput, error) {
		passages, err := client.SearchPassages(ctx, in.Query, in.Limit, !in.NoContext)
		if err != nil {
			return nil, DocumentSearchOutput{}, fmt.Errorf("document_search: %w", err)
		}
		hits := make([]DocumentSearchHit, 0, len(passages))
		for _, passage := range passages {
			hits = append(hits, DocumentSearchHit{
				DocumentID: passage.DocumentID,
				Title:      passage.Title,
				Ordinal:    passage.Ordinal,
				Text:       passage.Text,
				Before:     passage.Before,
				After:      passage.After,
			})
		}
		return nil, DocumentSearchOutput{Passages: hits}, nil
	}
}

// DocumentDeleteInput names the document to remove.
type DocumentDeleteInput struct {
	ID string `json:"id" jsonschema:"the document id to remove, with all its passages"`
}

// DocumentDeleteOutput reports how much went.
type DocumentDeleteOutput struct {
	ID      string `json:"id"`
	Removed int    `json:"removed" jsonschema:"passages removed"`
}

func addDocumentDeleteTool(server *mcp.Server, client *arcadedb.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "document_delete",
		Title:       "Delete a document",
		Description: "Remove a document and every passage it owns.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: new(true),
			IdempotentHint:  false,
		},
	}, documentDeleteHandler(client))
}

func documentDeleteHandler(
	client *arcadedb.Client,
) mcp.ToolHandlerFor[DocumentDeleteInput, DocumentDeleteOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in DocumentDeleteInput,
	) (*mcp.CallToolResult, DocumentDeleteOutput, error) {
		removed, err := client.DeleteDocument(ctx, in.ID)
		if err != nil {
			return nil, DocumentDeleteOutput{}, fmt.Errorf("document_delete: %w", err)
		}
		return nil, DocumentDeleteOutput{ID: in.ID, Removed: removed}, nil
	}
}
