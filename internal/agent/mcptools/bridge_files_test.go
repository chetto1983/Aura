package mcptools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
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
