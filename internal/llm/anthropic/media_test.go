package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type fakeLoader map[string]llm.VerifiedContentPart

func (l fakeLoader) LoadContentPart(_ context.Context, _, _, id string) (llm.VerifiedContentPart, error) {
	part, ok := l[id]
	if !ok {
		return part, errors.New("no such part")
	}
	return part, nil
}

// catalogJSON is a one-page /v1/models listing claude-sonnet-5-5 with the input
// capabilities the probe of 2026-11-27 recorded on every model.
func catalogJSON(image, pdf bool) string {
	return fmt.Sprintf(`{"data":[{"type":"model","id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5","max_input_tokens":1000000,"max_tokens":128000,`+
		`"capabilities":{"image_input":{"supported":%t},"pdf_input":{"supported":%t}}}],"has_more":false}`, image, pdf)
}

// mediaServer answers /v1/models with catalog and every message request with events,
// counting the catalogue reads and handing each message body to the test.
type mediaServer struct {
	client   *Client
	catalogs atomic.Int32
	auth     atomic.Value
	sent     chan captured
}

func newMediaServer(t *testing.T, catalog string, events []sseEvent) *mediaServer {
	t.Helper()
	server := &mediaServer{sent: make(chan captured, 8)}
	server.client = fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			server.catalogs.Add(1)
			server.auth.Store(r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, catalog)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body: %v", err)
		}
		server.sent <- captured{body: body, header: r.Header.Clone()}
		writeEvents(w, events...)
	})
	return server
}

func (s *mediaServer) send(t *testing.T, req llm.Request) map[string]any {
	t.Helper()
	drain(t, s.client, req)
	return (<-s.sent).body
}

var (
	pngBytes = []byte("\x89PNG\r\n\x1a\nimage")
	pdfBytes = []byte("%PDF-1.7 product sheet")
)

func base64Of(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func source(t *testing.T, block map[string]any) map[string]any {
	t.Helper()
	return object(t, block["source"])
}

func turnRequest(projection *llm.ContentProjection, tools map[string][]llm.ProjectedRequestPart) llm.Request {
	return llm.Request{Model: "claude-sonnet-5-5", LoadedHistoryMessages: 3, ContentProjection: projection, ToolMedia: tools,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "S"}, {Role: llm.RoleUser, Content: "old"}, {Role: llm.RoleAssistant, Content: "a"},
			{Role: llm.RoleUser, Content: "look at this"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("t1", "read_file", "{}")}},
			{Role: llm.RoleTool, ToolCallID: "t1", Content: "attached below"},
			{Role: llm.RoleUser, Content: "hint"},
		}}
}

func toolResultContent(t *testing.T, message map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, part := range blocksOf(t, message)[0]["content"].([]any) {
		out = append(out, object(t, part))
	}
	return out
}

func TestProjectedImageBecomesABlock(t *testing.T) {
	server := newMediaServer(t, catalogJSON(true, true), textReply("ok"))
	loader := fakeLoader{"ref1": {ID: "ref1", MIMEType: "image/png; charset=binary", FallbackText: "Photo of a receipt", Bytes: pngBytes}}
	req := turnRequest(&llm.ContentProjection{Loader: loader, ReferenceIDs: []string{"ref1"}},
		map[string][]llm.ProjectedRequestPart{"t1": {{Type: "media", MIMEType: "image/jpeg", Text: "/box/chart.jpg", Bytes: pngBytes}}})
	messages := messagesOf(t, server.send(t, req))
	current := blocksOf(t, messages[2])
	if !reflect.DeepEqual(typesOf(current), []string{"text", "image"}) {
		t.Fatalf("current user message %#v", current)
	}
	if got := source(t, current[1]); got["type"] != "base64" || got["media_type"] != "image/png" || got["data"] != base64Of(pngBytes) {
		t.Fatalf("upload image source %#v", got)
	}
	result := toolResultContent(t, messages[4])
	if len(result) != 2 || result[0]["text"] != "attached below" || result[1]["type"] != "image" || source(t, result[1])["media_type"] != "image/jpeg" {
		t.Fatalf("tool_result content %#v", result)
	}
}

func TestToolMediaPDFFollowsItsToolResult(t *testing.T) {
	server := newMediaServer(t, catalogJSON(true, true), textReply("ok"))
	loader := fakeLoader{"clip": {ID: "clip", MIMEType: "video/mp4", FallbackText: "A clip of the line", Bytes: []byte("mp4")}}
	req := turnRequest(&llm.ContentProjection{Loader: loader, ReferenceIDs: []string{"clip"}},
		map[string][]llm.ProjectedRequestPart{"t1": {{Type: "media", MIMEType: "application/pdf", Text: "/box/sheet.pdf", Bytes: pdfBytes}}})
	// An earlier turn's call answered under the same id keeps its result text alone.
	req.Messages = append(req.Messages[:1:1], append([]llm.Message{
		{Role: llm.RoleUser, Content: "before"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("t1", "read_file", "{}")}},
		{Role: llm.RoleTool, ToolCallID: "t1", Content: "old result"},
	}, req.Messages[1:]...)...)
	req.LoadedHistoryMessages = 6
	messages := messagesOf(t, server.send(t, req))
	if old := toolResultContent(t, messages[2]); len(old) != 1 || old[0]["text"] != "old result" {
		t.Fatalf("the earlier result carries this turn's media: %#v", old)
	}
	// The upload path projects images and video only: Claude reads no video, so it arrives
	// as its stored text.
	if current := blocksOf(t, messages[4]); !reflect.DeepEqual(typesOf(current), []string{"text", "text"}) || current[1]["text"] != "A clip of the line" {
		t.Fatalf("current user message %#v", current)
	}
	result := toolResultContent(t, messages[6])
	if len(result) != 2 || result[1]["type"] != "document" {
		t.Fatalf("tool_result content %#v", result)
	}
	if got := source(t, result[1]); got["type"] != "base64" || got["media_type"] != "application/pdf" || got["data"] != base64Of(pdfBytes) {
		t.Fatalf("document source %#v", got)
	}
}

// The web UI and Telegram put the turn's uploads, and the agent its tool media, on the
// context rather than on the request.
func TestContextMediaBecomeBlocks(t *testing.T) {
	server := newMediaServer(t, catalogJSON(true, true), textReply("ok"))
	loader := fakeLoader{"ref1": {ID: "ref1", MIMEType: "image/webp", Bytes: pngBytes}}
	ctx := llm.WithContentProjection(context.Background(), llm.ContentProjection{Loader: loader, ReferenceIDs: []string{"ref1"}})
	ctx, tools := llm.WithToolMedia(ctx)
	if err := tools.Add("t1", llm.ProjectedRequestPart{Type: "media", MIMEType: "application/pdf", Text: "/box/sheet.pdf", Bytes: pdfBytes}); err != nil {
		t.Fatal(err)
	}
	stream, err := server.client.Stream(ctx, turnRequest(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	messages := messagesOf(t, (<-server.sent).body)
	if current := blocksOf(t, messages[2]); !reflect.DeepEqual(typesOf(current), []string{"text", "image"}) {
		t.Fatalf("current user message %#v", current)
	}
	if result := toolResultContent(t, messages[4]); len(result) != 2 || result[1]["type"] != "document" {
		t.Fatalf("tool_result content %#v", result)
	}
}

func TestSVGStaysReferenceOnly(t *testing.T) {
	for name, catalog := range map[string]string{"reads images": catalogJSON(true, true), "reads no images": catalogJSON(false, false)} {
		t.Run(name, func(t *testing.T) {
			server := newMediaServer(t, catalog, textReply("ok"))
			loader := fakeLoader{"logo": {ID: "logo", MIMEType: "image/svg+xml", FallbackText: "The company logo", Bytes: []byte("<svg/>")}}
			req := turnRequest(&llm.ContentProjection{Loader: loader, ReferenceIDs: []string{"logo"}},
				map[string][]llm.ProjectedRequestPart{"t1": {
					{Type: "media", MIMEType: "image/bmp", Text: "/box/scan.bmp", Bytes: []byte("BM")},
					{Type: "media", MIMEType: "image/tiff", Bytes: []byte("II*")},
				}})
			body := server.send(t, req)
			messages := messagesOf(t, body)
			if current := blocksOf(t, messages[2]); current[len(current)-1]["text"] != "The company logo" {
				t.Fatalf("current user message %#v", current)
			}
			result := toolResultContent(t, messages[4])
			want := []string{"attached below", `Not shown: the current model does not read image/bmp input: "/box/scan.bmp"`, "Not shown: the current model does not read image/tiff input"}
			if len(result) != len(want) {
				t.Fatalf("tool_result content %#v", result)
			}
			for i, text := range want {
				if result[i]["text"] != text {
					t.Fatalf("tool_result part %d = %#v, want %q", i, result[i], text)
				}
			}
			raw, _ := json.Marshal(body)
			if strings.Contains(string(raw), `"image"`) || strings.Contains(string(raw), "base64") {
				t.Fatalf("a media block reached the wire: %s", raw)
			}
		})
	}
}

func TestCapabilitiesAreFetchedOnlyWithMedia(t *testing.T) {
	server := newMediaServer(t, catalogJSON(true, true), textReply("ok"))
	server.send(t, userRequest("no media"))
	if got := server.catalogs.Load(); got != 0 {
		t.Fatalf("a request without media read the catalogue %d times", got)
	}
	withMedia := turnRequest(nil, map[string][]llm.ProjectedRequestPart{"t1": {{Type: "media", MIMEType: "image/png", Bytes: pngBytes}}})
	for range 2 {
		server.send(t, withMedia)
	}
	if got := server.catalogs.Load(); got != 1 {
		t.Fatalf("two requests with media on one model read the catalogue %d times, want 1", got)
	}
	if got := server.auth.Load(); got != "Bearer "+houseKey {
		t.Fatalf("catalogue read with %q", got)
	}
	// A model the account does not list reads text only.
	withMedia.Model = "claude-unlisted-9-9"
	if result := toolResultContent(t, messagesOf(t, server.send(t, withMedia))[4]); len(result) != 2 || result[1]["type"] != "text" {
		t.Fatalf("an unlisted model got %#v", result)
	}
	if got := server.catalogs.Load(); got != 2 {
		t.Fatalf("a second model read the catalogue %d times in all, want 2", got)
	}
}

func TestMediaFailuresStopTheRequest(t *testing.T) {
	image := llm.VerifiedContentPart{ID: "x", MIMEType: "image/png", FallbackText: "img", Bytes: pngBytes}
	for name, req := range map[string]llm.Request{
		"no loader":       {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, ContentProjection: &llm.ContentProjection{ReferenceIDs: []string{"x"}}},
		"load fails":      {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, ContentProjection: &llm.ContentProjection{Loader: fakeLoader{}, ReferenceIDs: []string{"x"}}},
		"empty image":     {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, ContentProjection: &llm.ContentProjection{Loader: fakeLoader{"x": {ID: "x", MIMEType: "image/png"}}, ReferenceIDs: []string{"x"}}},
		"no user message": {Messages: []llm.Message{{Role: llm.RoleSystem, Content: "S"}}, ContentProjection: &llm.ContentProjection{Loader: fakeLoader{"x": image}, ReferenceIDs: []string{"x"}}},
		"bad role":        {Messages: []llm.Message{{Role: "developer", Content: "hi"}}},
		"tool without id": {Messages: []llm.Message{{Role: llm.RoleTool, Content: "hi"}}},
		"call without id": {Messages: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("", "lookup", "{}")}}}},
		"unnamed tool":    {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, Tools: []llm.ToolDef{tool("", "", "")}},
	} {
		t.Run(name, func(t *testing.T) {
			server := newMediaServer(t, catalogJSON(true, true), textReply("ok"))
			if _, err := server.client.Stream(context.Background(), req); err == nil {
				t.Fatal("request accepted")
			}
			if len(server.sent) != 0 {
				t.Fatal("a refused request reached the API")
			}
		})
	}
}

func TestCatalogueFailureStopsAMediaRequest(t *testing.T) {
	server := newMediaServer(t, `not json`, textReply("ok"))
	req := turnRequest(nil, map[string][]llm.ProjectedRequestPart{"t1": {{Type: "media", MIMEType: "image/png", Bytes: pngBytes}}})
	if _, err := server.client.Stream(context.Background(), req); !errors.Is(err, llm.ErrModelCatalogUnavailable) {
		t.Fatalf("Stream = %v, want the catalogue failure", err)
	}
	if len(server.sent) != 0 {
		t.Fatal("the request was sent without the model's capabilities")
	}
}

func TestMediaBlockRefusesWhatClaudeCannotRead(t *testing.T) {
	for name, part := range map[string]llm.ProjectedRequestPart{
		"malformed type": {MIMEType: "image/", Bytes: pngBytes},
		"no bytes":       {MIMEType: "image/png"},
		"bmp":            {MIMEType: "image/bmp", Bytes: pngBytes},
	} {
		if image, document, err := mediaBlock(part); err == nil || image != nil || document != nil {
			t.Errorf("%s: mediaBlock = %v, %v, %v; want an error", name, image, document, err)
		}
	}
}
