package openai_compat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type staticContentCaps struct {
	caps     llm.ProviderContentCapabilities
	detected bool
}

func (s staticContentCaps) ContentCapabilities(context.Context) (llm.ProviderContentCapabilities, bool) {
	return s.caps, s.detected
}

type staticProjectionLoader struct {
	parts map[string]llm.VerifiedContentPart
}

func (l staticProjectionLoader) LoadContentPart(_ context.Context, _, _, id string) (llm.VerifiedContentPart, error) {
	return l.parts[id], nil
}

func TestRequestBodyProjectsSupportedGarageMediaIntoNewestUserMessage(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = readRequestBody(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	imageBytes := []byte("png-bytes")
	audioBytes := []byte("wav-bytes")
	c := New(testConfig(srv.URL))
	c.contentCaps = staticContentCaps{
		caps:     llm.ProviderContentCapabilities{Modalities: map[string]bool{"image": true, "audio": true}},
		detected: true,
	}
	ch, err := c.Stream(context.Background(), llm.Request{
		Model: "multimodal",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "old user"},
			{Role: llm.RoleAssistant, Content: "old answer"},
			{Role: llm.RoleUser, Content: "describe attachments"},
		},
		ContentProjection: &llm.ContentProjection{
			Loader: staticProjectionLoader{parts: map[string]llm.VerifiedContentPart{
				"image": {ID: "image", MIMEType: "image/png", Bytes: imageBytes},
				"audio": {ID: "audio", MIMEType: "audio/wav", Bytes: audioBytes},
			}},
			Principal:    llm.ProjectionPrincipal{OwnerID: "owner"},
			ReferenceIDs: []string{"image", "audio"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_ = drain(ch)

	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("decode request: %v\n%s", err, gotBody)
	}
	if _, ok := body.Messages[0].Content.(string); !ok {
		t.Fatalf("older user message became multimodal: %#v", body.Messages[0].Content)
	}
	parts, ok := body.Messages[2].Content.([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("newest user content = %#v, want text+image+audio", body.Messages[2].Content)
	}
	imagePart := parts[1].(map[string]any)
	wantImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)
	if imagePart["type"] != "image_url" || imagePart["image_url"].(map[string]any)["url"] != wantImage {
		t.Fatalf("image part = %#v", imagePart)
	}
	audioPart := parts[2].(map[string]any)
	inputAudio := audioPart["input_audio"].(map[string]any)
	if audioPart["type"] != "input_audio" || inputAudio["data"] != base64.StdEncoding.EncodeToString(audioBytes) || inputAudio["format"] != "wav" {
		t.Fatalf("audio part = %#v", audioPart)
	}
}

func TestRequestBodyKeepsTextFallbackWhenModelIsTextOnly(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = readRequestBody(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	c.contentCaps = staticContentCaps{
		caps:     llm.ProviderContentCapabilities{Modalities: map[string]bool{"text": true}},
		detected: true,
	}
	const fallback = "<attachments>stored transcript</attachments>"
	ch, err := c.Stream(context.Background(), llm.Request{
		Model:    "text-only",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: fallback}},
		ContentProjection: &llm.ContentProjection{
			Loader: staticProjectionLoader{parts: map[string]llm.VerifiedContentPart{
				"audio": {ID: "audio", MIMEType: "audio/wav", Bytes: []byte("secret-audio")},
			}},
			Principal:    llm.ProjectionPrincipal{OwnerID: "owner"},
			ReferenceIDs: []string{"audio"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_ = drain(ch)
	if string(gotBody) == "" || bytes.Contains(gotBody, []byte("secret-audio")) {
		t.Fatalf("text-only wire leaked native bytes: %s", gotBody)
	}
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil || body.Messages[0].Content != fallback {
		t.Fatalf("fallback message changed: body=%s err=%v", gotBody, err)
	}
}

func readRequestBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return append([]byte(nil), raw...)
}
