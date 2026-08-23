package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_views_surface_test.go covers the half of bridge_views.go its first test
// file left unmeasured. Measured 2026-08-23: go-mutesting scored bridge_views.go at
// **37.0%** (20/54) against CLAUDE.md's >=70% spot-check floor, and four of its
// functions -- servesResources, viewURIs, readViewDocument and viewDeclaration --
// carried no test reference at all. The text half of viewDescriptor was equally
// unmeasured, which matters more than the number: carrying text is the fix that
// landed the same day, for servers whose tool returns a plain string and whose view
// therefore received null.
//
// It is a SEPARATE file on purpose, not an extension of bridge_views_test.go: that
// file was under active edit in another session while this was written.

// newViewServerAt is newViewServer's shape for a server that serves SEVERAL
// documents, each with its own MIME type -- which is what it takes to prove the
// hydrate loop carries on past one that cannot be read.
func newViewServerAt(t *testing.T, docs map[string]string, meta sdkmcp.Meta, tools ...*sdkmcp.Tool) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	for _, tool := range tools {
		server.AddTool(tool, trivialToolHandler)
	}
	for uri, mime := range docs {
		server.AddResource(
			&sdkmcp.Resource{URI: uri, Name: uri, MIMEType: mime},
			func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
				return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{
					URI: uri, MIMEType: mime, Text: fixtureViewHTML, Meta: meta,
				}}}, nil
			},
		)
	}
	return connectViewSession(t, server)
}

func connectViewSession(t *testing.T, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func boundTool(t *testing.T) *bridgedTool {
	t.Helper()
	srv, _ := newInMemoryMounted(t)
	return &bridgedTool{srv: srv, name: "bound", view: mcp.ViewRef{ResourceURI: fixtureViewURI}}
}

// --- viewDescriptor: the text half, added 2026-08-23 and until now unmeasured ---

// structuredContent is OPTIONAL in MCP. A server whose tool returns a plain string
// never sets it, so a descriptor carrying only the structured half handed such a
// view a null it could not tell from an empty result -- measured on the calendar
// sidecar, whose curated tool returns Task<string>.
func TestViewDescriptorCarriesTextWhenThereIsNoStructuredContent(t *testing.T) {
	descriptor, ok := boundTool(t).viewDescriptor(mcp.ToolPayload{Text: "tre eventi domani"})
	if !ok {
		t.Fatal("a view-bound tool must produce a descriptor")
	}
	if descriptor["text_content"] != "tre eventi domani" {
		t.Fatalf("text_content = %#v, want the tool's own text", descriptor["text_content"])
	}
	if _, carried := descriptor["structured_content"]; carried {
		t.Error("a payload with no structuredContent must carry no structured_content key")
	}
}

func TestViewDescriptorCarriesBothHalvesWhenTheServerSendsBoth(t *testing.T) {
	descriptor, _ := boundTool(t).viewDescriptor(mcp.ToolPayload{
		Text:       "due chat",
		Structured: json.RawMessage(`{"rows":[1,2]}`),
	})
	if descriptor["text_content"] != "due chat" {
		t.Errorf("text_content = %#v", descriptor["text_content"])
	}
	if string(descriptor["structured_content"].(json.RawMessage)) != `{"rows":[1,2]}` {
		t.Errorf("structured_content = %#v", descriptor["structured_content"])
	}
}

// The same cap governs both halves: a view is a panel, not a file viewer. And as
// with the structured half, the DATA is dropped and the descriptor survives.
func TestViewDescriptorOversizeTextIsDroppedAndReported(t *testing.T) {
	huge := strings.Repeat("x", MaxViewPayloadBytes+1)
	descriptor, ok := boundTool(t).viewDescriptor(mcp.ToolPayload{Text: huge})
	if !ok {
		t.Fatal("the descriptor must survive an oversize text")
	}
	if _, carried := descriptor["text_content"]; carried {
		t.Error("an oversize text must not be carried")
	}
	if descriptor["text_dropped_bytes"] != len(huge) {
		t.Errorf("text_dropped_bytes = %#v, want %d", descriptor["text_dropped_bytes"], len(huge))
	}
}

// The cap is EXCLUSIVE, and pinning that costs one test while leaving it unpinned
// costs a silent off-by-one at exactly the size an operator would report as "the
// big one never renders". A payload of exactly MaxViewPayloadBytes rides; one byte
// more does not. Asserting both sides also pins the constant itself.
func TestViewDescriptorCapIsExclusiveOnBothHalves(t *testing.T) {
	bound := boundTool(t)

	exactText := strings.Repeat("t", MaxViewPayloadBytes)
	atCap, _ := bound.viewDescriptor(mcp.ToolPayload{Text: exactText})
	if atCap["text_content"] != exactText {
		t.Error("text of exactly the cap must be carried, not dropped")
	}
	if _, dropped := atCap["text_dropped_bytes"]; dropped {
		t.Error("text of exactly the cap must not be reported as dropped")
	}

	// json.RawMessage of exactly the cap, built as a valid JSON string literal so
	// the value is what a server could really send.
	exactStructured := json.RawMessage(`"` + strings.Repeat("s", MaxViewPayloadBytes-2) + `"`)
	if len(exactStructured) != MaxViewPayloadBytes {
		t.Fatalf("fixture is %d bytes, want exactly %d", len(exactStructured), MaxViewPayloadBytes)
	}
	atCapS, _ := bound.viewDescriptor(mcp.ToolPayload{Structured: exactStructured})
	if _, carried := atCapS["structured_content"]; !carried {
		t.Error("structuredContent of exactly the cap must be carried, not dropped")
	}

	overS := append(json.RawMessage{}, exactStructured...)
	overS = append(overS, ' ')
	overCap, _ := bound.viewDescriptor(mcp.ToolPayload{Structured: overS})
	if _, carried := overCap["structured_content"]; carried {
		t.Error("one byte over the cap must be dropped")
	}
}

// An absent half must leave NO key at all: the cockpit distinguishes "the server
// sent nothing" from "the server sent an empty string" by the key's presence.
func TestViewDescriptorEmptyHalvesCarryNoKeyAtAll(t *testing.T) {
	descriptor, ok := boundTool(t).viewDescriptor(mcp.ToolPayload{})
	if !ok {
		t.Fatal("a view-bound tool must produce a descriptor even with an empty payload")
	}
	for _, key := range []string{"structured_content", "text_content", "payload_dropped_bytes", "text_dropped_bytes"} {
		if _, carried := descriptor[key]; carried {
			t.Errorf("an empty payload must carry no %q", key)
		}
	}
	// A one-byte structured half is NOT empty and must be carried -- the boundary
	// on the other side of the same switch arm.
	one, _ := boundTool(t).viewDescriptor(mcp.ToolPayload{Structured: json.RawMessage(`1`)})
	if _, carried := one["structured_content"]; !carried {
		t.Error("a one-byte structuredContent must be carried")
	}
}

// --- viewDeclaration: which of the two places the framing declaration may live in wins ---

func TestViewDeclarationPrefersThePartOverTheResult(t *testing.T) {
	part := &sdkmcp.ResourceContents{Meta: sdkmcp.Meta{"ui": map[string]any{"prefersBorder": true}}}
	result := &sdkmcp.ReadResourceResult{Meta: sdkmcp.Meta{"ui": map[string]any{"prefersBorder": false}}}

	got := viewDeclaration(part, result)
	ui, _ := got["ui"].(map[string]any)
	if ui == nil || ui["prefersBorder"] != true {
		t.Fatalf("the part's declaration must win over the result's, got %#v", got)
	}
}

// The fallback is the half a `len(part.Meta) > 0` written as `>= 0` would silently
// destroy: every server that hangs its declaration off the RESULT would then be
// read as declaring nothing, and its view would lose its framing with no error.
func TestViewDeclarationFallsBackToTheResultWhenThePartCarriesNone(t *testing.T) {
	result := &sdkmcp.ReadResourceResult{Meta: sdkmcp.Meta{"ui": map[string]any{"prefersBorder": true}}}

	for _, part := range []*sdkmcp.ResourceContents{
		{},
		{Meta: sdkmcp.Meta{}},
	} {
		got := viewDeclaration(part, result)
		ui, _ := got["ui"].(map[string]any)
		if ui == nil || ui["prefersBorder"] != true {
			t.Fatalf("an empty part meta must fall back to the result's, got %#v", got)
		}
	}
}

// --- viewURIs ---

func TestViewURIsAreDedupedAndSorted(t *testing.T) {
	const other = "ui://fixture/aaa.html" // sorts BEFORE fixtureViewURI
	advertised := []*sdkmcp.Tool{
		appTool("list_messages", fixtureViewURI),
		appTool("list_chats", fixtureViewURI), // same document, read once
		appTool("open_thread", other),
		mustTool("no_view", "", nil, nil), // no binding at all
	}

	got := viewURIs(viewsPolicy(), advertised)
	if len(got) != 2 {
		t.Fatalf("URIs = %#v, want the two distinct documents", got)
	}
	if got[0] != other || got[1] != fixtureViewURI {
		t.Errorf("URIs = %#v, want them sorted so a mount log is diffable", got)
	}
}

func TestViewURIsAreEmptyForAMountThatMayNotRender(t *testing.T) {
	advertised := []*sdkmcp.Tool{appTool("list_messages", fixtureViewURI)}
	if got := viewURIs(defaultBridgePolicy("fixture"), advertised); len(got) != 0 {
		t.Fatalf("a mount with no render grant must bind no document, got %#v", got)
	}
}

// --- servesResources ---

func TestServesResourcesReadsThePeersCapability(t *testing.T) {
	withResources := newViewServerAt(t,
		map[string]string{fixtureViewURI: mcp.AppMIMEType}, nil,
		appTool("list_messages", fixtureViewURI))
	if !servesResources(withResources) {
		t.Error("a peer advertising resources must be asked")
	}

	bare := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	bare.AddTool(mustTool("plain", "", nil, nil), trivialToolHandler)
	if servesResources(connectViewSession(t, bare)) {
		t.Error("a peer serving no resources must not be asked for one")
	}
}

// --- readViewDocument ---

func TestReadViewDocumentRefusesAnyOtherMIMEType(t *testing.T) {
	session := newViewServerAt(t, map[string]string{fixtureViewURI: "text/html"}, nil)

	_, err := readViewDocument(context.Background(), session, "fixture", fixtureViewURI)
	if err == nil {
		t.Fatal("a document served as plain text/html must not be read as a view")
	}
	if !strings.Contains(err.Error(), mcp.AppMIMEType) {
		t.Errorf("the error must name the type it required, got %v", err)
	}
}

func TestReadViewDocumentSurfacesTheReadFailure(t *testing.T) {
	session := newViewServerAt(t, map[string]string{fixtureViewURI: mcp.AppMIMEType}, nil)

	if _, err := readViewDocument(context.Background(), session, "fixture", "ui://fixture/absent.html"); err == nil {
		t.Fatal("a URI the server does not serve must surface as an error, not an empty document")
	}
}

func TestReadViewDocumentCarriesTheDocumentAndItsDeclaration(t *testing.T) {
	meta := sdkmcp.Meta{"ui": map[string]any{"prefersBorder": true}}
	session := newViewServerAt(t, map[string]string{fixtureViewURI: mcp.AppMIMEType}, meta)

	doc, err := readViewDocument(context.Background(), session, "fixture", fixtureViewURI)
	if err != nil {
		t.Fatalf("readViewDocument: %v", err)
	}
	if doc.Server != "fixture" || doc.ResourceURI != fixtureViewURI || doc.HTML != fixtureViewHTML {
		t.Fatalf("document = %#v", doc)
	}
	if !doc.Policy.PrefersBorder {
		t.Error("the declaration on the content part must reach the document")
	}
}

// --- hydrateViews: the loop must carry on, and the guard must be all three arms ---

// `continue`, not `break`. One unreadable document among several is a decoration
// that failed; stopping there would silently cost every LATER server document its
// view, and the sorted read order makes which ones lose deterministic rather than
// merely unlucky.
func TestHydrateViewsCarriesOnPastADocumentItCannotRead(t *testing.T) {
	const broken = "ui://fixture/aaa.html" // sorts FIRST, so it is read first
	advertised := []*sdkmcp.Tool{
		appTool("open_thread", broken),
		appTool("list_messages", fixtureViewURI),
	}
	session := newViewServerAt(t, map[string]string{
		broken:         "text/html", // wrong type: this read fails
		fixtureViewURI: mcp.AppMIMEType,
	}, nil, advertised...)

	catalog := mcp.NewViewCatalog()
	hydrateViews(context.Background(), session, "fixture", catalog, advertised, viewsPolicy())

	if _, ok := catalog.Get("fixture", broken); ok {
		t.Error("the unreadable document must not be catalogued")
	}
	if _, ok := catalog.Get("fixture", fixtureViewURI); !ok {
		t.Fatal("a document AFTER an unreadable one must still be catalogued")
	}
}

func TestHydrateViewsNoOpWhenThePeerServesNoResources(t *testing.T) {
	advertised := []*sdkmcp.Tool{appTool("list_messages", fixtureViewURI)}
	bare := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	for _, tool := range advertised {
		bare.AddTool(tool, trivialToolHandler)
	}

	catalog := mcp.NewViewCatalog()
	hydrateViews(context.Background(), connectViewSession(t, bare), "fixture", catalog, advertised, viewsPolicy())

	if len(catalog.URIs()) != 0 {
		t.Fatalf("a peer with no resources must leave the catalog empty, got %#v", catalog.URIs())
	}
}
