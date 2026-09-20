# Video attachments to the model — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A video attachment reaches the model as native video when, and only when, the selected LLM declares video input — on OpenRouter and llama.cpp, with the wire shape each one reads — and falls back to its text reference everywhere else.

**Architecture:** One admission rule (`assets.NativeMedia`) shared by the loader, the AG-UI gateway and Telegram decides that images and video may travel as bytes; the existing per-backend `ContentCapabilitySource` decides per request whether they do; `openai_compat.nativeContentPart` writes the backend's own video part through openai-go's `param.Override`.

**Tech Stack:** Go 1.27, `github.com/openai/openai-go/v3` v3.61.0, llama.cpp `server-b10951`, OpenRouter.

**Spec:** `docs/superpowers/specs/2026-09-19-studio-media-editing-design.md` (Part 2).

## Global Constraints

- **Measure first** (CLAUDE.md "PRD-first"): Task 1 runs before any code; a branch the measurement does not support is dropped, not guessed.
- Commit directly on `master`; stage by explicit path; short imperative subject, body says why, trailer `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- After every Go edit: `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/` for each package touched (WSL has native `-race`; on Windows use WSL).
- No file over 600 lines; no new env var; no model-name guessing — capability comes only from the `ContentCapabilitySource`.
- Audio never becomes native media (speech reaches the model as its transcript).
- Credentials: ask the operator for an API key in one line; never read one from a container, `.env` or a log.
- Mutation testing runs in CI only.
- WSL scripts run from a file (not `bash -s`), with `MSYS_NO_PATHCONV=1` when invoked from Git Bash.

## Facts already measured (2026-09-19, spike 105 session)

- `ghcr.io/ggml-org/llama.cpp:server-b10951` (published 2026-09-14, after PR #24269 of 2026-06-08) ships `/usr/bin/ffmpeg` 6.1.1; `libllama-server-impl.so` contains the content types `image_url input_audio input_video`, the prefixes `data:image/ data:video/ data:audio/`, the types `video/mp4 video/mpeg video/webm`, and the error "video input is not supported - hint: … mmproj".
- The `/props` key that reports video is **not** yet known (needs a video model loaded).
- `llamaCppModalities` passes every `/props` modality key through `clampInputModalities` and renames only `vision` → `image`, so a key literally named `video` already maps to the `video` modality.
- `ollamaModalities` maps only the `vision` capability.

## File structure

```
.planning/spikes/106-video-to-model/README.md   the measurement (Task 1)
internal/assets/turn_media_loader.go            NativeMedia + loader admits video (Task 2)
internal/assets/turn_media_loader_test.go       (Task 2, create)
internal/agui/server_context.go                 gateway uses NativeMedia (Task 2)
internal/agui/server_assets_run_test.go         video armed, audio still refused (Task 2)
internal/channels/telegram/bot_dispatch_media.go                Telegram uses NativeMedia (Task 2)
internal/channels/telegram/bot_dispatch_media_projection_test.go (Task 2)
internal/llm/openai_compat/request.go           video part per backend (Task 3)
internal/llm/openai_compat/request_video_test.go (Task 3, create)
internal/llm/openai_compat/wire_message_test.go new toSDKMessages signature (Task 3)
internal/llm/model_content_caps.go              llama.cpp key mapping if needed (Task 4)
docs/superpowers/verification/2026-09-19-video-to-model.md (Task 5)
```

---

### Task 1: Measure video input on llama.cpp and OpenRouter (spike 106)

**Files:**
- Create: `.planning/spikes/106-video-to-model/README.md`, `.planning/spikes/106-video-to-model/probe.sh`, `.planning/spikes/106-video-to-model/.gitignore` (`out/`)
- Modify: `.planning/spikes/MANIFEST.md` (row 106, idea `studio-media-editing`)

- [ ] **Step 1: Pick a video model that exists**

```bash
curl -s https://huggingface.co/api/models/ggml-org/Qwen3-VL-2B-Instruct-GGUF | head -c 300; echo
```
Expected: JSON with `"id":"ggml-org/Qwen3-VL-2B-Instruct-GGUF"`. If 404, try `ggml-org/gemma-4-E4B-it-GGUF` (the two models PR #24269 was tested with) and use whichever answers.

- [ ] **Step 2: Start a throwaway llama-server with it**

```bash
docker run -d --name aura-video-probe -p 18080:8080 -v aura-video-probe-cache:/root/.cache \
  ghcr.io/ggml-org/llama.cpp:server-b10951 -hf ggml-org/Qwen3-VL-2B-Instruct-GGUF --host 0.0.0.0 --port 8080 -c 16384
until curl -sf http://localhost:18080/health >/dev/null; do sleep 5; done
curl -s http://localhost:18080/props | python3 -c 'import json,sys; p=json.load(sys.stdin); print(json.dumps(p.get("modalities"), indent=1))'
```
Record the `modalities` object verbatim. The download is ~2–3 GB into the named volume.

- [ ] **Step 3: Send one real video**

Use a clip with burned-in timestamps (`.planning/spikes/105-studio-media-editing/media/clip-h264-silent.mp4`; regenerate with that spike's `make-media.sh` if absent). Write `probe.sh`:
```bash
#!/usr/bin/env bash
# One input_video request to the probe server; prints the answer and the timing.
set -euo pipefail
CLIP="${1:?clip path}"
B64=$(base64 -w0 "$CLIP")
jq -n --arg data "$B64" '{
  model: "probe",
  max_tokens: 200,
  messages: [{ role: "user", content: [
    { type: "text", text: "Read the timestamp written in the top-left corner of the first and of the last frame." },
    { type: "input_video", input_video: { data: $data } }
  ]}]
}' > out/request.json
time curl -s http://localhost:18080/v1/chat/completions -H 'Content-Type: application/json' -d @out/request.json | tee out/response.json | jq -r '.choices[0].message.content'
```
Run `mkdir -p out && bash probe.sh <clip>`. Expected: an answer that names timestamps near 00:00 and 00:07; record it, the time, and `docker logs aura-video-probe | tail -40` (frames decoded). If the server answers "video input is not supported", record that and stop the llama.cpp branch here.

- [ ] **Step 4: Send the same video to one OpenRouter video model**

Pick a model whose `architecture.input_modalities` contains `video`:
```bash
curl -s 'https://openrouter.ai/api/v1/models' | jq -r '.data[] | select(.architecture.input_modalities | index("video")) | .id' | head -20
```
Ask the operator, in one line, for an OpenRouter key for this probe. Then send `{"type":"video_url","video_url":{"url":"data:video/mp4;base64,…"}}` with the same prompt to one listed model (prefer a cheap Gemini Flash), and record the answer, the reported cost and the model id.

- [ ] **Step 5: Tear down and write the README**

```bash
docker rm -f aura-video-probe
```
`.planning/spikes/106-video-to-model/README.md` with the frontmatter used by spike 105 (`spike: 106`, `idea: studio-media-editing`, `type: standard`, `verdict: VALIDATED|PARTIAL|INVALIDATED`), sections What This Validates / How to Run / Investigation Trail / Results. Results must state: the exact `/props` key, whether `input_video` worked on b10951, the OpenRouter part shape and model used, and **the decision**: which backend branches Task 3 implements. Add the MANIFEST row.

- [ ] **Step 6: Commit**

```bash
git add .planning/spikes/106-video-to-model/README.md .planning/spikes/106-video-to-model/probe.sh .planning/spikes/106-video-to-model/.gitignore .planning/spikes/MANIFEST.md
git commit -m "docs(spike-106): measure video input on llama.cpp and OpenRouter" -m "The wire shape and capability key each backend really uses, measured before any code is written." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: One admission rule for native media

**Files:**
- Modify: `internal/assets/turn_media_loader.go`, `internal/agui/server_context.go:66-76`, `internal/channels/telegram/bot_dispatch_media.go`
- Create: `internal/assets/turn_media_loader_test.go`
- Modify tests: `internal/agui/server_assets_run_test.go`, `internal/channels/telegram/bot_dispatch_media_projection_test.go`

**Interfaces:**
- Produces: `func NativeMedia(modality Modality) bool` in package `assets` (true for `ModalityImage`, `ModalityVideo`).

- [x] **Step 1: Write the failing tests**

`internal/assets/turn_media_loader_test.go`:
```go
package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

type openerStub struct {
	asset Asset
	body  []byte
}

func (o openerStub) OpenForIdentity(context.Context, string, string) (io.ReadCloser, Asset, error) {
	return io.NopCloser(bytes.NewReader(o.body)), o.asset, nil
}

func TestNativeMedia(t *testing.T) {
	t.Parallel()
	for modality, want := range map[Modality]bool{
		ModalityImage: true, ModalityVideo: true,
		ModalityAudio: false, ModalityDocument: false, ModalityUnknown: false,
	} {
		if got := NativeMedia(modality); got != want {
			t.Errorf("NativeMedia(%q) = %v, want %v", modality, got, want)
		}
	}
}

func TestTurnMediaLoaderLoadsVideo(t *testing.T) {
	t.Parallel()
	body := []byte("mp4-bytes")
	digest := sha256.Sum256(body)
	loader := TurnMediaLoader{
		Opener: openerStub{body: body, asset: Asset{
			ID: "v1", ThreadID: "t1", MIMEType: "video/mp4", Modality: ModalityVideo,
			SizeBytes: int64(len(body)), ContentHash: hex.EncodeToString(digest[:]),
		}},
		ThreadID: "t1",
		Allowed:  map[string]bool{"v1": true},
	}
	part, err := loader.LoadContentPart(context.Background(), "", "owner", "v1")
	if err != nil {
		t.Fatalf("LoadContentPart: %v", err)
	}
	if part.MIMEType != "video/mp4" || !bytes.Equal(part.Bytes, body) {
		t.Fatalf("part = %+v", part)
	}
}

func TestTurnMediaLoaderStillRefusesAudio(t *testing.T) {
	t.Parallel()
	body := []byte("ogg")
	digest := sha256.Sum256(body)
	loader := TurnMediaLoader{
		Opener: openerStub{body: body, asset: Asset{
			ID: "a1", ThreadID: "t1", MIMEType: "audio/ogg", Modality: ModalityAudio,
			SizeBytes: int64(len(body)), ContentHash: hex.EncodeToString(digest[:]),
		}},
		ThreadID: "t1",
		Allowed:  map[string]bool{"a1": true},
	}
	_, err := loader.LoadContentPart(context.Background(), "", "owner", "a1")
	if err == nil || !strings.Contains(err.Error(), "not native media") {
		t.Fatalf("err = %v, want the modality refusal", err)
	}
}
```
If `OpenForIdentity`'s signature differs from `(ctx, id, identityID string) (io.ReadCloser, Asset, error)`, copy it from `MediaOpener` in `turn_media_loader.go`.

In `internal/channels/telegram/bot_dispatch_media_projection_test.go`, rename the test to `TestWithTurnMediaProjectionArmsImagesAndVideo`, add
`video := assetspkg.Asset{ID: "a-vid", IdentityID: "id-1", Modality: assetspkg.ModalityVideo}`,
pass `[]assetspkg.Asset{image, video, doc, voice}`, and expect `ReferenceIDs == ["a-img", "a-vid"]` and `loader.Allowed["a-vid"]`. Update its doc comment: native media is images and video; a voice note is words.

In `internal/agui/server_assets_run_test.go`, next to the image-projection test that reads `llm.ContentProjectionFromContext(run.turnCtx)` (line ~85), add a case whose attachment is `Modality: assets.ModalityVideo, MIMEType: "video/mp4"` and assert it is in `projection.ReferenceIDs`. Copy the image test's setup verbatim and change only the asset.

- [x] **Step 2: Run them to verify they fail**

Run (WSL): `go test ./internal/assets/ ./internal/agui/ ./internal/channels/telegram/ -run 'NativeMedia|TurnMediaLoader|TurnMediaProjection|Projection'`
Expected: FAIL — `undefined: NativeMedia`, then the video cases.

- [x] **Step 3: Implement**

In `internal/assets/turn_media_loader.go`, replace the "IMAGE is the only native modality…" paragraph of the `TurnMediaLoader` comment with:
```go
// Images and video are native media: their bytes may reach the model, and the provider
// client still decides per request, from the active model's declared input modalities,
// whether they do (llm.ContentCapabilitySource). Audio is not: a voice turn used to reach
// the model as an input_audio ATTACHMENT while the transcript the STT sidecar had already
// produced sat unused, so speech now reaches the model as TEXT on every channel — the
// transcript AudioProcessor writes into Asset.Summary, rendered by BuildAttachmentBlock.
```
add, above the type:
```go
// NativeMedia is the one admission rule for bytes that may reach the model: the loader,
// the AG-UI gateway and the Telegram channel all ask it, so no channel can drift.
func NativeMedia(modality Modality) bool {
	return modality == ModalityImage || modality == ModalityVideo
}
```
and in `LoadContentPart` replace `if asset.Modality != ModalityImage {` with `if !NativeMedia(asset.Modality) {`.

In `internal/agui/server_context.go` replace the comment "Images only. An audio attachment reaches the model as its TRANSCRIPT…" with
`// Native media only (assets.NativeMedia): an audio attachment reaches the model as its`
`// TRANSCRIPT (the STT summary BuildTurnContext renders below), never as bytes.`
and `if attachment.Modality != assets.ModalityImage {` with `if !assets.NativeMedia(attachment.Modality) {`.

In `internal/channels/telegram/bot_dispatch_media.go` update the package comment's last sentences to say images and video are projected and a voice note is not, rename nothing else, and replace the modality check with `if !assets.NativeMedia(attachment.Modality) {`.

- [x] **Step 4: Run the checks**

Run (WSL):
```bash
go vet ./internal/assets/ ./internal/agui/ ./internal/channels/telegram/
go build ./...
go test -race ./internal/assets/ ./internal/agui/ ./internal/channels/telegram/
```
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/assets/turn_media_loader.go internal/assets/turn_media_loader_test.go internal/agui/server_context.go internal/agui/server_assets_run_test.go internal/channels/telegram/bot_dispatch_media.go internal/channels/telegram/bot_dispatch_media_projection_test.go
git commit -m "feat(assets): let video attachments travel as native media" -m "One rule, shared by the loader, the web gateway and Telegram, admits images and video; whether a turn's model receives them is still decided by its declared input modalities, and audio stays text." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Write each backend's video part

Implement only the branches Task 1 kept. The code below assumes both were kept; drop the llama.cpp `case` and its test row if it was not.

**Files:**
- Modify: `internal/llm/openai_compat/request.go`, `internal/llm/openai_compat/wire_message_test.go`
- Create: `internal/llm/openai_compat/request_video_test.go`

**Interfaces:**
- Consumes: `llm.ReasoningTarget(provider, baseURL) llm.ReasoningTargetKind` and its constants.
- Produces: `toSDKMessages(messages []llm.Message, native []llm.ProjectedRequestPart, target llm.ReasoningTargetKind)`; `nativeContentPart(media llm.ProjectedRequestPart, target llm.ReasoningTargetKind)`.

- [x] **Step 1: Write the failing test**

`internal/llm/openai_compat/request_video_test.go`:
```go
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
```

- [x] **Step 2: Run it to verify it fails**

Run (WSL): `go test ./internal/llm/openai_compat/ -run 'VideoPart|CarriesTheVideo'`
Expected: FAIL — `too many arguments in call to nativeContentPart`.

- [x] **Step 3: Implement**

In `internal/llm/openai_compat/request.go`:
1. `buildSDKRequest`: `messages, err := toSDKMessages(req.Messages, native, llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL))`.
2. `toSDKMessages` gains the parameter `target llm.ReasoningTargetKind` and calls `nativeContentPart(media, target)`.
3. `nativeContentPart(media llm.ProjectedRequestPart, target llm.ReasoningTargetKind)` gains, before `default:`,
```go
	case "video":
		return videoContentPart(media.MIMEType, encoded, target)
```
4. Add below it:
```go
// videoContentPart writes the one video part each backend reads. openai-go has no video
// variant, so the part is serialised through param.Override (packages/param/encoder.go
// MarshalUnion writes the override when no variant is set). OpenRouter documents
// {"type":"video_url","video_url":{"url":<data URL>}}
// (openrouter.ai/docs/guides/overview/multimodal/videos); llama-server documents
// {"type":"input_video","input_video":{"data":<base64>}} (tools/server/README.md, PR #24269,
// measured in spike 106). No other backend has a video part — Ollama's OpenAI bridge
// included — and none of their capability sources reports video, so the default is a
// guard, not a path.
func videoContentPart(mimeType, encoded string, target llm.ReasoningTargetKind) (openai.ChatCompletionContentPartUnionParam, bool) {
	switch target {
	case llm.ReasoningTargetOpenRouter:
		return param.Override[openai.ChatCompletionContentPartUnionParam](map[string]any{
			"type":      "video_url",
			"video_url": map[string]any{"url": "data:" + mimeType + ";base64," + encoded},
		}), true
	case llm.ReasoningTargetLlamaCpp:
		return param.Override[openai.ChatCompletionContentPartUnionParam](map[string]any{
			"type":        "input_video",
			"input_video": map[string]any{"data": encoded},
		}), true
	default:
		return openai.ChatCompletionContentPartUnionParam{}, false
	}
}
```
If the compiler cannot infer `param.Override`'s second type parameter, write `param.Override[openai.ChatCompletionContentPartUnionParam, *openai.ChatCompletionContentPartUnionParam](…)`.

In `internal/llm/openai_compat/wire_message_test.go` pass `llm.ReasoningTargetNone` as the new third argument at both call sites.

- [x] **Step 4: Run the checks**

Run (WSL):
```bash
go vet ./internal/llm/...
go build ./...
go test -race ./internal/llm/openai_compat/ ./internal/llm/
```
Expected: PASS, including the existing image/audio multimodal tests.

- [x] **Step 5: Commit**

```bash
git add internal/llm/openai_compat/request.go internal/llm/openai_compat/request_video_test.go internal/llm/openai_compat/wire_message_test.go
git commit -m "feat(llm): send video in the shape each backend reads" -m "OpenRouter takes a video_url data URL and llama.cpp an input_video payload; openai-go has neither, so both go through param.Override, and a backend with no video part gets none." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Pin llama.cpp's video capability key

**Files:**
- Modify: `internal/llm/model_content_caps.go` (only if spike 106 measured a key other than `video`)
- Test: the existing llama.cpp modality probe test file (find it with `grep -rln "llamaCppModalities\|modalities" internal/llm/*_test.go`)

- [x] **Step 1: Write the failing (or pinning) test**

Add a case to the llama.cpp `/props` probe test: a fake server answering `{"modalities":{"vision":true,"<measured key>":true}}` must yield `ProviderContentCapabilities.Modalities` containing `image` and `video`, so `SupportsMIME("video/mp4")` is true. Model it on the existing vision case in that file.

- [x] **Step 2: Run it**

Run (WSL): `go test ./internal/llm/ -run <that test name>`
Expected: PASS already if the measured key is `video` (then this task only pins the behaviour); FAIL otherwise.

- [x] **Step 3: Map the key if it is not `video`**

In `llamaCppModalities`, beside `if name == "vision" { name = "image" }`, add the measured rename, with a comment citing spike 106.

- [x] **Step 4: Run the checks and commit**

```bash
go vet ./internal/llm/ && go test -race ./internal/llm/
git add internal/llm/model_content_caps.go <the test file>
git commit -m "test(llm): pin how llama.cpp reports video input" -m "The /props key measured in spike 106 now maps to the video modality, so a video model on llama.cpp receives clips and any other model does not." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: Push, gates, live verification

**Files:**
- Create: `docs/superpowers/verification/2026-09-19-video-to-model.md`

- [ ] **Step 1: Full local gate**

Run (WSL): `make quality` (vet, file-size, contracts, lint, deadcode, test-race, govulncheck, build).
Expected: `ok: quality gate passed`.

- [ ] **Step 2: Push**

```bash
git push origin master
```
Expected: lefthook pre-push green.

- [ ] **Step 3: Update the stack**

When the edge image carries the commit (in WSL, from a script file):
```bash
cd /opt/aura && docker compose pull aura && docker compose up -d aura && docker compose up -d --no-deps --force-recreate aura-migrate
docker inspect aura --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```

- [ ] **Step 4: Exercise it for real**

Note the current primary model (Settings → Model routing) so it can be restored. Then, on `https://localhost`:
1. Route to the OpenRouter video model used in spike 106; in a new chat attach an MP4 (the burned-in-timestamp clip) and ask what the first and last frames show. Expected: the answer reads the timestamps. Confirm in the daemon log that the request carried a `video_url` part (log the part types only, never bytes).
2. Route back to the text-only model (e.g. the previous primary). Same attachment. Expected: no video bytes sent; the answer is based on the text reference.
3. If Task 1 kept llama.cpp: route to a llama.cpp server running the video model and repeat 1 (expected part type `input_video`).
4. Send a video note on Telegram to the linked chat with the video model routed; expected as in 1.
5. Restore the operator's original primary model.

- [ ] **Step 5: Record and commit**

Write `docs/superpowers/verification/2026-09-19-video-to-model.md`: revision, model ids, per-case outcome, what it does not prove (per-model duration limits, very large clips). Then:
```bash
git add docs/superpowers/verification/2026-09-19-video-to-model.md
git commit -m "docs(llm): record the live video-to-model verification" -m "A video-capable model read the clip, a text-only one received only its reference, on the running stack." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git push origin master
```
