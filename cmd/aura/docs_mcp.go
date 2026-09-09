package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type docsMCPIngestInput struct {
	Path     string `json:"path" jsonschema:"Path to an existing file inside Aura's configured workspace"`
	SourceID string `json:"source_id,omitempty" jsonschema:"Optional stable source identifier"`
}

type docsMCPSearchInput struct {
	Query       string   `json:"query" jsonschema:"Question, exact identifier, topic or filename to search"`
	DocumentIDs []string `json:"document_ids,omitempty" jsonschema:"Restrict retrieval to these returned document IDs"`
	Limit       int      `json:"limit,omitempty" jsonschema:"Maximum documents to return; defaults to 8"`
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
		Instructions: "Use document_ingest to store a workspace file in the operator's library. " +
			"Acceptance is not completed indexing: document_search reports indexed passages and degradation. " +
			"Read the returned passages before answering; a filename match alone is not evidence. " +
			"Both tools use Aura's production document handlers and a fixed operator identity.",
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
		Name: "document_search", Description: "Retrieve full passage evidence, citations and index status from Aura's production retriever.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input docsMCPSearchInput) (*mcp.CallToolResult, map[string]any, error) {
		args := []string{"search", input.Query}
		if input.Limit != 0 {
			args = append(args, "--limit", strconv.Itoa(input.Limit))
		}
		for _, id := range input.DocumentIDs {
			args = append(args, "--document-id", id)
		}
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
