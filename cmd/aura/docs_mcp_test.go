package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func docsMCPSession(t *testing.T, factory docsServiceFactory) *mcp.ClientSession {
	t.Helper()
	server, err := newDocsMCPServer("operator-1", factory)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "document-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestDocsMCPFullEvidenceAndFixedIdentity(t *testing.T) {
	text := strings.Repeat("Full passage content. ", 1000) + "NOT_BUILT BUILDING READY STALE"
	svc := &fakeDocsService{response: documents.RetrievalResponse{
		Status: documents.RetrievalComplete,
		Documents: []documents.RetrievalDocument{{DocumentID: "doc-1", Passages: []documents.RetrievalPassage{
			{Text: text, CitationToken: "document:doc-1@hash#chars=0-22029"},
		}}},
	}}
	session := docsMCPSession(t, fakeDocsFactory(svc))
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "document_search", Arguments: map[string]any{
			"query": "gav.getStatus", "document_ids": []string{"doc-1"}, "limit": 3,
		},
	})
	if err != nil || result.IsError {
		t.Fatalf("call: %v, %#v", err, result)
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(text)) || !bytes.Contains(payload, []byte("document:doc-1@hash")) {
		t.Fatal("complete passage or citation lost in MCP transport")
	}
	if svc.retrievalRequest.IdentityID != "operator-1" || svc.retrievalRequest.Limit != 3 ||
		len(svc.retrievalRequest.DocumentIDs) != 1 || svc.retrievalRequest.DocumentIDs[0] != "doc-1" {
		t.Fatalf("scope changed: %#v", svc.retrievalRequest)
	}
}

func TestDocsMCPIngestUsesExistingHandler(t *testing.T) {
	svc := &fakeDocsService{ingestAsset: assets.Asset{ID: "asset-1", Status: assets.StatusAccepted, FileName: "manual.pdf"}}
	session := docsMCPSession(t, fakeDocsFactory(svc))
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "document_ingest", Arguments: map[string]any{"path": "manual.pdf", "source_id": "stable-source"},
	})
	if err != nil || result.IsError {
		t.Fatalf("call: %v, %#v", err, result)
	}
	if svc.ingestReq.IdentityID != "operator-1" || svc.ingestReq.SourceRef != "stable-source" ||
		svc.ingestReq.SourceKind != assets.SourceCLI {
		t.Fatalf("ingress changed: %#v", svc.ingestReq)
	}
	payload, _ := json.Marshal(result.StructuredContent)
	if !bytes.Contains(payload, []byte(`"status":"accepted"`)) {
		t.Fatalf("acceptance status changed: %s", payload)
	}
}

func TestDocsMCPRejectsInvalidSearchAndPropagatesFailure(t *testing.T) {
	factory := func(context.Context) (docsCLIService, func(), error) {
		return nil, nil, errors.New("database unavailable")
	}
	session := docsMCPSession(t, factory)
	for _, args := range []map[string]any{
		{"query": ""}, {"query": "GAV", "limit": -1}, {"query": "GAV"},
		{"query": "GAV", "identity_id": "another-user"},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "document_search", Arguments: args})
		if err == nil && !result.IsError {
			t.Fatalf("invalid/unavailable request succeeded: %#v", args)
		}
	}
}

func TestDocsMCPManifestAndConfiguration(t *testing.T) {
	factory := fakeDocsFactory(&fakeDocsService{})
	if _, err := newDocsMCPServer("", factory); err == nil {
		t.Fatal("missing operator accepted")
	}
	if _, err := newDocsMCPServer("operator-1", nil); err == nil {
		t.Fatal("missing service accepted")
	}
	if err := runDocsCommand(t.Context(), []string{"mcp", "extra"}, &bytes.Buffer{}, factory); err == nil {
		t.Fatal("extra MCP arguments accepted")
	}
	session := docsMCPSession(t, factory)
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	// Asserted by name, not by count: a manifest is a contract about WHICH tools an
	// operator's client can reach, and a bare number says nothing about a swap.
	manifest := map[string]bool{}
	for _, tool := range listed.Tools {
		manifest[tool.Name] = true
	}
	for _, want := range []string{"document_ingest", "document_search", "document_open"} {
		if !manifest[want] {
			t.Fatalf("manifest is missing %s: %v", want, manifest)
		}
	}
	if len(listed.Tools) != len(manifest) || len(manifest) != 3 {
		t.Fatalf("unexpected manifest: %v", manifest)
	}
	for _, tool := range listed.Tools {
		wantReadOnly := tool.Name == "document_search"
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != wantReadOnly {
			t.Fatalf("wrong effect annotation: %#v", tool)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if bytes.Contains(schema, []byte("identity_id")) {
			t.Fatal("caller identity exposed in input schema")
		}
	}
}
