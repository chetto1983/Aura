package openai_compat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestVideoPartPerBackend(t *testing.T) {
	t.Parallel()
	video := llm.ProjectedRequestPart{MIMEType: "video/mp4", Bytes: []byte("video")}
	encoded := base64.StdEncoding.EncodeToString(video.Bytes)
	cases := []struct {
		name   string
		target llm.ReasoningTargetKind
		want   map[string]any
	}{
		{"openrouter", llm.ReasoningTargetOpenRouter, map[string]any{
			"type": "video_url", "video_url": map[string]any{"url": "data:video/mp4;base64," + encoded},
		}},
		{"llamacpp", llm.ReasoningTargetLlamaCpp, map[string]any{
			"type": "input_video", "input_video": map[string]any{"data": encoded},
		}},
		{"ollama", llm.ReasoningTargetOllama, nil},
		{"unknown backend", llm.ReasoningTargetNone, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			part, ok := nativeContentPart(video, tc.target)
			if tc.want == nil {
				if ok {
					t.Fatalf("a backend without a video part emitted one")
				}
				return
			}
			if !ok {
				t.Fatal("no part emitted")
			}
			raw, err := json.Marshal(part)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal %s: %v", raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("part = %s, want %v", raw, tc.want)
			}
		})
	}
}

func TestLlamaCppRequestCarriesTheVideoOnlyWhenTheModelTakesIt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		modality  map[string]bool
		wantParts int
	}{
		{"video model", map[string]bool{"image": true, "video": true}, 2},
		{"image-only model", map[string]bool{"image": true}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody = readRequestBody(t, r)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer srv.Close()
			cfg := testConfig(srv.URL)
			cfg.Provider = "llamacpp"
			c := New(cfg)
			c.contentCaps = staticContentCaps{caps: llm.ProviderContentCapabilities{Modalities: tc.modality}, detected: true}
			ch, err := c.Stream(context.Background(), llm.Request{
				Model:    "qwen3-vl",
				Messages: []llm.Message{{Role: llm.RoleUser, Content: "what happens in the clip?"}},
				ContentProjection: &llm.ContentProjection{
					Loader: staticProjectionLoader{parts: map[string]llm.VerifiedContentPart{
						"v": {ID: "v", MIMEType: "video/mp4", Bytes: []byte("video")},
					}},
					Principal:    llm.ProjectionPrincipal{OwnerID: "owner"},
					ReferenceIDs: []string{"v"},
				},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			_ = drain(ch)
			var body struct {
				Messages []struct {
					Content any `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(gotBody, &body); err != nil {
				t.Fatalf("decode: %v\n%s", err, gotBody)
			}
			parts, isParts := body.Messages[0].Content.([]any)
			if tc.wantParts == 0 {
				if isParts {
					t.Fatalf("an image-only model received the video: %s", gotBody)
				}
				return
			}
			if !isParts || len(parts) != tc.wantParts || parts[1].(map[string]any)["type"] != "input_video" {
				t.Fatalf("content = %#v, want text + input_video", body.Messages[0].Content)
			}
		})
	}
}
