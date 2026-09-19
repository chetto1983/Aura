package multimodal

import (
	"strings"
	"testing"
)

// The table is the union of the two suites this replaced — the web lane's Markdown
// cases and the Telegram lane's emoji cases — plus the inputs where the two lanes used
// to answer differently, now decided once.
func TestSpeechText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{"bold and italic", "Questo è **importante** e *utile* davvero", "Questo è importante e utile davvero"},
		{"heading", "## Crisi Internazionale\nLa notizia dominante", "Crisi Internazionale\nLa notizia dominante"},
		{"setext heading", "Titolo\n======\ntesto", "Titolo\ntesto"},
		{"bullets and numbers", "- primo punto\n- secondo\n\n1. terzo", "primo punto\nsecondo\nterzo"},
		{"task list boxes", "- [x] fatto\n- [ ] da fare", "fatto\nda fare"},
		{"inline code keeps its text", "usa `docker compose up` ora", "usa docker compose up ora"},
		{"link keeps its label", "vedi [la guida](https://example.com/x) qui", "vedi la guida qui"},
		{"image keeps its alt", "![grafico vendite](chart.png) mostra", "grafico vendite mostra"},
		{"blockquote", "> citazione importante", "citazione importante"},
		{"table cells read as a list", "| a | b |\n|---|---|\n| uno | due |", "a, b\nuno, due"},
		{"strikethrough", "prezzo ~~100~~ 80 euro", "prezzo 100 80 euro"},
		{"inline html does not fuse words", "riga<br>successiva", "riga successiva"},
		{"thematic break", "sopra\n\n---\n\nsotto", "sopra\nsotto"},
		{"soft break is a space", "una riga\ncontinua qui", "una riga continua qui"},
		{"plain prose untouched", "Una frase normale, con virgole e punti.", "Una frase normale, con virgole e punti."},

		{"emoji", "Ciao! Sto benissimo 😊", "Ciao! Sto benissimo"},
		{"emoji ZWJ sequence", "ok 👨‍👩‍👧‍👦 fine", "ok fine"},
		{"list of status glyphs", "- 🟡 primo\n- ✅ secondo", "primo\nsecondo"},
		{"emoji only is empty", "😊🟡✅", ""},
		{"math symbols are words", "2 + 2 = 4", "2 + 2 = 4"},

		// Where the lanes disagreed. Telegram read the URL, dictated the code, and ate
		// the underscore in user_id; the web lane read the emoji.
		{"bare url dropped", "fonte: https://www.ansa.it/x.html oggi", "fonte: oggi"},
		{"autolink dropped", "fonte <https://x.y/z> qui", "fonte qui"},
		{"fenced code dropped whole", "prima\n\n```go\nfunc main() {}\n```\n\ndopo", "prima\ndopo"},
		{"code only is empty", "```py\nprint(1)\n```", ""},
		{"snake_case survives", "usa il campo user_id come chiave", "usa il campo user_id come chiave"},
		{"underscore emphasis unwraps", "**grassetto** e _corsivo_", "grassetto e corsivo"},
		// A pattern stripper saw "# QUESTO" here as a heading marker to remove; the
		// parser knows it is inside a fence and drops the whole block.
		{"hash inside a fence is not a heading", "```bash\n# QUESTO NO\n```\nfine", "fine"},
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
	cases := []struct {
		name          string
		in            string
		maxChars      int
		want          string
		wantTruncated bool
	}{
		// A cap on the raw text would cut inside "**" and speak the remains.
		{"cap counts clean runes", "**importante**", 10, "importante", false},
		{"over the cap", "abcdefghij", 4, "abcd", true},
		{"at the cap", "abcd", 4, "abcd", false},
		{"runes, never split mid-character", "àèìòù", 3, "àèì", true},
		{"zero disables the cap", strings.Repeat("a", 5000), 0, strings.Repeat("a", 5000), false},
		{"negative disables the cap", "abc", -1, "abc", false},
		{"nothing speakable", "😊🟡✅", 4, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, truncated := PrepareSpeech(tc.in, tc.maxChars)
			if got != tc.want || truncated != tc.wantTruncated {
				t.Fatalf("PrepareSpeech(%q, %d) = %q, %v; want %q, %v",
					tc.in, tc.maxChars, got, truncated, tc.want, tc.wantTruncated)
			}
		})
	}
}

func TestConfigured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		local, cloud string
		want         bool
	}{
		{"neither", "", "", false},
		{"local sidecar", "http://aura-tts:8880/v1", "", true},
		{"cloud model", "", "hexgrad/kokoro-82m", true},
		{"both", "http://aura-tts:8880/v1", "hexgrad/kokoro-82m", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := (TTSConfig{LocalBaseURL: tc.local, CloudModel: tc.cloud}).Configured(); got != tc.want {
				t.Errorf("TTSConfig.Configured() = %v, want %v", got, tc.want)
			}
			if got := (STTConfig{LocalBaseURL: tc.local, CloudModel: tc.cloud}).Configured(); got != tc.want {
				t.Errorf("STTConfig.Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}
