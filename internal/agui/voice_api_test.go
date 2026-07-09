package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// fakeTTS is an in-package ttsSynthesizer: it records the text + call count and returns
// fixed bytes (or a configurable error). AudioFormat reports mp3 (the web override).
type fakeTTS struct {
	audio   []byte
	err     error
	gotText string
	calls   int
}

func (f *fakeTTS) Synthesize(_ context.Context, text string) ([]byte, error) {
	f.calls++
	f.gotText = text
	if f.err != nil {
		return nil, f.err
	}
	return f.audio, nil
}

func (f *fakeTTS) AudioFormat() string { return "mp3" }

// fakeSTT is an in-package sttTranscriber: it records the mapped format + fileName +
// call count and returns a configurable transcript (incl. the empty-string edge) or an
// error.
type fakeSTT struct {
	text      string
	err       error
	gotFormat string
	gotFile   string
	calls     int
}

func (f *fakeSTT) Transcribe(_ context.Context, _ []byte, fileName, format string) (string, error) {
	f.calls++
	f.gotFile = fileName
	f.gotFormat = format
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

// assertAssetServiceUntouched is the no-persist proof (D-08): after a voice handler
// runs, a recording fakeAssetService wired via SetAssetService must show no method was
// reached — no presign/create, no read, no list. handleSTT/handleTTS never call the
// asset service, so every recording field stays zero.
func assertAssetServiceUntouched(t *testing.T, f *fakeAssetService) {
	t.Helper()
	if f.presignReq.FileName != "" || f.getID != "" || f.openID != "" || f.listThreadID != "" || f.listIdentityID != "" {
		t.Fatalf("asset service was touched (transcribe/synthesize-and-discard violated): presign=%q get=%q open=%q listThread=%q listIdentity=%q",
			f.presignReq.FileName, f.getID, f.openID, f.listThreadID, f.listIdentityID)
	}
}

// newSTTRequest builds a POST /api/stt multipart request whose `audio` part carries the
// given Content-Type (e.g. "audio/webm;codecs=opus") — CreatePart (not CreateFormFile)
// so the part's Content-Type is set, which the handler maps via assets.AudioFormat.
func newSTTRequest(t *testing.T, partContentType string, data []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="audio"; filename="clip.webm"`)
	hdr.Set("Content-Type", partContentType)
	fw, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/stt", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func newVoiceServer() *Server {
	return NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
}

// TestTTS_Owner is the happy path: an authed owner POSTs {text}, gets audio/mpeg + the
// exact synth bytes, the synthesizer sees the text, and the recording asset service is
// UNTOUCHED (synthesize-and-discard).
func TestTTS_Owner(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{audio: []byte("ID3\x03\x00fake-mp3-bytes")}
	assetSvc := &fakeAssetService{}
	s := newVoiceServer()
	s.SetAssetService(assetSvc)
	s.SetVoice(tts, &fakeSTT{}, 4096)

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts", strings.NewReader(`{"text":"hi"}`)), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("Content-Type = %q, want audio/mpeg", ct)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, tts.audio) {
		t.Fatalf("body = %q, want %q", got, tts.audio)
	}
	if tts.gotText != "hi" {
		t.Fatalf("synth gotText = %q, want hi", tts.gotText)
	}
	if got := rec.Header().Get("X-Aura-TTS-Truncated"); got != "" {
		t.Fatalf("unexpected truncation header %q for a short input", got)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", ns)
	}
	assertAssetServiceUntouched(t, assetSvc)
}

// TestTTS_Unauth proves the route adds no unauthenticated surface: wrapped in RequireAuth
// with no cookie it is 401 and the synthesizer is never reached.
func TestTTS_Unauth(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{audio: []byte("x")}
	s := newVoiceServer()
	s.SetVoice(tts, &fakeSTT{}, 4096)
	gated := RequireAuth(s.Mux(), testDeps("operator-secret"))

	req := httptest.NewRequest(http.MethodPost, "/api/tts", strings.NewReader(`{"text":"hi"}`))
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no session)", rec.Code)
	}
	if tts.calls != 0 {
		t.Fatalf("synth reached without a session: calls=%d", tts.calls)
	}
}

// TestTTS_Degraded: with no SetVoice (nil tts) the POST answers 503.
func TestTTS_Degraded(t *testing.T) {
	t.Parallel()
	s := newVoiceServer()
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts", strings.NewReader(`{"text":"hi"}`)), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (nil tts)", rec.Code)
	}
}

// TestTTS_CharCap covers the D-05 soft cap: over the cap → the synthesizer receives the
// rune-safe prefix AND the response carries X-Aura-TTS-Truncated: true; exactly at the
// cap → no truncation header; empty text → 400 with no synth call.
func TestTTS_CharCap(t *testing.T) {
	t.Parallel()
	const maxChars = 8
	cases := []struct {
		name          string
		text          string
		wantStatus    int
		wantSynthText string // "" ⇒ synth must NOT be called
		wantTruncated bool
	}{
		{"under cap", "hello", http.StatusOK, "hello", false},
		{"at cap", "abcdefgh", http.StatusOK, "abcdefgh", false},
		{"over cap", "abcdefghIJKL", http.StatusOK, "abcdefgh", true},
		{"empty", "", http.StatusBadRequest, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tts := &fakeTTS{audio: []byte("audio")}
			s := newVoiceServer()
			s.SetVoice(tts, &fakeSTT{}, maxChars)
			payload, err := json.Marshal(map[string]string{"text": tc.text})
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts", bytes.NewReader(payload)), localIdentityID)
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantSynthText == "" {
				if tts.calls != 0 {
					t.Fatalf("synth called for empty text: calls=%d", tts.calls)
				}
				return
			}
			if tts.gotText != tc.wantSynthText {
				t.Fatalf("synth text = %q, want %q (rune-safe prefix)", tts.gotText, tc.wantSynthText)
			}
			truncated := rec.Header().Get("X-Aura-TTS-Truncated") == "true"
			if truncated != tc.wantTruncated {
				t.Fatalf("X-Aura-TTS-Truncated present = %v, want %v", truncated, tc.wantTruncated)
			}
		})
	}
}

// TestSTT_Owner is the happy path: an authed owner uploads a webm;codecs=opus part; the
// handler strips the codecs suffix to "webm", returns the transcript, and the recording
// asset service is UNTOUCHED (transcribe-and-discard).
func TestSTT_Owner(t *testing.T) {
	t.Parallel()
	stt := &fakeSTT{text: "ciao mondo"}
	assetSvc := &fakeAssetService{}
	s := newVoiceServer()
	s.SetAssetService(assetSvc)
	s.SetVoice(&fakeTTS{}, stt, 4096)

	req := withPrincipal(newSTTRequest(t, "audio/webm;codecs=opus", []byte("opus-container-bytes")), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Text != "ciao mondo" {
		t.Fatalf("text = %q, want 'ciao mondo'", got.Text)
	}
	if stt.gotFormat != "webm" {
		t.Fatalf("stt gotFormat = %q, want webm (;codecs=opus stripped)", stt.gotFormat)
	}
	assertAssetServiceUntouched(t, assetSvc)
}

// TestSTT_EmptyTranscript is the Nyquist edge: the transcriber yields "" (silence) → a
// clean 200 {"text":""}, never a 5xx, and STILL no asset is written.
func TestSTT_EmptyTranscript(t *testing.T) {
	t.Parallel()
	stt := &fakeSTT{text: ""}
	assetSvc := &fakeAssetService{}
	s := newVoiceServer()
	s.SetAssetService(assetSvc)
	s.SetVoice(&fakeTTS{}, stt, 4096)

	req := withPrincipal(newSTTRequest(t, "audio/webm;codecs=opus", []byte("silence")), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want a clean 200 on an empty transcript (never 5xx): %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v, ok := got["text"]; !ok || v != "" {
		t.Fatalf(`body = %v, want {"text":""}`, got)
	}
	if stt.calls != 1 {
		t.Fatalf("transcribe calls = %d, want 1", stt.calls)
	}
	assertAssetServiceUntouched(t, assetSvc)
}

// TestSTT_Unauth: RequireAuth + no cookie → 401, transcriber never reached.
func TestSTT_Unauth(t *testing.T) {
	t.Parallel()
	stt := &fakeSTT{text: "x"}
	s := newVoiceServer()
	s.SetVoice(&fakeTTS{}, stt, 4096)
	gated := RequireAuth(s.Mux(), testDeps("operator-secret"))

	req := newSTTRequest(t, "audio/webm;codecs=opus", []byte("x"))
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no session)", rec.Code)
	}
	if stt.calls != 0 {
		t.Fatalf("transcribe reached without a session: calls=%d", stt.calls)
	}
}

// TestSTT_Degraded: with no SetVoice (nil stt) the POST answers 503.
func TestSTT_Degraded(t *testing.T) {
	t.Parallel()
	s := newVoiceServer()
	req := withPrincipal(newSTTRequest(t, "audio/webm;codecs=opus", []byte("x")), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (nil stt)", rec.Code)
	}
}

// TestVoiceCapabilities is the presence-probe table (D-11): both/tts-only/stt-only/none
// map to the reflected {tts,stt}, and the unconfigured case is a 200 {false,false} —
// NEVER 503.
func TestVoiceCapabilities(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		setTTS, setSTT   bool
		wantTTS, wantSTT bool
	}{
		{"both", true, true, true, true},
		{"tts only", true, false, true, false},
		{"stt only", false, true, false, true},
		{"none", false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newVoiceServer()
			// Pass nil INTERFACES (not typed-nil concretes) so s.tts/s.stt report absence
			// correctly — the same contract the composition root's SetVoice guard upholds.
			var tts ttsSynthesizer
			var stt sttTranscriber
			if tc.setTTS {
				tts = &fakeTTS{}
			}
			if tc.setSTT {
				stt = &fakeSTT{}
			}
			if tc.setTTS || tc.setSTT {
				s.SetVoice(tts, stt, 4096)
			}
			req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/voice/capabilities", nil), localIdentityID)
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (capabilities is never 503): %s", rec.Code, rec.Body.String())
			}
			var got voiceCapabilitiesDTO
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.TTS != tc.wantTTS || got.STT != tc.wantSTT {
				t.Fatalf("caps = {tts:%v stt:%v}, want {tts:%v stt:%v}", got.TTS, got.STT, tc.wantTTS, tc.wantSTT)
			}
		})
	}
}

// TestVoiceCapabilities_Unauth: RequireAuth + no cookie → 401 even though the clients
// are wired (the whole-mux gate fires before the handler).
func TestVoiceCapabilities_Unauth(t *testing.T) {
	t.Parallel()
	s := newVoiceServer()
	s.SetVoice(&fakeTTS{}, &fakeSTT{}, 4096)
	gated := RequireAuth(s.Mux(), testDeps("operator-secret"))

	req := httptest.NewRequest(http.MethodGet, "/api/voice/capabilities", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no session)", rec.Code)
	}
}

// TestCapText is the direct rune-cap unit: a non-positive cap disables truncation, an
// input at/under the cap is untouched, and an over-cap input is cut to the rune-safe
// prefix (multi-byte runes counted as one, never split mid-character).
func TestCapText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		text      string
		maxChars  int
		wantText  string
		wantTrunc bool
	}{
		{"cap disabled (zero)", "abcdef", 0, "abcdef", false},
		{"cap disabled (negative)", "abcdef", -1, "abcdef", false},
		{"under cap", "abc", 8, "abc", false},
		{"at cap", "abcdefgh", 8, "abcdefgh", false},
		{"over cap ascii", "abcdefghXY", 8, "abcdefgh", true},
		{"over cap multibyte (rune-safe)", "àéîõü✓漢字", 4, "àéîõ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, trunc := capText(tc.text, tc.maxChars)
			if got != tc.wantText || trunc != tc.wantTrunc {
				t.Fatalf("capText(%q, %d) = (%q, %v), want (%q, %v)", tc.text, tc.maxChars, got, trunc, tc.wantText, tc.wantTrunc)
			}
		})
	}
}

// TestTTS_SynthError: a synthesizer failure is a 502 (BadGateway) — an upstream fault,
// not a server bug (500).
func TestTTS_SynthError(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{err: errors.New("openrouter 500")}
	s := newVoiceServer()
	s.SetVoice(tts, &fakeSTT{}, 4096)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts", strings.NewReader(`{"text":"hi"}`)), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (synth error)", rec.Code)
	}
}

// TestTTS_InvalidJSON: a malformed body is a 400 with no synth call.
func TestTTS_InvalidJSON(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{audio: []byte("x")}
	s := newVoiceServer()
	s.SetVoice(tts, &fakeSTT{}, 4096)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts", strings.NewReader(`{"text":`)), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (invalid JSON)", rec.Code)
	}
	if tts.calls != 0 {
		t.Fatalf("synth called on invalid JSON: calls=%d", tts.calls)
	}
}

// TestTTS_NoPrincipal is the handler's own defense-in-depth 401 (bare mux, no principal —
// independent of the parent-mux RequireAuth): the POST is 401 and synth is never called.
func TestTTS_NoPrincipal(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{audio: []byte("x")}
	s := newVoiceServer()
	s.SetVoice(tts, &fakeSTT{}, 4096)
	req := httptest.NewRequest(http.MethodPost, "/api/tts", strings.NewReader(`{"text":"hi"}`))
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no principal on the bare mux)", rec.Code)
	}
	if tts.calls != 0 {
		t.Fatalf("synth called with no principal: calls=%d", tts.calls)
	}
}

// TestSTT_TranscribeError: a transcriber failure is a 502, and still no asset is written.
func TestSTT_TranscribeError(t *testing.T) {
	t.Parallel()
	stt := &fakeSTT{err: errors.New("openrouter 500")}
	assetSvc := &fakeAssetService{}
	s := newVoiceServer()
	s.SetAssetService(assetSvc)
	s.SetVoice(&fakeTTS{}, stt, 4096)
	req := withPrincipal(newSTTRequest(t, "audio/webm;codecs=opus", []byte("x")), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (transcribe error)", rec.Code)
	}
	assertAssetServiceUntouched(t, assetSvc)
}

// TestSTT_MissingAudioPart: a body with no `audio` part is a 400 with no transcribe call.
func TestSTT_MissingAudioPart(t *testing.T) {
	t.Parallel()
	stt := &fakeSTT{text: "x"}
	s := newVoiceServer()
	s.SetVoice(&fakeTTS{}, stt, 4096)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/stt", strings.NewReader("not-multipart")), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing audio part)", rec.Code)
	}
	if stt.calls != 0 {
		t.Fatalf("transcribe called with no audio part: calls=%d", stt.calls)
	}
}

// TestSTT_NoPrincipal is the handler's own 401 (bare mux, no principal).
func TestSTT_NoPrincipal(t *testing.T) {
	t.Parallel()
	stt := &fakeSTT{text: "x"}
	s := newVoiceServer()
	s.SetVoice(&fakeTTS{}, stt, 4096)
	req := newSTTRequest(t, "audio/webm;codecs=opus", []byte("x"))
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no principal)", rec.Code)
	}
	if stt.calls != 0 {
		t.Fatalf("transcribe called with no principal: calls=%d", stt.calls)
	}
}

// TestVoiceCapabilities_NoPrincipal is the handler's own 401 branch (bare mux, no
// principal) — distinct from the parent-mux RequireAuth gate exercised in _Unauth.
func TestVoiceCapabilities_NoPrincipal(t *testing.T) {
	t.Parallel()
	s := newVoiceServer()
	s.SetVoice(&fakeTTS{}, &fakeSTT{}, 4096)
	req := httptest.NewRequest(http.MethodGet, "/api/voice/capabilities", nil)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no principal on the bare mux)", rec.Code)
	}
}
