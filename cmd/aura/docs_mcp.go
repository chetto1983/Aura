package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type docsMCPIngestInput struct {
	Path     string `json:"path" jsonschema:"Path to an existing file inside Aura's configured workspace"`
	SourceID string `json:"source_id,omitempty" jsonschema:"Optional stable source identifier"`
}

type docsMCPOpenInput struct {
	DocumentID string `json:"document_id" jsonschema:"Document id from a document_search hit (doc_...)"`
	FileName   string `json:"file_name,omitempty" jsonschema:"Optional bare name for the written copy; defaults to the original file name"`
}

type docsMCPSearchInput struct {
	Query       string   `json:"query" jsonschema:"Question, exact identifier, topic or filename to search"`
	DocumentIDs []string `json:"document_ids,omitempty" jsonschema:"Restrict retrieval to these returned document IDs"`
	Limit       int      `json:"limit,omitempty" jsonschema:"Maximum documents to return; defaults to 8"`
	Neighbours  int      `json:"neighbours,omitempty" jsonschema:"Also return this many passages either side of every hit, 0-3; use it when a hit is cut mid-table or mid-definition and the rest of it is in the adjacent chunk"`
}

func runDocsMCP(ctx context.Context, factory docsServiceFactory) error {
	server, err := newDocsMCPServer(identityctx.IdentityID(ctx), factory)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

func newDocsMCPServer(operatorID string, factory docsServiceFactory) (*mcp.Server, error) {
	if strings.TrimSpace(operatorID) == "" || factory == nil {
		return nil, fmt.Errorf("documents MCP requires the operator identity and document service")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "aura-documents", Version: "1.0.0"}, &mcp.ServerOptions{
		// What a RESULT means belongs to the tools, not here: a client shows these
		// instructions once and a model reads a tool description every time it considers
		// calling it. This says only what is true of the server itself.
		Instructions: "This server exposes the operator's document library through Aura's own " +
			"production handlers, under one fixed operator identity. Ingest with document_ingest, " +
			"find with document_search, and read the file itself with document_open. Accepting an " +
			"ingest is not the same as having indexed it: document_search is what reports the " +
			"indexed passages and any degradation.",
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "document_ingest", Description: "Ingest a workspace file through Aura's production document pipeline.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input docsMCPIngestInput) (*mcp.CallToolResult, map[string]any, error) {
		args := []string{"ingest"}
		if input.SourceID != "" {
			args = append(args, "--source-id", input.SourceID)
		}
		args = append(args, "--", input.Path)
		return docsMCPCall(ctx, operatorID, args, factory)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "document_search", Description: documents.SearchToolContract,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input docsMCPSearchInput) (*mcp.CallToolResult, map[string]any, error) {
		args := []string{"search", input.Query}
		if input.Limit != 0 {
			args = append(args, "--limit", strconv.Itoa(input.Limit))
		}
		if input.Neighbours != 0 {
			args = append(args, "--neighbours", strconv.Itoa(input.Neighbours))
		}
		for _, id := range input.DocumentIDs {
			args = append(args, "--document-id", id)
		}
		return docsMCPCall(ctx, operatorID, args, factory)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "document_open",
		Description: documents.OpenToolContract +
			" The file is written onto the workspace filesystem this server can reach; read it from " +
			"the path returned here.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input docsMCPOpenInput) (*mcp.CallToolResult, map[string]any, error) {
		args := []string{"open"}
		if input.FileName != "" {
			args = append(args, "--file-name", input.FileName)
		}
		args = append(args, "--", input.DocumentID)
		return docsMCPCall(ctx, operatorID, args, factory)
	})
	return server, nil
}

func docsMCPCall(ctx context.Context, operatorID string, args []string, factory docsServiceFactory) (*mcp.CallToolResult, map[string]any, error) {
	var output bytes.Buffer
	if err := runDocsCommand(identityctx.WithIdentityID(ctx, operatorID), args, &output, factory); err != nil {
		return nil, nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return nil, nil, fmt.Errorf("decode document result: %w", err)
	}
	return nil, result, nil
}
