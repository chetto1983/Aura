package multimodal

import (
	"strings"
	"testing"
)

// The table is the UNION of the two suites this normalizer replaced — the web lane's
// Markdown coverage (internal/agui) and the Telegram lane's emoji stripping — plus the
// cases where they used to disagree, which are now decided once and pinned here.
func TestSpeechText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		// ── Markdown syntax (was: the web lane only) ─────────────────────────────
		{"bold and italic markers", "Questo è **importante** e *utile* davvero", "Questo è importante e utile davvero"},
		{"heading", "## Crisi Internazionale\nLa notizia dominante", "Crisi Internazionale\nLa notizia dominante"},
		{"bullets and numbers", "- primo punto\n2. secondo punto\n* terzo", "primo punto\nsecondo punto\nterzo"},
		{"task list marker", "- [x] fatto\n- [ ] da fare", "fatto\nda fare"},
		{"inline code keeps text", "usa `docker compose up` ora", "usa docker compose up ora"},
		{"link keeps label drops url", "vedi [la guida](https://example.com/x) qui", "vedi la guida qui"},
		{"image alt kept", "![grafico vendite](chart.png) mostra", "grafico vendite mostra"},
		{"blockquote", "> citazione importante", "citazione importante"},
		{"table pipes to pauses", "| a | b |\n|---|---|\n| uno | due |", "a, b\n\nuno, due"},
		{"strikethrough", "prezzo ~~100~~ 80 euro", "prezzo 100 80 euro"},
		{"html tag stripped", "riga<br>successiva", "rigasuccessiva"},
		{"hrule dropped", "sopra\n---\nsotto", "sopra\n\nsotto"},
		{"autolink dropped", "fonte <https://x.y/z> qui", "fonte qui"},
		{"plain prose untouched", "Una frase normale, con virgole e punti.", "Una frase normale, con virgole e punti."},

		// ── Emoji and symbols (was: the Telegram lane only) ──────────────────────
		{"emoji stripped", "Ciao! Sto benissimo 😊", "Ciao! Sto benissimo"},
		{"emoji with ZWJ sequence", "ok 👨‍👩‍👧‍👦 fine", "ok fine"},
		{"list + status glyphs", "- 🟡 primo\n- ✅ secondo", "primo\nsecondo"},
		{"emoji-only is empty", "😊🟡✅", ""},
		{"math symbols survive — they are read as words", "2 + 2 = 4 e a < b", "2 + 2 = 4 e a < b"},

		// ── Where the two used to DISAGREE: decided once, here ───────────────────
		// Telegram read these aloud; the web lane dropped them. The web lane wins.
		{"bare url dropped (Telegram used to read it)", "fonte: https://www.ansa.it/x.html oggi", "fonte: oggi"},
		{"fenced code dropped whole (Telegram used to dictate it)", "prima\n```go\nfunc main() {}\n```\ndopo", "prima\n\ndopo"},
		{"code-only strips to empty", "```py\nprint(1)\n```", ""},
		// Telegram stripped EVERY underscore, which also ate the one inside user_id.
		{"snake_case survives", "usa il campo user_id come chiave", "usa il campo user_id come chiave"},
		{"emphasis underscore pair still unwraps", "davvero _notevole_ direi", "davvero notevole direi"},
		{"bold plus underscore italics", "**grassetto** e _corsivo_", "grassetto e corsivo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SpeechText(tc.in); got != tc.want {
				t.Fatalf("SpeechText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPrepareSpeech(t *testing.T) {
	t.Parallel()

	t.Run("normalizes before capping, so the cap counts CLEAN runes", func(t *testing.T) {
		t.Parallel()
		// 24 raw bytes of markup reduce to "importante" — a cap applied to the raw
		// text would have cut inside the markers and spoken the remains.
		spoken, truncated := PrepareSpeech("**importante**", 10)
		if spoken != "importante" || truncated {
			t.Fatalf("PrepareSpeech = %q, %v; want %q, false", spoken, truncated, "importante")
		}
	})

	t.Run("caps and reports it", func(t *testing.T) {
		t.Parallel()
		spoken, truncated := PrepareSpeech("abcdefghij", 4)
		if spoken != "abcd" || !truncated {
			t.Fatalf("PrepareSpeech = %q, %v; want %q, true", spoken, truncated, "abcd")
		}
	})

	t.Run("counts runes, never splitting a multi-byte character", func(t *testing.T) {
		t.Parallel()
		spoken, truncated := PrepareSpeech("àèìòù", 3)
		if spoken != "àèì" || !truncated {
			t.Fatalf("PrepareSpeech = %q, %v; want %q, true", spoken, truncated, "àèì")
		}
		if !strings.ContainsRune(spoken, 'ì') {
			t.Fatalf("multi-byte rune was split: %q", spoken)
		}
	})

	t.Run("a non-positive cap disables capping", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("a", 5000)
		for _, max := range []int{0, -1} {
			spoken, truncated := PrepareSpeech(long, max)
			if len(spoken) != 5000 || truncated {
				t.Fatalf("PrepareSpeech(maxChars=%d) capped at %d runes", max, len(spoken))
			}
		}
	})

	t.Run("nothing speakable is empty and never truncated", func(t *testing.T) {
		t.Parallel()
		spoken, truncated := PrepareSpeech("😊🟡✅", 4)
		if spoken != "" || truncated {
			t.Fatalf("PrepareSpeech = %q, %v; want \"\", false", spoken, truncated)
		}
	})
}

// TestConfigured pins the ONE predicate both composition roots now read — the rule
// that used to be written out at each of them, with opposite polarity.
func TestConfigured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		local, cloud string
		want         bool
	}{
		{"neither", "", "", false},
		{"local sidecar only", "http://aura-tts:8880/v1", "", true},
		{"cloud model only", "", "hexgrad/kokoro-82m", true},
		{"both", "http://aura-tts:8880/v1", "hexgrad/kokoro-82m", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tts := TTSConfig{LocalBaseURL: tc.local, CloudModel: tc.cloud}
			if got := tts.Configured(); got != tc.want {
				t.Errorf("TTSConfig.Configured() = %v, want %v", got, tc.want)
			}
			stt := STTConfig{LocalBaseURL: tc.local, CloudModel: tc.cloud}
			if got := stt.Configured(); got != tc.want {
				t.Errorf("STTConfig.Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}
