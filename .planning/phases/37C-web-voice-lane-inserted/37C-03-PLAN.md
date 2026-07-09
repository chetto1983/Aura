---
phase: 37C-web-voice-lane-inserted
plan: 03
type: execute
wave: 3
depends_on: ["37C-02"]
files_modified:
  - internal/agui/voice_api.go
  - internal/agui/voice_api_test.go
  - internal/agui/server.go
  - cmd/aura/serve_voice.go
  - cmd/aura/serve_voice_test.go
  - cmd/aura/serve_webui_voice.go
  - cmd/aura/serve.go
  - cmd/aura/serve_webui.go
autonomous: true
requirements: [WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04]
must_haves:
  truths:
    - "POST /api/tts returns Content-Type audio/mpeg with the synthesized bytes for an authed request; 401 without a cookie; 503 when s.tts==nil"
    - "POST /api/tts caps input at ttsMaxChars — the synthesizer receives the capped prefix and the response carries X-Aura-TTS-Truncated when the source text exceeds the cap; empty text → 400"
    - "POST /api/stt transcribes and creates NO asset/Garage object/DB row (a recording fakeAssetService is untouched); 401; 503 when s.stt==nil; audio/webm;codecs=opus maps to STT format webm"
    - "POST /api/stt returns 200 {\"text\":\"\"} cleanly when the transcriber yields an empty string (no error, no persist, no asset call) — the empty-transcript edge is handled, not a 5xx"
    - "GET /api/voice/capabilities returns 200 {tts,stt} reflecting client presence — {false,false} when unconfigured (never 503); 401 without a cookie"
    - "the web TTSClient is constructed with Format=mp3 (AudioFormat()==mp3) while the Telegram opus TTSClient is untouched; SetVoice (D-13) injects narrow ttsSynthesizer/sttTranscriber seams, not concrete clients"
  artifacts:
    - path: "internal/agui/voice_api.go"
      provides: "registerVoiceRoutes + handleTTS/handleSTT/handleVoiceCapabilities + ttsSynthesizer/sttTranscriber seams + SetVoice"
      contains: "registerVoiceRoutes"
      min_lines: 90
    - path: "internal/agui/voice_api_test.go"
      provides: "daemon-free three-handler suite (auth/identity/degrade/content-type/char-cap/no-persist/empty-transcript)"
      contains: "TestTTS"
    - path: "cmd/aura/serve_voice.go"
      provides: "composition-root: build mp3 web TTSClient + STTClient from config, call SetVoice"
      contains: "SetVoice"
    - path: "cmd/aura/serve_webui_voice.go"
      provides: "parent-mux route mounts (registerVoiceRoutes + route constants)"
      contains: "/api/tts"
  key_links:
    - from: "internal/agui/server.go"
      to: "internal/agui/voice_api.go"
      via: "s.registerVoiceRoutes(mux) in Mux()"
      pattern: "registerVoiceRoutes"
    - from: "cmd/aura/serve.go"
      to: "cmd/aura/serve_voice.go"
      via: "wireVoiceProviders(aguiServer, cfg) after NewServer"
      pattern: "wireVoice"
    - from: "cmd/aura/serve_webui.go"
      to: "cmd/aura/serve_webui_voice.go"
      via: "registerVoiceRoutes(mux, aguiHandler, auth) in newServeHandler"
      pattern: "registerVoiceRoutes"
  prohibitions:
    - "MUST NOT persist the STT audio — no assets.Asset, no Garage object, no DB row, no async poll (D-08); the handler must not call the asset service"
    - "MUST NOT thread a per-call Format through the shared TTSClient — build a DEDICATED web TTSClient with TTSConfig.Format=mp3 (RESEARCH Landmine #2); leave the Telegram opus client untouched"
    - "MUST NOT let GET /api/voice/capabilities return 503 when unconfigured — it returns 200 {false,false} (D-11)"
    - "MUST NOT 5xx on an empty transcript — an empty STT result is a clean 200 {\"text\":\"\"} (RESEARCH Nyquist edge: STT returns empty string, session ends clean)"
    - "MUST NOT depend on concrete *multimodal.TTSClient/*STTClient in the handler struct — depend on the narrow ttsSynthesizer/sttTranscriber interfaces so tests inject fakes with no network"
    - "MUST NOT let voice_api.go or serve_webui.go exceed 600 LOC — handlers live in the new voice_api.go, mounts in the new serve_webui_voice.go"
---

<objective>
Build the three thin identity-scoped voice handlers over narrow interface seams, the `SetVoice` setter, and the composition-root wiring that constructs a DEDICATED mp3 web `TTSClient` (Format="mp3") + an `STTClient` from `config.Config` (cloud-only, built only when the model is set — D-12) and injects them via `SetVoice` (D-13) — plus the parent-mux mounts. All handler logic is pure request/response and is proven by a daemon-free unit suite that satisfies the owned-surface ≥85% gate (the handlers land in the `db_integration neo4j_integration` coverage gate; no daemon is required or introduced).

Purpose: Deliver the backend contract (routes, request/response shapes, degrade behavior, mp3-vs-opus) the web consumers (37C-04/05) and the e2e (37C-06) build against.
Output: `internal/agui/voice_api.go` (+ test), `SetVoice` on `Server`, `cmd/aura/serve_voice.go` (+ test), `cmd/aura/serve_webui_voice.go`, and the two one-line wiring edits.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md
@internal/agui/assets_api.go
@internal/agui/audit_api.go
@internal/agui/settings_api.go
@internal/multimodal/tts.go
@internal/multimodal/stt.go
</context>

<artifacts_produced>
This plan produces:
- **`internal/agui/voice_api.go`**: `registerVoiceRoutes(mux)`, `handleTTS`, `handleSTT`, `handleVoiceCapabilities`; the `ttsSynthesizer` interface (`Synthesize(ctx, text) ([]byte, error)`, `AudioFormat() string`) + `sttTranscriber` interface (`Transcribe(ctx, audio []byte, fileName, format string) (string, error)`); `SetVoice(tts ttsSynthesizer, stt sttTranscriber, maxChars int)`; the `X-Aura-TTS-Truncated` response header contract.
- **`Server` fields** `tts ttsSynthesizer`, `stt sttTranscriber`, `ttsMaxChars int` (server.go) + `s.registerVoiceRoutes(mux)` in `Mux()`.
- **`cmd/aura/serve_voice.go`**: `wireVoiceProviders(server *agui.Server, cfg *config.Config)` — constructs the mp3 web `multimodal.TTSClient` (Format="mp3") + `STTClient` only when the cloud model is set, calls `SetVoice`.
- **`cmd/aura/serve_webui_voice.go`**: route constants (`ttsRoute`, `sttRoute`, `voiceCapabilitiesRoute`) + `registerVoiceRoutes(mux, aguiHandler, auth)` (POSTs behind `agentRunCapability`, capabilities RequireAuth-only).
- Wiring: `wireVoiceProviders(aguiServer, chat.cfg)` call in serve.go; `registerVoiceRoutes(mux, aguiHandler, auth)` call in serve_webui.go.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: voice_api.go — 3 handlers + ttsSynthesizer/sttTranscriber seams + SetVoice + daemon-free test suite</name>
  <files>internal/agui/voice_api.go, internal/agui/voice_api_test.go, internal/agui/server.go</files>
  <behavior>
    - TestTTS_Owner: withPrincipal(POST /api/tts {"text":"hi"}) → 200, Content-Type audio/mpeg, body == fake synth bytes; fakeTTS.gotText=="hi"; a recording fakeAssetService is UNTOUCHED.
    - TestTTS_Unauth: RequireAuth(s.Mux(), testDeps) + no cookie → 401; fakeTTS.calls==0.
    - TestTTS_Degraded: no SetVoice (nil tts) → 503.
    - TestTTS_CharCap: len(text) > ttsMaxChars → synth receives the ttsMaxChars-length prefix AND response has header X-Aura-TTS-Truncated: true; len(text)==ttsMaxChars → no truncated header; empty text → 400 (no synth call).
    - TestSTT_Owner: withPrincipal(POST /api/stt, multipart audio part typed audio/webm;codecs=opus) → 200 {"text":...}; fakeSTT.gotFormat=="webm"; recording fakeAssetService UNTOUCHED.
    - TestSTT_EmptyTranscript: fakeSTT returns "" (empty transcript) → 200 {"text":""} (NOT a 5xx), the recording fakeAssetService is UNTOUCHED (no insert, no persist) — the RESEARCH Nyquist edge "STT returns empty string, session ends clean".
    - TestSTT_Unauth / TestSTT_Degraded: 401 / 503.
    - TestVoiceCapabilities: table — both set→{true,true}; tts only→{true,false}; none→200 {false,false} (never 503); 401 without cookie.
    - All handler tests wrap in goleak.VerifyNone.
  </behavior>
  <read_first>
    - internal/agui/assets_api.go:13-59 — registerAssetRoutes + the nil-guard→503 / principalIdentityID→401 handler shape to mirror for the three voice handlers.
    - internal/agui/audit_api.go:52-96 — the SetSettingsStore-style setter + the SELF-scoped GET /api/me handler (the capabilities precedent: 200 with a computed payload, principalFrom fallback to localIdentityID in loopback).
    - internal/agui/settings_api.go:37-53 — the narrow-interface seam + setter pattern (declare ttsSynthesizer/sttTranscriber consumer-side).
    - internal/agui/server.go:98-134,170-192 — the Server struct (add tts/stt/ttsMaxChars fields) + Mux() where s.registerAssetRoutes(mux) is called (add s.registerVoiceRoutes(mux) beside it).
    - internal/multimodal/tts.go:42,60 + stt.go:52 — the exact Synthesize/AudioFormat/Transcribe signatures the seams must match so *multimodal.TTSClient/*STTClient satisfy them.
    - internal/agui/asset_download_test.go:25-129 — the happy-path (withPrincipal + s.Mux().ServeHTTP) and 401 (RequireAuth(s.Mux(), testDeps) + no cookie) harness to reuse; find the existing withPrincipal/testDeps helpers.
    - internal/assets/audio_processor.go — assets.AudioFormat (exported in 37C-02) for the /api/stt container→format mapping.
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q6 (items 1-3) + § Validation Architecture "Go unit tests" + § "Nyquist edge / sampling cases" (empty transcript) — the concrete assertions.
  </read_first>
  <action>
    Create internal/agui/voice_api.go. Declare the consumer-side seams `ttsSynthesizer` (`Synthesize(ctx context.Context, text string) ([]byte, error)`; `AudioFormat() string`) and `sttTranscriber` (`Transcribe(ctx context.Context, audio []byte, fileName, format string) (string, error)`). Add `SetVoice(tts ttsSynthesizer, stt sttTranscriber, maxChars int)` storing them on the Server. Add `registerVoiceRoutes(mux)` mounting `POST /api/tts`→`handleTTS`, `POST /api/stt`→`handleSTT`, `GET /api/voice/capabilities`→`handleVoiceCapabilities`. handleTTS: nil `s.tts`→503; `principalIdentityID`→401; decode JSON `{text}`; empty text→400; if `len(text) > s.ttsMaxChars` (and maxChars>0) take the rune-safe prefix and set response header `X-Aura-TTS-Truncated: true` (the D-05 char cap); call `s.tts.Synthesize(r.Context(), text)`; on error→502; set `Content-Type: audio/mpeg` (canonical mp3 MIME) + `X-Content-Type-Options: nosniff`; write bytes. handleSTT: nil `s.stt`→503; `principalIdentityID`→401; parse multipart, read the `audio` file part; map its part Content-Type via `assets.AudioFormat` (which strips `;codecs=`); call `s.stt.Transcribe(r.Context(), bytes, fileName, format)`; on error→502; write JSON `{text}` — and when the transcriber returns an EMPTY string this is still a clean 200 `{"text":""}` (do NOT 5xx; the empty-transcript edge ends clean with no asset call). NEVER call the asset service (no persist). handleVoiceCapabilities: `principalIdentityID`→401 (inherit the whole-mux RequireAuth); return 200 JSON `{tts: s.tts != nil, stt: s.stt != nil}` — NEVER 503. Add `tts`, `stt`, `ttsMaxChars` fields to the Server struct (server.go) and call `s.registerVoiceRoutes(mux)` in `Mux()` beside `s.registerAssetRoutes(mux)`. Create voice_api_test.go implementing the `<behavior>` suite with in-package fakes: `fakeTTS` (records gotText/calls, returns fixed bytes + a configurable error), `fakeSTT` (records gotFormat, returns a configurable text incl. the empty-string case), and a recording `fakeAssetService` wired via SetAssetService and asserted untouched for the no-persist proof (incl. the empty-transcript case). Keep voice_api.go ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `grep -q "registerVoiceRoutes" internal/agui/voice_api.go` AND `grep -q "func (s \\*Server) SetVoice" internal/agui/voice_api.go`.
    - `grep -q "registerVoiceRoutes" internal/agui/server.go` (wired into Mux()).
    - `grep -q "audio/mpeg" internal/agui/voice_api.go` AND `grep -q "X-Aura-TTS-Truncated" internal/agui/voice_api.go`.
    - voice_api.go does NOT reference the asset service in handleSTT (no persist): `handleSTT` body contains no `s.assets` call.
    - `go test ./internal/agui/ -run 'TestTTS|TestSTT|TestVoiceCapabilities'` passes (all behaviors incl. no-persist assertion, codecs→webm, AND the empty-transcript 200 {"text":""} clean case).
    - `go vet ./internal/agui/` clean; voice_api.go ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agui/ -run 'TestTTS|TestSTT|TestVoiceCapabilities' && go vet ./internal/agui/ && echo VOICE_API_OK</automated>
  </verify>
  <done>The three handlers exist over the narrow seams, wired into Mux(), with a daemon-free suite proving auth/identity/degrade/content-type/char-cap/no-persist, codecs→format mapping, and the empty-transcript clean-200 edge.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Composition-root — build mp3 web TTSClient + STTClient from config, wire SetVoice + parent-mux mounts</name>
  <files>cmd/aura/serve_voice.go, cmd/aura/serve_voice_test.go, cmd/aura/serve_webui_voice.go, cmd/aura/serve.go, cmd/aura/serve_webui.go</files>
  <behavior>
    - TestWireVoiceProviders_Mp3: buildWebTTSClient(cfg with TTSModel set) → a *multimodal.TTSClient whose AudioFormat()=="mp3" (web override), NOT the Telegram opus format.
    - TestWireVoiceProviders_OpusUntouched: the Telegram-path multimodalConfig(cfg).TTSFormat stays "opus" (assert the two coexist; a table over the web client mp3 vs the Telegram opus config).
    - TestWireVoiceProviders_Degraded: cfg with TTSModel=="" and STTCloudModel=="" → SetVoice not called with clients (nil tts/stt), so the capabilities endpoint reports {false,false} (assert via the constructed server's handler or that buildWebTTSClient returns nil).
    - (Optional, if an httptest round-trip is cheap) the web TTS client POSTs response_format=mp3 to /audio/speech while a Telegram-config client POSTs response_format=opus.
  </behavior>
  <read_first>
    - cmd/aura/serve.go:338,371,423-429 — where agui.NewServer is built and the setters (SetAssetService, wireSettingsProviders, SetAuditStore, SetIdentityAdmin) are called; add `wireVoiceProviders(aguiServer, chat.cfg)` here after NewServer.
    - cmd/aura/serve_settings.go — the wireSettingsProviders shape (a small cmd/aura wiring func) to mirror for wireVoiceProviders in the new serve_voice.go.
    - cmd/aura/serve_channels.go:110-129 — multimodalConfig(cfg): the exact config→multimodal mapping (LLM.BaseURL/APIKey as the OpenRouter credential, STTCloudModel/TTSModel/TTSVoice, MultimodalTimeoutSec) to reuse; note TTSFormat stays opus there (Telegram, untouched).
    - internal/multimodal/tts.go:18-47 + stt.go:20-45 — TTSConfig/STTConfig fields; the web TTS client sets CloudModel=cfg.TTSModel, Voice=cfg.TTSVoice, Format="mp3"; STT sets CloudModel=cfg.STTCloudModel, Language=cfg.STTLanguage.
    - cmd/aura/serve_webui_musr.go — the parent-mux mount extraction pattern (route constants + registerMUSRRoutes) to mirror EXACTLY for serve_webui_voice.go (keeps serve_webui.go ≤600).
    - cmd/aura/serve_webui.go:96-115,360,490-498,526 — agentRunCapability const, the RequireCapability mount style, where registerMUSRRoutes is called (add registerVoiceRoutes beside it), and the whole-mux RequireAuth wrap (:526) that gates all three routes.
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q6 (items 4-5) + Landmine #2 — the cloud-only "build only when model set" gating + mp3 web override.
  </read_first>
  <action>
    Create cmd/aura/serve_voice.go with `wireVoiceProviders(server *agui.Server, cfg *config.Config)`: build `tts` only when `cfg.TTSModel != ""` via `multimodal.NewTTSClient(multimodal.TTSConfig{CloudModel: cfg.TTSModel, Voice: cfg.TTSVoice, Format: "mp3", OpenRouterBaseURL: cfg.LLM.BaseURL, OpenRouterAPIKey: cfg.LLM.APIKey, TimeoutSec: cfg.MultimodalTimeoutSec})`; build `stt` only when `cfg.STTCloudModel != ""` via `multimodal.NewSTTClient(multimodal.STTConfig{CloudModel: cfg.STTCloudModel, Language: cfg.STTLanguage, OpenRouterBaseURL: cfg.LLM.BaseURL, OpenRouterAPIKey: cfg.LLM.APIKey, TimeoutSec: cfg.MultimodalTimeoutSec})`; if either is non-nil call `server.SetVoice(tts, stt, cfg.TTSMaxChars)` (a nil client ⇒ that capability=false ⇒ that POST 503s). Extract the mp3 web-client construction into a small `buildWebTTSClient(cfg)` helper so the test can assert `AudioFormat()=="mp3"` without a live call. Add the call `wireVoiceProviders(aguiServer, chat.cfg)` in serve.go after NewServer (beside the other setters). Create cmd/aura/serve_webui_voice.go mirroring serve_webui_musr.go: route constants `ttsRoute = "POST /api/tts"`, `sttRoute = "POST /api/stt"`, `voiceCapabilitiesRoute = "GET /api/voice/capabilities"`, and `registerVoiceRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps)` mounting the two POSTs behind `agui.RequireCapability(aguiHandler, auth, agentRunCapability)` (cost-bearing) and the capabilities route as bare `aguiHandler` (RequireAuth-only, like meRoute). Add `registerVoiceRoutes(mux, aguiHandler, auth)` in newServeHandler beside registerMUSRRoutes. Create serve_voice_test.go implementing the `<behavior>` cases (a nil-server-free unit test over buildWebTTSClient + the mp3-vs-opus table; wrap httptest round-trips in goleak if used). Refactor-on-touch: keep serve_webui.go ≤600 LOC (the extraction guarantees it).
  </action>
  <acceptance_criteria>
    - `grep -q "Format: *\"mp3\"" cmd/aura/serve_voice.go` (web override) AND `grep -q "SetVoice" cmd/aura/serve_voice.go`.
    - `grep -q "wireVoiceProviders(aguiServer" cmd/aura/serve.go` (wired after NewServer).
    - `grep -q "POST /api/tts" cmd/aura/serve_webui_voice.go` AND `grep -q "registerVoiceRoutes(mux, aguiHandler, auth)" cmd/aura/serve_webui.go`.
    - `grep -q "agentRunCapability" cmd/aura/serve_webui_voice.go` (the two POSTs are capability-gated).
    - `go test ./cmd/aura/ -run 'WireVoice|Voice'` passes, asserting the web TTS client AudioFormat()=="mp3" while the Telegram config stays opus.
    - `go build ./...` + `go vet ./cmd/aura/` clean; serve_webui.go ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./cmd/aura/ -run 'WireVoice|Voice' && go build ./... && go vet ./cmd/aura/ && echo VOICE_WIRING_OK</automated>
  </verify>
  <done>The composition root builds a dedicated mp3 web TTSClient + STTClient from config (Telegram opus untouched), injects them via SetVoice, and mounts the three routes on the parent mux (POSTs capability-gated, capabilities RequireAuth-only); serve_webui.go stays under 600 LOC.</done>
</task>

</tasks>

<verification>
- `go test ./internal/agui/ ./cmd/aura/` passes with the new suites (daemon-free).
- `go build ./...` + `go vet ./internal/agui/ ./cmd/aura/` clean.
- Full-matrix `-race` on the two packages runs green in WSL at the wave boundary (no goroutine leaks — goleak.VerifyNone).
- File-size hook: voice_api.go and serve_webui.go both ≤600 LOC.
</verification>

<success_criteria>
- The three voice routes answer per contract: `/api/tts`→audio/mpeg (+truncation header, char-cap prefix), `/api/stt`→transcribe-and-discard (incl. a clean 200 {"text":""} on an empty transcript), `/api/voice/capabilities`→200 {tts,stt} (never 503); 401 without a cookie; 503 on nil client for the two POSTs.
- The web TTSClient is mp3; the Telegram opus client is untouched; SetVoice injects narrow seams.
</success_criteria>

<output>
Create `.planning/phases/37C-web-voice-lane-inserted/37C-03-SUMMARY.md` when done.
</output>
