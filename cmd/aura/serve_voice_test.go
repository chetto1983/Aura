package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// voiceCaps drives the real GET /api/voice/capabilities handler on a wired server and
// returns the reported presence. It stamps a principal via identityctx (the fallback
// principalFrom reads) so the handler's auth guard passes without a live session — this
// is the true end-to-end proof that wireVoiceProviders' SetVoice injection took effect.
func voiceCaps(t *testing.T, srv *agui.Server) (tts, stt bool) {
	t.Helper()
	ctx := identityctx.WithIdentityID(context.Background(), "00000000-0000-0000-0000-000000000001")
	req := httptest.NewRequest(http.MethodGet, "/api/voice/capabilities", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d, want 200 (never 503): %s", rec.Code, rec.Body.String())
	}
	var got struct {
		TTS bool `json:"tts"`
		STT bool `json:"stt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	return got.TTS, got.STT
}

// TestWireVoiceProviders_Mp3 proves the web TTS client is the DEDICATED mp3 instance
// (D-02 / RESEARCH Landmine #2): AudioFormat()=="mp3", the web override — NOT opus.
func TestWireVoiceProviders_Mp3(t *testing.T) {
	cfg := &config.Config{TTSModel: "hexgrad/kokoro-82m", TTSVoice: "if_sara"}
	tts := buildWebTTSClient(cfg)
	if tts == nil {
		t.Fatal("buildWebTTSClient returned nil for a configured TTSModel")
	}
	if got := tts.AudioFormat(); got != "mp3" {
		t.Fatalf("web TTS AudioFormat() = %q, want mp3 (web override)", got)
	}
}

// TestWireVoiceProviders_OpusUntouched proves the web mp3 client and the Telegram opus
// path coexist (RESEARCH Landmine #2): the web client is mp3 while multimodalConfig(cfg)
// still maps TTSFormat=opus — building the web client never mutates the Telegram path.
func TestWireVoiceProviders_OpusUntouched(t *testing.T) {
	cfg := &config.Config{
		TTSModel:  "hexgrad/kokoro-82m",
		TTSVoice:  "if_sara",
		TTSFormat: "opus", // the Telegram voice-note container (config default)
	}
	web := buildWebTTSClient(cfg)
	if web == nil || web.AudioFormat() != "mp3" {
		t.Fatalf("web TTS is not mp3: nil=%v", web == nil)
	}
	if tg := multimodalConfig(cfg); tg.TTSFormat != "opus" {
		t.Fatalf("telegram TTSFormat = %q, want opus (untouched by the web mp3 override)", tg.TTSFormat)
	}
}

// TestBuildWebTTSClient_LocalRoute proves the DEFAULT web TTS routes to the LOCAL aura-tts
// (Kokoro) sidecar with the mp3 override — no cloud model set. It drives Synthesize against
// an httptest stand-in for the sidecar and asserts the OpenAI /audio/speech shape: the
// local path (no Authorization Bearer, no model id — Kokoro is single-model) with
// response_format=mp3. This is the ground truth for "web voice uses the local sidecar".
func TestBuildWebTTSClient_LocalRoute(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3-bytes"))
	}))
	defer ts.Close()

	cfg := &config.Config{TTSBaseURL: ts.URL, TTSVoice: "if_sara"} // no TTSModel → local
	client := buildWebTTSClient(cfg)
	if client == nil {
		t.Fatal("buildWebTTSClient returned nil for a configured local TTSBaseURL")
	}
	if got := client.AudioFormat(); got != "mp3" {
		t.Fatalf("web TTS AudioFormat() = %q, want mp3", got)
	}
	audio, err := client.Synthesize(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(audio) != "mp3-bytes" {
		t.Fatalf("audio = %q, want mp3-bytes", audio)
	}
	if gotPath != "/audio/speech" {
		t.Fatalf("local TTS path = %q, want /audio/speech", gotPath)
	}
	if gotAuth != "" {
		t.Fatalf("local TTS sent Authorization %q, want none (local sidecar, no key)", gotAuth)
	}
	if gotBody["response_format"] != "mp3" {
		t.Fatalf("local TTS response_format = %v, want mp3", gotBody["response_format"])
	}
	if m, ok := gotBody["model"]; ok && m != "" {
		t.Fatalf("local TTS sent model %v, want none (Kokoro is single-model)", m)
	}
}

// TestBuildWebSTTClient_LocalRoute proves the DEFAULT web STT routes to the LOCAL aura-stt
// (faster-whisper) sidecar — no cloud model set. It drives Transcribe against an httptest
// stand-in and asserts the multipart /audio/transcriptions shape: no Authorization, the
// model + language form fields carried, transcript decoded from {"text":...}.
func TestBuildWebSTTClient_LocalRoute(t *testing.T) {
	var gotPath, gotAuth, gotContentType, gotModel, gotLang string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		_ = r.ParseMultipartForm(1 << 20)
		gotModel = r.FormValue("model")
		gotLang = r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ciao mondo"}`))
	}))
	defer ts.Close()

	cfg := &config.Config{STTBaseURL: ts.URL, STTModel: "large-v3-turbo", STTLanguage: "it"} // no cloud → local
	client := buildWebSTTClient(cfg)
	if client == nil {
		t.Fatal("buildWebSTTClient returned nil for a configured local STTBaseURL")
	}
	if client.Cloud() {
		t.Fatal("web STT reports Cloud() for a local base URL (want local)")
	}
	transcript, err := client.Transcribe(context.Background(), []byte("webm-bytes"), "dictation", "webm")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if transcript != "ciao mondo" {
		t.Fatalf("transcript = %q, want 'ciao mondo'", transcript)
	}
	if gotPath != "/audio/transcriptions" {
		t.Fatalf("local STT path = %q, want /audio/transcriptions", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("local STT Content-Type = %q, want multipart/form-data", gotContentType)
	}
	if gotAuth != "" {
		t.Fatalf("local STT sent Authorization %q, want none (local sidecar)", gotAuth)
	}
	if gotModel != "large-v3-turbo" {
		t.Fatalf("local STT model field = %q, want large-v3-turbo", gotModel)
	}
	if gotLang != "it" {
		t.Fatalf("local STT language field = %q, want it", gotLang)
	}
}

// TestBuildWebSTTClient_Selectable proves the local↔cloud selection (supersedes the
// original cloud-only gating): neither set ⇒ nil (capability absent); a local base URL ⇒
// a local-routed client; a cloud model ⇒ a cloud-routed client.
func TestBuildWebSTTClient_Selectable(t *testing.T) {
	if stt := buildWebSTTClient(&config.Config{}); stt != nil {
		t.Fatal("buildWebSTTClient built a client with no base URL and no cloud model (want nil)")
	}
	local := buildWebSTTClient(&config.Config{STTBaseURL: "http://aura-stt:9000/v1"})
	if local == nil || local.Cloud() {
		t.Fatalf("STT with a local base URL is not local-routed: nil=%v", local == nil)
	}
	cloud := buildWebSTTClient(&config.Config{STTCloudModel: "openai/whisper-large-v3"})
	if cloud == nil || !cloud.Cloud() {
		t.Fatalf("STT with a cloud model is not cloud-routed: nil=%v", cloud == nil)
	}
}

// TestWireVoiceProviders_Degraded proves the fully-unset path: with NEITHER a local base
// URL NOR a cloud model the builders both return nil and wireVoiceProviders is a no-op —
// the capabilities endpoint reports {false,false} (never 503).
func TestWireVoiceProviders_Degraded(t *testing.T) {
	cfg := &config.Config{} // no base URLs, no cloud models
	if buildWebTTSClient(cfg) != nil || buildWebSTTClient(cfg) != nil {
		t.Fatal("voice clients built despite no base URL and no cloud model (want nil/nil)")
	}
	srv := agui.NewServer(nil, nil, agui.ServerConfig{})
	wireVoiceProviders(srv, cfg)
	if tts, stt := voiceCaps(t, srv); tts || stt {
		t.Fatalf("degraded caps = {tts:%v stt:%v}, want {false,false}", tts, stt)
	}
}

// TestWireVoiceProviders_Branches exercises every switch arm of wireVoiceProviders across
// BOTH the local (base-URL) and cloud (model) selectors, and proves the typed-nil footgun
// is handled: a tts-only wire must report {tts:true, stt:false}, NOT a nil-*STTClient
// wrapped in a non-nil interface (which would report stt:true and panic on the first
// call). The capabilities round-trip is the ground truth that SetVoice received a proper
// nil interface for the absent client.
func TestWireVoiceProviders_Branches(t *testing.T) {
	cases := []struct {
		name             string
		cfg              *config.Config
		wantTTS, wantSTT bool
	}{
		{"tts cloud only", &config.Config{TTSModel: "hexgrad/kokoro-82m"}, true, false},
		{"stt cloud only", &config.Config{STTCloudModel: "openai/whisper-large-v3"}, false, true},
		{"both cloud", &config.Config{TTSModel: "hexgrad/kokoro-82m", STTCloudModel: "openai/whisper-large-v3"}, true, true},
		{"tts local only", &config.Config{TTSBaseURL: "http://aura-tts:8880/v1"}, true, false},
		{"stt local only", &config.Config{STTBaseURL: "http://aura-stt:9000/v1"}, false, true},
		{"both local", &config.Config{TTSBaseURL: "http://aura-tts:8880/v1", STTBaseURL: "http://aura-stt:9000/v1"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := agui.NewServer(nil, nil, agui.ServerConfig{})
			wireVoiceProviders(srv, tc.cfg)
			tts, stt := voiceCaps(t, srv)
			if tts != tc.wantTTS || stt != tc.wantSTT {
				t.Fatalf("caps = {tts:%v stt:%v}, want {tts:%v stt:%v}", tts, stt, tc.wantTTS, tc.wantSTT)
			}
		})
	}
}
