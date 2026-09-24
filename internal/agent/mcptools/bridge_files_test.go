package mcptools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// recordingSink stands in for tools.MCPFileSink: it records what it was handed and
// reports each available part as written under one fixed directory.
type recordingSink struct {
	server string
	parts  []mcp.FilePart
}

func (s *recordingSink) Materialize(_ context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome {
	s.server, s.parts = server, parts
	out := make([]mcp.FileOutcome, len(parts))
	for i, part := range parts {
		if part.Unavailable != "" {
			out[i] = part.NotMaterialized("")
			continue
		}
		out[i] = mcp.FileOutcome{
			Path: "/workspace/mcp-files/req-1/fixture/" + part.Name, Name: part.Name,
			MIMEType: part.MIMEType, SizeBytes: int64(len(part.Data)),
		}
	}
	return out
}

const fixtureFileTemplate = "fixture://files/{name}"

// mountReturning mounts a fixture whose one tool, fetch, answers with content. The
// tool is declared at mount and its handler replaced after, so the advertised tool
// set never drifts.
func mountReturning(t *testing.T, content ...sdkmcp.Content) (*MountedServer, *sdkmcp.Server) {
	t.Helper()
	tool := mustTool("fetch", "", nil, nil)
	srv, server := newInMemoryMounted(t, tool)
	server.AddTool(tool, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: content}, nil
	})
	return srv, server
}

// serveFiles answers every read of fixture://files/{name} with body typed
// contentsMIME, and counts the reads.
func serveFiles(server *sdkmcp.Server, contentsMIME string, body []byte, reads *atomic.Int32) {
	server.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{URITemplate: fixtureFileTemplate, Name: "files", MIMEType: contentsMIME},
		func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			reads.Add(1)
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: contentsMIME, Blob: body},
			}}, nil
		})
}

// describeParts renders parts for a failure message without their bytes, which run to
// tens of MiB in the call-cap tests.
func describeParts(parts ...mcp.FilePart) string {
	described := make([]string, len(parts))
	for i, part := range parts {
		described[i] = fmt.Sprintf("{%s %s data:%d size:%d unavailable:%q}", part.Name, part.MIMEType, len(part.Data), part.Size, part.Unavailable)
	}
	return strings.Join(described, " ")
}

// executeFetch runs fetch the way the agent does, with previewCap as the preview cap.
func executeFetch(t *testing.T, srv *MountedServer, previewCap int) tools.ToolResult {
	t.Helper()
	bridged := bridgeToolsWithPolicy("fixture", srv, []*sdkmcp.Tool{mustTool("fetch", "", nil, nil)}, 0, defaultBridgePolicy("fixture"))
	ctx := tools.WithToolCallContext(t.Context(), "sess", "tc1", t.TempDir(), previewCap)
	res, err := bridged[0].Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return res
}

func TestCallToolReadsALinkBackOnTheCallingSession(t *testing.T) {
	srv, server := mountReturning(t,
		&sdkmcp.TextContent{Text: `{"attachmentId":"abc"}`},
		&sdkmcp.ResourceLink{URI: "fixture://files/abc", Name: "invoice.pdf", MIMEType: "application/pdf", Size: new(int64(8))},
	)
	var reads atomic.Int32
	serveFiles(server, mcp.OctetStream, []byte("%PDF-1.7"), &reads)

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	want := []mcp.FilePart{{Name: "invoice.pdf", MIMEType: "application/pdf", Data: []byte("%PDF-1.7")}}
	if !reflect.DeepEqual(payload.Files, want) {
		t.Fatalf("Files = %#v, want %#v", payload.Files, want)
	}
	if payload.Links != nil || reads.Load() != 1 {
		t.Fatalf("links left %v, reads %d; want resolved, read once", payload.Links, reads.Load())
	}
}

func TestCallToolNeverReadsALinkOverTheCap(t *testing.T) {
	srv, server := mountReturning(t, &sdkmcp.ResourceLink{URI: "fixture://files/big", Name: "big.zip", Size: new(int64(mcp.MaxFileBytes + 1))})
	var reads atomic.Int32
	serveFiles(server, "application/zip", []byte("PK"), &reads)

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if len(payload.Files) != 1 || payload.Files[0].Unavailable != mcp.FileCapExceeded(mcp.MaxFileBytes+1) || reads.Load() != 0 {
		t.Fatalf("Files = %+v, reads %d; want refused unread", payload.Files, reads.Load())
	}
}

// The sink refuses a call whose files add up to more than the call cap, all or nothing,
// so a byte read past it is a byte read for nothing: the third 25 MiB link takes the
// total to 75 MiB, and the fourth is never asked for.
func TestCallToolStopsReadingLinksPastTheCallCap(t *testing.T) {
	links := make([]sdkmcp.Content, 4)
	for i := range links {
		links[i] = &sdkmcp.ResourceLink{URI: fmt.Sprintf("fixture://files/part%d", i+1), Name: fmt.Sprintf("part%d.bin", i+1)}
	}
	srv, server := mountReturning(t, links...)
	var reads atomic.Int32
	serveFiles(server, mcp.OctetStream, make([]byte, mcp.MaxFileBytes), &reads)

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if reads.Load() != 3 || len(payload.Files) != 4 {
		t.Fatalf("reads %d, files %d; want 3 reads and 4 parts", reads.Load(), len(payload.Files))
	}
	for _, read := range payload.Files[:3] {
		if len(read.Data) != mcp.MaxFileBytes {
			t.Fatalf("%s carries %d bytes, want %d", read.Name, len(read.Data), mcp.MaxFileBytes)
		}
	}
	if refused := payload.Files[3]; refused.Name != "part4.bin" || refused.Data != nil || refused.Unavailable != mcp.CallCapExceeded() {
		t.Fatalf("the fourth link = %s, want part4.bin refused unread for the call cap", describeParts(refused))
	}
}

// Only bytes count against the call cap: the inline files' Data does, and a part that
// has none does not, whatever size it advertises.
func TestResolveLinksCountsOnlyBytesAgainstTheCallCap(t *testing.T) {
	cases := []struct {
		name      string
		inline    mcp.FilePart
		wantReads int32
		want      mcp.FilePart
	}{
		{
			name:      "inline bytes over the cap leave no budget",
			inline:    mcp.FilePart{Name: "big.bin", Data: make([]byte, mcp.MaxCallFileBytes+1)},
			wantReads: 0,
			want:      mcp.FilePart{Name: "a.bin", MIMEType: "image/png", Size: 7, Unavailable: mcp.CallCapExceeded()},
		},
		{
			name:      "an unavailable part carries no bytes, whatever size it advertises",
			inline:    mcp.FilePart{Name: "gone.bin", Unavailable: "read failed: expired", Size: mcp.MaxCallFileBytes + 1},
			wantReads: 1,
			want:      mcp.FilePart{Name: "a.bin", MIMEType: "image/png", Data: []byte("x")},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, server := mountReturning(t)
			var reads atomic.Int32
			serveFiles(server, mcp.OctetStream, []byte("x"), &reads)
			session, err := srv.currentSession()
			if err != nil {
				t.Fatalf("currentSession: %v", err)
			}

			got := resolveLinks(t.Context(), session, mcp.ToolPayload{
				Files: []mcp.FilePart{c.inline},
				Links: []*sdkmcp.ResourceLink{{URI: "fixture://files/a", Name: "a.bin", MIMEType: "image/png", Size: new(int64(7))}},
			})

			if reads.Load() != c.wantReads || len(got.Files) != 2 || !reflect.DeepEqual(got.Files[1], c.want) {
				t.Fatalf("reads %d, files %s; want %d reads and the link as %s", reads.Load(), describeParts(got.Files...), c.wantReads, describeParts(c.want))
			}
		})
	}
}

func TestResolveLinksStopsReadingWhenTheContextIsDone(t *testing.T) {
	srv, server := mountReturning(t)
	var reads atomic.Int32
	serveFiles(server, mcp.OctetStream, []byte("x"), &reads)
	session, err := srv.currentSession()
	if err != nil {
		t.Fatalf("currentSession: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := resolveLinks(ctx, session, mcp.ToolPayload{Links: []*sdkmcp.ResourceLink{
		{URI: "fixture://files/a", Name: "a.bin"}, {URI: "fixture://files/b", Name: "b.bin"},
	}})

	if reads.Load() != 0 || len(got.Files) != 2 {
		t.Fatalf("reads %d, files %s; want no read and both links reported", reads.Load(), describeParts(got.Files...))
	}
	for _, part := range got.Files {
		if part.Data != nil || part.Unavailable != "read failed: "+context.Canceled.Error() {
			t.Fatalf("part = %s, want the context's error as its reason", describeParts(part))
		}
	}
}

func TestCallToolReportsAFailedLinkReadAsTheFileNotTheCall(t *testing.T) {
	srv, server := mountReturning(t,
		&sdkmcp.TextContent{Text: "still useful"},
		&sdkmcp.ResourceLink{URI: "fixture://files/gone", Name: "gone.pdf"},
	)
	server.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{URITemplate: fixtureFileTemplate, Name: "files"},
		func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
		})

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("a failed link read must not fail the call: %v", err)
	}

	if payload.Text != "still useful" || len(payload.Files) != 1 || !strings.HasPrefix(payload.Files[0].Unavailable, "read failed: ") {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestPreferredMIMETakesTheMoreSpecificType(t *testing.T) {
	cases := []struct{ contents, link, want string }{
		{"application/pdf", "image/png", "application/pdf"},
		{mcp.OctetStream, "image/png", "image/png"},
		{"", "image/png", "image/png"},
		{"", "", mcp.OctetStream},
		{mcp.OctetStream, "", mcp.OctetStream},
	}
	for _, c := range cases {
		if got := preferredMIME(c.contents, c.link); got != c.want {
			t.Errorf("preferredMIME(%q, %q) = %q, want %q", c.contents, c.link, got, c.want)
		}
	}
}

func TestLinkFileRefusesOversizedAndEmptyReads(t *testing.T) {
	link := &sdkmcp.ResourceLink{URI: "fixture://files/abc", MIMEType: "image/jpeg"}
	big := &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: link.URI, Blob: make([]byte, mcp.MaxFileBytes+1)}}}
	if got := linkFile(link, big); got.Unavailable != mcp.FileCapExceeded(mcp.MaxFileBytes+1) || got.Data != nil || got.Size != mcp.MaxFileBytes+1 {
		t.Fatalf("oversized read = %+v", got)
	}
	for _, empty := range []*sdkmcp.ReadResourceResult{nil, {}} {
		if got := linkFile(link, empty); got.Unavailable != "the server returned no contents" {
			t.Fatalf("empty read = %+v", got)
		}
	}
	text := &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: link.URI, Text: "hi"}}}
	if got := linkFile(link, text); got.Name != "abc" || string(got.Data) != "hi" || got.MIMEType != "image/jpeg" {
		t.Fatalf("a nameless link takes the URI's last segment: %+v", got)
	}
}

func TestBridgedToolReportsFilePathsAndNeverTheBytes(t *testing.T) {
	secret := []byte("attachment-bytes-that-must-not-reach-the-model")
	srv, server := mountReturning(t,
		&sdkmcp.TextContent{Text: "one image, one link"},
		&sdkmcp.ImageContent{Data: secret, MIMEType: "image/png"},
		&sdkmcp.ResourceLink{URI: "fixture://files/abc", Name: "invoice.pdf"},
	)
	var reads atomic.Int32
	serveFiles(server, "application/pdf", secret, &reads)
	sink := &recordingSink{}
	srv.files = sink

	res := executeFetch(t, srv, 2048)

	if sink.server != "fixture" || len(sink.parts) != 2 {
		t.Fatalf("sink got server %q, %d parts; want fixture, 2", sink.server, len(sink.parts))
	}
	for _, leak := range []string{string(secret), base64.StdEncoding.EncodeToString(secret)} {
		if strings.Contains(res.Preview, leak) {
			t.Fatalf("the model saw the file's bytes: %q", res.Preview)
		}
	}
	for _, want := range []string{"one image, one link\n\n{\"files\":[", `"path":"/workspace/mcp-files/req-1/fixture/invoice.pdf"`, filesNote} {
		if !strings.Contains(res.Preview, want) {
			t.Fatalf("preview %q lacks %q", res.Preview, want)
		}
	}
}

func TestFilesFooterSurvivesASmallPreviewCap(t *testing.T) {
	srv, _ := mountReturning(t,
		&sdkmcp.TextContent{Text: strings.Repeat("email body line\n", 400)},
		&sdkmcp.ImageContent{Data: []byte("png"), MIMEType: "image/png"},
	)
	srv.files = &recordingSink{}

	res := executeFetch(t, srv, 512)

	if !res.Truncated || !strings.Contains(res.Preview, `"path":"/workspace/mcp-files/req-1/fixture/"`) {
		t.Fatalf("a long text truncated the path away: %q", res.Preview)
	}
}

// A path is handed to the model to be copied into rm or document_open, so the footer
// must not turn & < > into unicode escapes the way json.Marshal does.
func TestFilesFooterKeepsHTMLCharactersInPathsLiteral(t *testing.T) {
	srv, server := mountReturning(t,
		&sdkmcp.ResourceLink{URI: "fixture://files/qa", Name: "Q&A.pdf"},
		&sdkmcp.ResourceLink{URI: "fixture://files/draft", Name: "<draft>.pdf"},
	)
	var reads atomic.Int32
	serveFiles(server, "application/pdf", []byte("%PDF-1.7"), &reads)
	srv.files = &recordingSink{}

	res := executeFetch(t, srv, 2048)

	for _, want := range []string{`/Q&A.pdf"`, `/<draft>.pdf"`} {
		if !strings.Contains(res.Preview, want) {
			t.Fatalf("preview %q lacks %q", res.Preview, want)
		}
	}
	for _, char := range "&<>" {
		escaped := fmt.Sprintf(`\u%04x`, char)
		if strings.Contains(res.Preview, escaped) {
			t.Fatalf("preview %q carries the escape %s, so its path cannot be typed", res.Preview, escaped)
		}
	}
}

func TestAMountWithoutAFileSinkSaysSoAndKeepsTheText(t *testing.T) {
	srv, _ := mountReturning(t,
		&sdkmcp.TextContent{Text: "hi"},
		&sdkmcp.ImageContent{Data: []byte("png"), MIMEType: "image/png"},
	)

	res := executeFetch(t, srv, 2048)

	if !strings.HasPrefix(res.Preview, "hi\n\n") || !strings.Contains(res.Preview, `"not_materialized":"this host has no workspace for MCP files"`) {
		t.Fatalf("preview = %q", res.Preview)
	}
	if strings.Contains(res.Preview, `"note"`) {
		t.Fatalf("no file was written, so there is nothing to delete: %q", res.Preview)
	}
}

func TestALinkOnlyResultStillReachesTheModel(t *testing.T) {
	srv, server := mountReturning(t, &sdkmcp.ResourceLink{URI: "fixture://files/abc", Name: "photo.jpg", MIMEType: "image/jpeg"})
	var reads atomic.Int32
	serveFiles(server, mcp.OctetStream, []byte("\xff\xd8\xff"), &reads)
	srv.files = &recordingSink{}

	res := executeFetch(t, srv, 2048)

	if !strings.HasPrefix(res.Preview, `{"files":[{"path":"/workspace/mcp-files/req-1/fixture/photo.jpg"`) {
		t.Fatalf("preview = %q", res.Preview)
	}
}

// An OAuth-protected server gives every identity its own session, and a link names a
// resource on the session that returned it. Both identities' copies of the server
// answer the same link URI with different bytes, so a call whose link was read on the
// wrong session hands the sink the wrong identity's file. The mount also has to carry
// its sink onto the pooled parent, which is where the bridged tools look for it.
func TestIdentityScopedMountReadsLinksOnTheCallersSession(t *testing.T) {
	var mu sync.Mutex
	var serverSessions []*sdkmcp.ServerSession
	reads := map[string]*atomic.Int32{"identity-a": {}, "identity-b": {}}
	bytesOf := func(identity string) []byte { return []byte("bytes only " + identity + " owns") }
	policy := bridgePolicy{identityScoped: true}

	connect := func(_ context.Context, hctx context.Context, options mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		owner := identityctx.IdentityID(hctx)
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
		server.AddTool(mustTool("fetch", "", nil, nil), func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.ResourceLink{URI: "fixture://files/doc", Name: "doc.pdf"}}}, nil
		})
		serveFiles(server, "application/pdf", bytesOf(owner), reads[owner])
		clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(hctx, serverTransport, nil)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		serverSessions = append(serverSessions, serverSession)
		mu.Unlock()
		options.Sending = sendingMiddleware(policy, owner)
		return connectClient(hctx, clientTransport, options)
	}

	sink := &recordingSink{}
	reg := tools.NewRegistry()
	handshakeCtx := identityctx.WithIdentityID(t.Context(), "identity-a")
	closer, names, host, err := openIdentityScopedHTTPMount(t.Context(), handshakeCtx, reg, "fixture", policy, MountOptions{Files: sink}, connect)
	if err != nil {
		t.Fatalf("openIdentityScopedHTTPMount: %v", err)
	}
	t.Cleanup(func() {
		_ = closer()
		mu.Lock()
		sessions := append([]*sdkmcp.ServerSession(nil), serverSessions...)
		mu.Unlock()
		for _, session := range sessions {
			_ = session.Close()
		}
	})
	if host.files != sink {
		t.Fatalf("mounted host files = %v, want the sink the mount was given", host.files)
	}
	tool, ok := reg.Get(names[0])
	if !ok {
		t.Fatalf("%s is not registered", names[0])
	}

	for _, identity := range []string{"identity-a", "identity-b"} {
		ctx := tools.WithToolCallContext(identityctx.WithIdentityID(t.Context(), identity), "sess", "tc-"+identity, t.TempDir(), 2048)
		if _, err := tool.Execute(ctx, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("Execute as %s: %v", identity, err)
		}
		if sink.server != "fixture" || len(sink.parts) != 1 || string(sink.parts[0].Data) != string(bytesOf(identity)) {
			t.Fatalf("as %s the sink got server %q, parts %+v; want fixture and one part of %q", identity, sink.server, sink.parts, bytesOf(identity))
		}
	}
	for identity, count := range reads {
		if count.Load() != 1 {
			t.Fatalf("%s's session served %d reads, want exactly the one its own call made", identity, count.Load())
		}
	}
}
