package openai_compat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

var (
	visionCaps   = staticContentCaps{caps: llm.ProviderContentCapabilities{Modalities: map[string]bool{"text": true, "image": true}}, detected: true}
	textOnlyCaps = staticContentCaps{caps: llm.ProviderContentCapabilities{Modalities: map[string]bool{"text": true}}, detected: true}
)

type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
}

// captureMessages streams request through a Client whose content caps are fixed, and returns
// the raw "messages" array the provider received.
func captureMessages(t *testing.T, caps llm.ContentCapabilitySource, request llm.Request) json.RawMessage {
	t.Helper()
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = readRequestBody(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := New(testConfig(srv.URL))
	c.contentCaps = caps
	ch, err := c.Stream(context.Background(), request)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_ = drain(ch)
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("decode request: %v\n%s", err, gotBody)
	}
	return body.Messages
}

func toolCall(id, name, args string) llm.ToolCall {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name, call.Function.Arguments = name, args
	return call
}

func decodeMessages(t *testing.T, raw json.RawMessage) []wireMessage {
	t.Helper()
	var messages []wireMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatalf("decode messages: %v\n%s", err, raw)
	}
	return messages
}

func contentParts(t *testing.T, message wireMessage) []map[string]any {
	t.Helper()
	var parts []map[string]any
	if err := json.Unmarshal(message.Content, &parts); err != nil {
		t.Fatalf("content is not a part array: %s", message.Content)
	}
	return parts
}

// toolRound is a round-2 conversation: the model called read_file and another tool in one
// batch, and a later round called a third tool.
func toolRound() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "cosa c'è nella foto?"},
		{Role: llm.RoleAssistant, Content: "Guardo.", ToolCalls: []llm.ToolCall{
			toolCall("c1", "read_file", `{"path":"/workspace/photo.png"}`),
			toolCall("c2", "current_time", `{}`),
		}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "/workspace/photo.png is an image (image/png, 2x2) and is attached below for you to look at."},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: "10:00"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c3", "current_time", `{}`)}},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: "10:01"},
		{Role: llm.RoleUser, Content: "<budget>steps left: 3</budget>"},
	}
}

var photoBytes = []byte("\x89PNG photo bytes")

func photoMedia() map[string][]llm.ProjectedRequestPart {
	return map[string][]llm.ProjectedRequestPart{"c1": {{Type: "media", MIMEType: "image/png", Text: "/workspace/photo.png", Bytes: photoBytes}}}
}

// The image must follow the LAST tool message of its block: a user message between c1 and
// c2 would separate c2's result from the assistant turn that called it, which providers
// reject.
func TestToolMediaFollowsTheLastToolMessageOfItsBlock(t *testing.T) {
	messages := decodeMessages(t, captureMessages(t, visionCaps, llm.Request{
		Model: "vision", Messages: toolRound(), ToolMedia: photoMedia(),
	}))
	roles := make([]string, len(messages))
	for i, m := range messages {
		roles[i] = m.Role
	}
	want := "system user assistant tool tool user assistant tool user"
	if got := strings.Join(roles, " "); got != want {
		t.Fatalf("roles = %s\nwant    %s", got, want)
	}
	if messages[4].ToolCallID != "c2" {
		t.Fatalf("message 4 = %+v, want c2's result right before the image", messages[4])
	}
	parts := contentParts(t, messages[5])
	if len(parts) != 2 || parts[0]["type"] != "text" || !strings.Contains(parts[0]["text"].(string), "/workspace/photo.png") {
		t.Fatalf("image message parts = %#v, want a text part naming the path, then the image", parts)
	}
	// The image rides in a user message, so its caption must disown the user's authority.
	if !strings.Contains(parts[0]["text"].(string), "not a message from the user") {
		t.Fatalf("caption = %q, want it framed as tool output", parts[0]["text"])
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(photoBytes)
	if parts[1]["type"] != "image_url" || parts[1]["image_url"].(map[string]any)["url"] != wantURL {
		t.Fatalf("image part = %#v", parts[1])
	}
}

// Caps that cannot be detected are the text-only floor, as they are for chat uploads.
func TestToolMediaBecomesATextNoteWhenTheModelCannotSeeImages(t *testing.T) {
	for name, caps := range map[string]llm.ContentCapabilitySource{
		"text-only model":   textOnlyCaps,
		"caps not detected": staticContentCaps{caps: visionCaps.caps, detected: false},
	} {
		t.Run(name, func(t *testing.T) {
			raw := captureMessages(t, caps, llm.Request{Model: "text", Messages: toolRound(), ToolMedia: photoMedia()})
			if strings.Contains(string(raw), "data:image") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(photoBytes)) {
				t.Fatalf("a model that cannot see images was sent the image: %s", raw)
			}
			messages := decodeMessages(t, raw)
			if len(messages) != 9 || messages[5].Role != llm.RoleUser {
				t.Fatalf("messages = %s, want a user note after the tool block", raw)
			}
			var note string
			if err := json.Unmarshal(messages[5].Content, &note); err != nil {
				t.Fatalf("note is not plain text: %s", messages[5].Content)
			}
			if !strings.Contains(note, "/workspace/photo.png") || !strings.Contains(note, "does not accept images") {
				t.Fatalf("note = %q, want the path and why it is not shown", note)
			}
		})
	}
}

// With no tool media the wire must be exactly what it was before tool media existed: the
// literal below is the request this conversation produced before the change.
func TestNoToolMediaLeavesTheWireUnchanged(t *testing.T) {
	const before = `[{"content":"sys","role":"system"},{"content":"cosa c'è nella foto?","role":"user"},{"content":"Guardo.","tool_calls":[{"id":"c1","function":{"arguments":"{\"path\":\"/workspace/photo.png\"}","name":"read_file"},"type":"function"},{"id":"c2","function":{"arguments":"{}","name":"current_time"},"type":"function"}],"role":"assistant"},{"content":"/workspace/photo.png is an image (image/png, 2x2) and is attached below for you to look at.","tool_call_id":"c1","role":"tool"},{"content":"10:00","tool_call_id":"c2","role":"tool"},{"tool_calls":[{"id":"c3","function":{"arguments":"{}","name":"current_time"},"type":"function"}],"role":"assistant"},{"content":"10:01","tool_call_id":"c3","role":"tool"},{"content":"<budget>steps left: 3</budget>","role":"user"}]`
	for name, media := range map[string]map[string][]llm.ProjectedRequestPart{
		"none":                      nil,
		"for a call no longer sent": {"gone": photoMedia()["c1"]},
	} {
		t.Run(name, func(t *testing.T) {
			got := captureMessages(t, visionCaps, llm.Request{Model: "vision", Messages: toolRound(), ToolMedia: media})
			if string(got) != before {
				t.Fatalf("wire changed:\ngot  %s\nwant %s", got, before)
			}
		})
	}
}

// A photo the user attached in the chat still belongs to the user's own message, not to the
// image message tool media adds after the tool block.
func TestChatUploadsStillAttachToTheRealLastUserMessage(t *testing.T) {
	upload := []byte("user upload")
	messages := decodeMessages(t, captureMessages(t, visionCaps, llm.Request{
		Model: "vision",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "sys"},
			{Role: llm.RoleUser, Content: "confronta con la foto"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "read_file", `{"path":"/workspace/photo.png"}`)}},
			{Role: llm.RoleTool, ToolCallID: "c1", Content: "attached"},
		},
		ContentProjection: &llm.ContentProjection{
			Loader:       staticProjectionLoader{parts: map[string]llm.VerifiedContentPart{"u": {ID: "u", MIMEType: "image/jpeg", Bytes: upload}}},
			Principal:    llm.ProjectionPrincipal{OwnerID: "owner"},
			ReferenceIDs: []string{"u"},
		},
		ToolMedia: photoMedia(),
	}))
	if len(messages) != 5 {
		t.Fatalf("messages = %+v, want system user assistant tool user", messages)
	}
	userParts := contentParts(t, messages[1])
	uploadURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(upload)
	if len(userParts) != 2 || userParts[1]["image_url"].(map[string]any)["url"] != uploadURL {
		t.Fatalf("the chat upload left the user's message: %#v", userParts)
	}
	toolParts := contentParts(t, messages[4])
	if len(toolParts) != 2 || strings.Contains(string(messages[4].Content), base64.StdEncoding.EncodeToString(upload)) {
		t.Fatalf("tool image message = %s, want only the tool's image", messages[4].Content)
	}
}
