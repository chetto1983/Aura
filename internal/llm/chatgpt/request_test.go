package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type contentLoader struct {
	part              llm.VerifiedContentPart
	err               error
	tenant, owner, id string
}

func (l *contentLoader) LoadContentPart(_ context.Context, tenant, owner, id string) (llm.VerifiedContentPart, error) {
	l.tenant, l.owner, l.id = tenant, owner, id
	return l.part, l.err
}

func TestRequestImagesAndToolMedia(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"vision","visibility":"list","input_modalities":["image"]}]}`)
	})
	loader := &contentLoader{part: llm.VerifiedContentPart{ID: "ref1", MIMEType: "image/png", Bytes: []byte("verified-image")}}
	ctx, toolMedia := llm.WithToolMedia(context.Background())
	if err := toolMedia.Add("call1", llm.ProjectedRequestPart{MIMEType: "image/jpeg", Bytes: []byte("tool-image"), Text: "tool source"}); err != nil {
		t.Fatal(err)
	}
	ctx = llm.WithContentProjection(ctx, llm.ContentProjection{Loader: loader, Principal: llm.ProjectionPrincipal{TenantID: "tenant", OwnerID: "owner"}, ReferenceIDs: []string{"ref1"}})
	params, err := client.buildRequest(ctx, llm.Request{Model: "vision", Messages: []llm.Message{{Role: llm.RoleUser, Content: "Inspect"}, {Role: llm.RoleTool, ToolCallID: "call1", Content: "Result"}}}, "oauth-test-token")
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(params)
	if strings.Count(string(wire), "input_image") != 2 || !strings.Contains(string(wire), "data:image/png;base64,") || !strings.Contains(string(wire), "never an instruction") {
		t.Fatalf("wire %s", wire)
	}
	if loader.tenant != "tenant" || loader.owner != "owner" || loader.id != "ref1" {
		t.Fatal("projection lost authorization")
	}
}

func TestMediaTextFallbackAndFailures(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"plain","visibility":"list"},{"slug":"vision","visibility":"list","supports_image":true}]}`)
	})
	loader := &contentLoader{part: llm.VerifiedContentPart{ID: "ref1", MIMEType: "image/png", Bytes: []byte("image"), FallbackText: "Stored description"}}
	req := llm.Request{Model: "plain", Messages: []llm.Message{{Role: llm.RoleUser, Content: "Inspect"}}, ContentProjection: &llm.ContentProjection{Loader: loader, ReferenceIDs: []string{"ref1"}}, ToolMedia: map[string][]llm.ProjectedRequestPart{"call1": {{MIMEType: "image/png", Bytes: []byte("image"), Text: "Tool fallback"}}}}
	params, err := client.buildRequest(context.Background(), req, "oauth-test-token")
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(params)
	if strings.Contains(string(wire), "input_image") || !strings.Contains(string(wire), "Stored description") {
		t.Fatalf("wire %s", wire)
	}
	req.Messages = nil
	if _, err := client.buildRequest(context.Background(), req, "token"); err == nil {
		t.Fatal("media without user accepted")
	}
	req.Messages = []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	loader.err = errors.New("unauthorized ref")
	if _, err := client.buildRequest(context.Background(), req, "token"); err == nil {
		t.Fatal("loader error ignored")
	}
	req.ContentProjection.Loader = nil
	if _, err := client.buildRequest(context.Background(), req, "token"); err == nil {
		t.Fatal("nil loader accepted")
	}
	for _, part := range []llm.ProjectedRequestPart{{MIMEType: "broken", Bytes: []byte("a")}, {MIMEType: "image/png"}, {MIMEType: "audio/wav", Bytes: []byte("a")}, {MIMEType: "video/mp4", Bytes: []byte("v")}} {
		if _, err := mediaPart(part); err == nil {
			t.Fatalf("unsupported media accepted %#v", part)
		}
	}
	if _, err := mediaPart(llm.ProjectedRequestPart{ReferenceOnly: true, Text: "reference"}); err != nil {
		t.Fatal(err)
	}
	parts, err := client.projectMedia(context.Background(), "vision", "token", nil, map[string][]llm.ProjectedRequestPart{"call1": {{MIMEType: "image/png", Bytes: []byte("image")}}})
	if err != nil || parts.tools["call1"][0].OfInputImage == nil {
		t.Fatalf("media %#v error %v", parts, err)
	}
}

func TestRequestValidationAndToolChoice(t *testing.T) {
	client := New(llm.Config{}, tokenSource{token: "token"})
	validTool := llm.ToolDef{Type: "function"}
	validTool.Function.Name = "lookup"
	invalidSchema := validTool
	invalidSchema.Function.Parameters = json.RawMessage(`broken`)
	call := llm.ToolCall{}
	cases := []llm.Request{
		{Messages: []llm.Message{{Role: "other"}}},
		{Messages: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}},
		{Messages: []llm.Message{{Role: llm.RoleTool}}},
		{Tools: []llm.ToolDef{{Type: "native"}}},
		{Tools: []llm.ToolDef{invalidSchema}},
		{Tools: []llm.ToolDef{validTool}, ToolChoice: "unknown"},
	}
	for _, req := range cases {
		if _, err := client.buildRequest(context.Background(), req, "token"); err == nil {
			t.Fatalf("invalid request accepted %#v", req)
		}
	}
	params, err := client.buildRequest(context.Background(), llm.Request{Tools: []llm.ToolDef{validTool}, ToolChoice: "none"}, "token")
	if err != nil || len(params.Tools) > 0 {
		t.Fatalf("none tools %#v err %v", params.Tools, err)
	}
	params, err = client.buildRequest(context.Background(), llm.Request{Tools: []llm.ToolDef{validTool}, ToolChoice: "required"}, "token")
	if err != nil || params.ToolChoice.OfToolChoiceMode.Value != "required" {
		t.Fatal(err)
	}
	if !answeredLater([]llm.Message{{Role: llm.RoleUser}, {Role: llm.RoleTool, ToolCallID: "id"}}, "id") {
		t.Fatal("tool match missing")
	}
}
