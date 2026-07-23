package agui

// voice_speech_text_test.go pins the Markdown→speech stripping (operator directive
// 2026-07-23: TTS must never read raw Markdown syntax aloud) plus the handleTTS
// integration: the synthesizer receives CLEAN text and a strip-to-empty body 400s.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpeechText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{"bold and italic markers", "Questo è **importante** e *utile* davvero", "Questo è importante e utile davvero"},
		{"heading", "## Crisi Internazionale\nLa notizia dominante", "Crisi Internazionale\nLa notizia dominante"},
		{"bullets and numbers", "- primo punto\n2. secondo punto\n* terzo", "primo punto\nsecondo punto\nterzo"},
		{"task list marker", "- [x] fatto\n- [ ] da fare", "fatto\nda fare"},
		{"inline code keeps text", "usa `docker compose up` ora", "usa docker compose up ora"},
		{"fenced code block dropped", "prima\n```go\nfunc main() {}\n```\ndopo", "prima\n\ndopo"},
		{"link keeps label drops url", "vedi [la guida](https://example.com/x) qui", "vedi la guida qui"},
		{"bare url dropped", "fonte: https://www.ansa.it/sito/notizie/x.html oggi", "fonte: oggi"},
		{"image alt kept", "![grafico vendite](chart.png) mostra", "grafico vendite mostra"},
		{"blockquote", "> citazione importante", "citazione importante"},
		// The dropped separator row leaves a paragraph break — a natural TTS pause.
		{"table pipes to pauses", "| a | b |\n|---|---|\n| uno | due |", "a, b\n\nuno, due"},
		{"strikethrough", "prezzo ~~100~~ 80 euro", "prezzo 100 80 euro"},
		{"html tag stripped", "riga<br>successiva", "rigasuccessiva"},
		{"snake_case survives", "usa il campo user_id come chiave", "usa il campo user_id come chiave"},
		{"emphasis underscore pair", "davvero _notevole_ direi", "davvero notevole direi"},
		{"hrule dropped", "sopra\n---\nsotto", "sopra\n\nsotto"},
		{"code-only strips to empty", "```py\nprint(1)\n```", ""},
		{"plain prose untouched", "Una frase normale, con virgole e punti.", "Una frase normale, con virgole e punti."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := speechText(tc.in); got != tc.want {
				t.Fatalf("speechText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHandleTTSStripsMarkdownBeforeSynthesis(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{audio: []byte("ID3fake")}
	s := newVoiceServer()
	s.SetVoice(tts, &fakeSTT{}, 4096)

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts",
		strings.NewReader(`{"text":"## Titolo\n**grassetto** e [link](https://x.y) qui"}`)), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.ContainsAny(tts.gotText, "#*[]()") || strings.Contains(tts.gotText, "https://") {
		t.Fatalf("synthesizer received raw markdown: %q", tts.gotText)
	}
	if !strings.Contains(tts.gotText, "grassetto e link qui") {
		t.Fatalf("prose lost in stripping: %q", tts.gotText)
	}
}

func TestHandleTTSCodeOnlyBodyIs400(t *testing.T) {
	t.Parallel()
	tts := &fakeTTS{audio: []byte("x")}
	s := newVoiceServer()
	s.SetVoice(tts, &fakeSTT{}, 4096)

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/tts",
		strings.NewReader("{\"text\":\"```\\ncode only\\n```\"}")), localIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code-only text: status = %d, want 400 (no speakable content)", rec.Code)
	}
	if tts.calls != 0 {
		t.Fatalf("synthesizer reached %d time(s) for unspeakable input, want 0", tts.calls)
	}
}
