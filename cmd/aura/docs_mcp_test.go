package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// docsMCPRegistry stands in for the production registry with the SAME native tool types
// the agent runs, over the test double's seams. Registering anything else here would be
// testing a different server than the one that ships.
func docsMCPRegistry(svc *fakeDocsService) *tools.Registry {
	registry := tools.NewRegistry()
	registry.Register(&tools.DocumentSearch{Library: svc})
	// No router: an open therefore denies with sandbox_unavailable, which is what the agent
	// gets when its box is unreachable. Where the file lands is exercised in the tool's own
	// package, against a real box.
	registry.Register(&tools.DocumentOpen{Documents: svc})
	// The paging tool is production's too: a document result over the preview cap is
	// truncated with a footer naming its sidecar, and without this the footer points at
	// nothing a client of this server can call.
	registry.Register(&tools.ReadToolOutput{})
	return registry
}

// docsMCPConfig carries the production preview cap on purpose: a result that would be cut
// short for the model must be cut short here too, or the server measures a bigger answer
// than the agent ever sees.
func docsMCPConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{RunDir: t.TempDir(), ToolPreviewCap: 30000}
}

func docsMCPSession(t *testing.T, svc *fakeDocsService) *mcp.ClientSession {
	t.Helper()
	server, err := newDocsMCPServer("operator-1", docsMCPRegistry(svc), docsMCPConfig(t), fakeDocsFactory(svc))
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
	session := docsMCPSession(t, svc)
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
	session := docsMCPSession(t, svc)
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
	session := docsMCPSession(t, &fakeDocsService{retrieveErr: errors.New("database unavailable")})
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
	svc := &fakeDocsService{}
	factory := fakeDocsFactory(svc)
	registry, cfg := docsMCPRegistry(svc), docsMCPConfig(t)
	for _, missing := range []string{"operator", "registry", "configuration", "service"} {
		operator, reg, conf, fac := "operator-1", registry, cfg, factory
		switch missing {
		case "operator":
			operator = ""
		case "registry":
			reg = nil
		case "configuration":
			conf = nil
		case "service":
			fac = nil
		}
		if _, err := newDocsMCPServer(operator, reg, conf, fac); err == nil {
			t.Fatalf("server built without the %s", missing)
		}
	}
	if err := runDocsCommand(t.Context(), []string{"mcp", "extra"}, &bytes.Buffer{}, factory); err == nil {
		t.Fatal("extra MCP arguments accepted")
	}
	session := docsMCPSession(t, svc)
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
	for _, want := range []string{"document_ingest", "document_search", "document_open", "read_tool_output"} {
		if !manifest[want] {
			t.Fatalf("manifest is missing %s: %v", want, manifest)
		}
	}
	if len(listed.Tools) != len(manifest) || len(manifest) != 4 {
		t.Fatalf("unexpected manifest: %v", manifest)
	}
	// The effect hints of a native tool are ITS OWN descriptors, read off the same Spec the
	// policy gateway reads: a client is told what the runtime believes, not a second opinion
	// formed here. document_ingest has no native twin, so its hint is the one written here.
	for _, tool := range listed.Tools {
		wantReadOnly := false
		if native, ok := docsMCPRegistry(svc).Get(tool.Name); ok {
			wantReadOnly = !native.Spec().Mutating
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != wantReadOnly {
			t.Fatalf("effect annotation is not the tool's own: %#v", tool)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if bytes.Contains(schema, []byte("identity_id")) {
			t.Fatal("caller identity exposed in input schema")
		}
	}
}

// TestDocsMCPPagesASpilledResult walks the path the agent walks when a document result is
// larger than the preview cap: the first call comes back cut on a rune boundary with a
// footer naming its sidecar, and read_tool_output returns the bytes the cut removed.
// Measured 2026-09-09 against the live library, which is why the cap is kept rather than
// lifted for this transport: document_search with neighbours:2 over the default limit
// produced 54,936 bytes, the client received 30,000 of them ending mid-JSON, and the
// footer named a tool the server did not publish.
func TestDocsMCPPagesASpilledResult(t *testing.T) {
	tail := "TAIL_MARKER_REACHABLE_ONLY_BY_PAGING"
	svc := &fakeDocsService{response: documents.RetrievalResponse{
		Status: documents.RetrievalComplete,
		Documents: []documents.RetrievalDocument{{DocumentID: "doc-1", Passages: []documents.RetrievalPassage{
			{Text: strings.Repeat("Passage body. ", 4000) + tail, CitationToken: "document:doc-1@hash#chars=0-56000"},
		}}},
	}}
	session := docsMCPSession(t, svc)
	first, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "document_search", Arguments: map[string]any{"query": "tail"},
	})
	if err != nil || first.IsError {
		t.Fatalf("search: %v, %#v", err, first)
	}
	preview := docsMCPText(t, first)
	if !strings.Contains(preview, "[output truncated") {
		t.Fatalf("the preview cap did not apply: %d bytes back", len(preview))
	}
	if strings.Contains(preview, tail) {
		t.Fatal("the tail survived the cut, so this no longer exercises paging")
	}
	spillID, offset := docsMCPSpillPointer(t, preview)

	rest, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "read_tool_output", Arguments: map[string]any{"tool_call_id": spillID, "offset": offset},
	})
	if err != nil || rest.IsError {
		t.Fatalf("read_tool_output: %v, %#v", err, rest)
	}
	if !strings.Contains(docsMCPText(t, rest), tail) {
		t.Fatal("paging back did not return the bytes the cap removed")
	}
}

func docsMCPText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var joined strings.Builder
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("non-text content %T", content)
		}
		joined.WriteString(text.Text)
	}
	return joined.String()
}

// docsMCPSpillPointer reads the sidecar id and next offset out of the truncation footer,
// which is the only place a client learns them -- exactly as the model is told to.
var docsMCPFooter = regexp.MustCompile(`read_tool_output\(tool_call_id="([^"]+)", offset=(\d+)`)

func docsMCPSpillPointer(t *testing.T, preview string) (string, int) {
	t.Helper()
	found := docsMCPFooter.FindStringSubmatch(preview)
	if found == nil {
		t.Fatalf("no read_tool_output pointer in the footer: %q", preview[max(0, len(preview)-200):])
	}
	offset, err := strconv.Atoi(found[2])
	if err != nil {
		t.Fatal(err)
	}
	return found[1], offset
}

// This server exists to evaluate the document tools, so a client of it must be handed the
// SAME text the agent runtime hands the model -- otherwise an evaluation measures a
// paraphrase. The two had drifted: measured 2026-09-09 by asking a blind agent to
// transcribe what it was given, this server's document_search said only "Retrieve full
// passage evidence, citations and index status", naming neither citation_token nor
// requires_open nor the neighbours parameter, all of which the agent-side description
// explained.
func TestDocsMCPToolsAreDerivedFromTheNativeSpecs(t *testing.T) {
	session := docsMCPSession(t, &fakeDocsService{})
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	native := map[string]tools.Spec{
		"document_search": (&tools.DocumentSearch{}).Spec(),
		"document_open":   (&tools.DocumentOpen{}).Spec(),
	}
	for _, tool := range listed.Tools {
		spec, derived := native[tool.Name]
		if !derived {
			continue
		}
		if tool.Description != spec.Description {
			t.Fatalf("%s was restated instead of derived: mcp=%q native=%q",
				tool.Name, tool.Description, spec.Description)
		}
		// And the parameters with it. A parameter the schema does not offer cannot be used
		// however well the prose explains it: the hand-written struct this replaced handed a
		// client query, limit and document_ids and nothing else, while the prose told it to
		// pass neighbours.
		if got, want := jsonValue(t, tool.InputSchema), jsonValue(t, spec.Parameters); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s parameters were reshaped instead of derived: mcp=%v native=%v",
				tool.Name, got, want)
		}
		delete(native, tool.Name)
	}
	if len(native) != 0 {
		t.Fatalf("tools missing from the manifest: %v", native)
	}
}

func jsonValue(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
