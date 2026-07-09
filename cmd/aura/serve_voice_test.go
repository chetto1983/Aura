package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestWireVoiceProviders_CloudOnlySTT proves the cloud-only STT gating (D-12): a client
// is built only when STTCloudModel is set — empty ⇒ nil (capability absent), set ⇒ a
// cloud-routed client.
func TestWireVoiceProviders_CloudOnlySTT(t *testing.T) {
	if stt := buildWebSTTClient(&config.Config{}); stt != nil {
		t.Fatal("buildWebSTTClient built a client with no STTCloudModel (want nil — cloud-only D-12)")
	}
	stt := buildWebSTTClient(&config.Config{STTCloudModel: "openai/whisper-large-v3"})
	if stt == nil {
		t.Fatal("buildWebSTTClient returned nil for a configured STTCloudModel")
	}
	if !stt.Cloud() {
		t.Fatal("web STT client is not cloud-routed")
	}
}

// TestWireVoiceProviders_Degraded proves the both-unset path (D-12): with neither model
// set the builders both return nil and wireVoiceProviders is a no-op — the capabilities
// endpoint reports {false,false} (never 503).
func TestWireVoiceProviders_Degraded(t *testing.T) {
	cfg := &config.Config{} // no TTSModel, no STTCloudModel
	if buildWebTTSClient(cfg) != nil || buildWebSTTClient(cfg) != nil {
		t.Fatal("voice clients built despite no cloud models set (want nil/nil — D-12)")
	}
	srv := agui.NewServer(nil, nil, agui.ServerConfig{})
	wireVoiceProviders(srv, cfg)
	if tts, stt := voiceCaps(t, srv); tts || stt {
		t.Fatalf("degraded caps = {tts:%v stt:%v}, want {false,false}", tts, stt)
	}
}

// TestWireVoiceProviders_Branches exercises every switch arm of wireVoiceProviders and
// proves the typed-nil footgun is handled: a tts-only wire must report {tts:true,
// stt:false}, NOT a nil-*STTClient wrapped in a non-nil interface (which would report
// stt:true and panic on the first call). The end-to-end capabilities round-trip is the
// ground truth that SetVoice received a proper nil interface for the absent client.
func TestWireVoiceProviders_Branches(t *testing.T) {
	cases := []struct {
		name             string
		cfg              *config.Config
		wantTTS, wantSTT bool
	}{
		{"tts only", &config.Config{TTSModel: "hexgrad/kokoro-82m"}, true, false},
		{"stt only", &config.Config{STTCloudModel: "openai/whisper-large-v3"}, false, true},
		{"both", &config.Config{TTSModel: "hexgrad/kokoro-82m", STTCloudModel: "openai/whisper-large-v3"}, true, true},
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
