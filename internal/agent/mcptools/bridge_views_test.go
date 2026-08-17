package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

const (
	fixtureViewURI  = "ui://fixture/thread.html"
	fixtureViewHTML = `<!doctype html><html><head></head><body>view</body></html>`
)

func viewsPolicy() bridgePolicy { return bridgePolicy{views: true} }

// appTool builds a tool declaring the MCP Apps binding in the shape BOTH known
// implementations emit: the official ext-apps SDK (`registerAppTool`, Microsoft's
// samples) and our own fork write `_meta.ui.resourceUri` and nothing else.
func appTool(name, uri string) *sdkmcp.Tool {
	tool := mustTool(name, "a view-bound tool", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true})
	tool.Meta = sdkmcp.Meta{"ui": map[string]any{"resourceUri": uri}}
	return tool
}

func TestViewRefFor_GatedOnTheMountPolicy(t *testing.T) {
	tool := appTool("list_messages", fixtureViewURI)

	if ref := viewRefFor(viewsPolicy(), tool); ref.ResourceURI != fixtureViewURI {
		t.Fatalf("a permitted mount must read the binding, got %#v", ref)
	}
	// The default policy carries no trust class, so it renders nothing. This is
	// the fail-closed default every test double and every plain `mcpServers` entry
	// gets, and it must stay that way.
	if ref := viewRefFor(defaultBridgePolicy("fixture"), tool); ref.ResourceURI != "" {
		t.Fatalf("a mount with no trust class must read no binding, got %#v", ref)
	}
	if ref := viewRefFor(viewsPolicy(), mustTool("plain", "", nil, nil)); ref.ResourceURI != "" {
		t.Fatalf("a tool declaring no view must yield none, got %#v", ref)
	}
}

// A tool aiming the host somewhere other than ui:// is a misdeclaration, and the
// gate is in mcp.ViewRefFromMeta — asserted here too because THIS is the call
// site a server actually reaches.
func TestViewRefFor_RejectsANonAppScheme(t *testing.T) {
	tool := appTool("evil", "https://evil.test/app.html")
	if ref := viewRefFor(viewsPolicy(), tool); ref.ResourceURI != "" {
		t.Fatalf("a non-ui:// binding must not resolve, got %#v", ref)
	}
}

// newViewServer pairs a real in-memory server that serves one view resource,
// declaring its framing on the CONTENT PART — where the official ext-apps SDK
// puts it (registerAppResource writes contents[0]._meta.ui).
func newViewServer(t *testing.T, mime string, meta sdkmcp.Meta, tools ...*sdkmcp.Tool) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	for _, tool := range tools {
		server.AddTool(tool, trivialToolHandler)
	}
	server.AddResource(
		&sdkmcp.Resource{URI: fixtureViewURI, Name: "thread", MIMEType: mime},
		func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{
				URI: fixtureViewURI, MIMEType: mime, Text: fixtureViewHTML, Meta: meta,
			}}}, nil
		},
	)

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

func TestHydrateViews_CatalogsTheDocumentAndItsDeclaration(t *testing.T) {
	declaration := sdkmcp.Meta{"ui": map[string]any{
		"prefersBorder": true,
		"csp":           map[string]any{"frameDomains": []any{"https://learn-video.azurefd.net"}},
	}}
	advertised := []*sdkmcp.Tool{appTool("list_messages", fixtureViewURI), appTool("list_chats", fixtureViewURI)}
	session := newViewServer(t, mcp.AppMIMEType, declaration, advertised...)

	catalog := mcp.NewViewCatalog()
	hydrateViews(context.Background(), session, "fixture", catalog, advertised, viewsPolicy())

	doc, ok := catalog.Get("fixture", fixtureViewURI)
	if !ok {
		t.Fatal("the view must be catalogued")
	}
	if doc.HTML != fixtureViewHTML {
		t.Fatalf("HTML = %q", doc.HTML)
	}
	if !doc.Policy.PrefersBorder {
		t.Error("the display hint on the content part must be read")
	}
	if len(doc.Policy.FrameDomains) != 1 || doc.Policy.FrameDomains[0] != "https://learn-video.azurefd.net" {
		t.Errorf("FrameDomains = %#v", doc.Policy.FrameDomains)
	}
	// Two tools pointing at one document is the common case; it must be read once.
	if got := catalog.URIs(); len(got) != 1 {
		t.Errorf("URIs = %#v, want exactly one entry", got)
	}
}

// A `ui://` resource is served as exactly one MIME type. Rendering one that
// arrived under another would let a server smuggle a document past the single
// declaration that says it is meant to be rendered.
func TestHydrateViews_RefusesAWrongMIMEType(t *testing.T) {
	advertised := []*sdkmcp.Tool{appTool("list_messages", fixtureViewURI)}
	session := newViewServer(t, "text/html", nil, advertised...)

	catalog := mcp.NewViewCatalog()
	hydrateViews(context.Background(), session, "fixture", catalog, advertised, viewsPolicy())

	if _, ok := catalog.Get("fixture", fixtureViewURI); ok {
		t.Fatal("a document served as plain text/html must not be catalogued")
	}
}

func TestHydrateViews_NoOpWhenTheMountMayNotRender(t *testing.T) {
	advertised := []*sdkmcp.Tool{appTool("list_messages", fixtureViewURI)}
	session := newViewServer(t, mcp.AppMIMEType, nil, advertised...)

	catalog := mcp.NewViewCatalog()
	hydrateViews(context.Background(), session, "fixture", catalog, advertised, defaultBridgePolicy("fixture"))
	if len(catalog.URIs()) != 0 {
		t.Fatal("a mount with no render grant must catalogue nothing")
	}
	// A nil catalog is the "renders nothing" host; it must not panic.
	hydrateViews(context.Background(), session, "fixture", nil, advertised, viewsPolicy())
}

func TestViewDescriptor(t *testing.T) {
	srv, _ := newInMemoryMounted(t, mustTool("plain", "", nil, nil))

	bound := &bridgedTool{srv: srv, name: "list_messages", view: mcp.ViewRef{ResourceURI: fixtureViewURI}}
	descriptor, ok := bound.viewDescriptor(mcp.ToolPayload{Structured: json.RawMessage(`{"rows":[1]}`)})
	if !ok {
		t.Fatal("a view-bound tool must produce a descriptor")
	}
	if descriptor["server"] != "fixture" || descriptor["resource_uri"] != fixtureViewURI {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if string(descriptor["structured_content"].(json.RawMessage)) != `{"rows":[1]}` {
		t.Fatalf("structured content = %#v", descriptor["structured_content"])
	}

	unbound := &bridgedTool{srv: srv, name: "plain"}
	if _, ok := unbound.viewDescriptor(mcp.ToolPayload{}); ok {
		t.Fatal("a tool with no view must produce no descriptor")
	}
}

// An oversize payload drops the DATA, never the descriptor: the view renders its
// empty state, which reads as "nothing to show" — while dropping the descriptor
// would look to the cockpit exactly like a server that declared no view at all.
func TestViewDescriptor_OversizePayloadKeepsTheDescriptor(t *testing.T) {
	srv, _ := newInMemoryMounted(t)
	bound := &bridgedTool{srv: srv, name: "big", view: mcp.ViewRef{ResourceURI: fixtureViewURI}}

	huge := json.RawMessage(`["` + strings.Repeat("x", MaxViewPayloadBytes) + `"]`)
	descriptor, ok := bound.viewDescriptor(mcp.ToolPayload{Structured: huge})
	if !ok {
		t.Fatal("the descriptor must survive an oversize payload")
	}
	if _, carried := descriptor["structured_content"]; carried {
		t.Error("an oversize payload must not be carried")
	}
	if descriptor["payload_dropped_bytes"] != len(huge) {
		t.Errorf("the drop must be reported, got %#v", descriptor["payload_dropped_bytes"])
	}
}

// A view calls back with no human in the loop and no agent turn around it, so it
// gets a strictly smaller grant than the model has.
func TestCallReadOnlyTool_RefusesAnythingNotReadOnly(t *testing.T) {
	readOnly := mustTool("list_messages", "", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true})
	// No annotations at all is the fail-closed mutating+destructive default.
	mutating := mustTool("send_message", "", nil, nil)
	srv, _ := newInMemoryMounted(t, readOnly, mutating)
	if _, err := Bridge(context.Background(), "fixture", srv); err != nil {
		t.Fatalf("Bridge: %v", err)
	}

	if _, err := srv.CallReadOnlyTool(context.Background(), "list_messages", nil); err != nil {
		t.Fatalf("a read-only tool must be callable from a view: %v", err)
	}
	if _, err := srv.CallReadOnlyTool(context.Background(), "send_message", nil); err == nil {
		t.Fatal("a mutating tool must be refused")
	}
	// toolIsReadOnly fails closed for a name the server never advertised, so the
	// same line that refuses a dangerous tool refuses an unknown one.
	if _, err := srv.CallReadOnlyTool(context.Background(), "never_advertised", nil); err == nil {
		t.Fatal("an unadvertised tool must be refused")
	}
}

func TestViewCallers_ResolvesOnlyMountedServers(t *testing.T) {
	readOnly := mustTool("list_messages", "", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true})
	srv, _ := newInMemoryMounted(t, readOnly)
	if _, err := Bridge(context.Background(), "fixture", srv); err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	callers := ViewCallers{"fixture": srv}

	if _, err := callers.CallForView(context.Background(), "fixture", "list_messages", nil); err != nil {
		t.Fatalf("CallForView: %v", err)
	}
	if _, err := callers.CallForView(context.Background(), "other", "list_messages", nil); err == nil {
		t.Fatal("a server with no callback entry must be refused, not silently empty")
	}
}

// The whole point of the change: a view-bound call must carry the server's
// structuredContent out of the bridge, which CallToolText flattened away.
func TestExecute_CarriesTheViewDescriptorOnTheResultMeta(t *testing.T) {
	tool := appTool("list_messages", fixtureViewURI)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(tool, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{
			Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: "2 messages"}},
			StructuredContent: map[string]any{"messages": []any{"a", "b"}},
		}, nil
	})

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", func(_, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		return connectClient(hctx, clientTransport, o)
	})
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	callCtx := tools.WithToolCallContext(ctx, "sess", "tc1", t.TempDir(), 2048)
	bridged := bridgeToolsWithPolicy("fixture", srv, []*sdkmcp.Tool{tool}, 0, viewsPolicy())
	srv.trackBridgedTools(bridged)
	res, err := bridged[0].Execute(callCtx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Preview != "2 messages" {
		t.Errorf("the model still reads the text: %q", res.Preview)
	}
	if res.Meta == nil {
		t.Fatal("a view-bound result must carry the descriptor on its Meta")
	}
	descriptor, ok := (*res.Meta)[viewMetaKey].(map[string]any)
	if !ok {
		t.Fatalf("Meta = %#v", *res.Meta)
	}
	if string(descriptor["structured_content"].(json.RawMessage)) != `{"messages":["a","b"]}` {
		t.Fatalf("structured content = %#v", descriptor["structured_content"])
	}

	// The same tool on a mount that may not render carries nothing: the model's
	// view of the call is byte-identical, only the descriptor disappears.
	plain := bridgeToolsWithPolicy("fixture", srv, []*sdkmcp.Tool{tool}, 0, defaultBridgePolicy("fixture"))
	plainRes, err := plain[0].Execute(callCtx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if plainRes.Meta != nil {
		t.Fatalf("an ungranted mount must attach no descriptor, got %#v", *plainRes.Meta)
	}
}
