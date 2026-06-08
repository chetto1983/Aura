package telegram

import "testing"

// TestSanitizeForSpeech proves the spoken copy is cleaned of emoji + markdown
// markup before the TTS sidecar sees it (the operator hit Kokoro voicing emoji /
// asterisks on a live voice-note reply).
func TestSanitizeForSpeech(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"emoji stripped", "Ciao! Sto benissimo 😊", "Ciao! Sto benissimo"},
		{"emoji with ZWJ sequence", "ok 👨‍👩‍👧‍👦 fine", "ok fine"},
		{"bold and italic markers", "**grassetto** e _corsivo_", "grassetto e corsivo"},
		{"inline code unwrapped", "usa `go build` ora", "usa go build ora"},
		{"link to text", "vedi [la doc](https://x.y/z) qui", "vedi la doc qui"},
		{"heading marker", "# Titolo\ntesto", "Titolo\ntesto"},
		{"list + status glyphs", "- 🟡 primo\n- ✅ secondo", "primo\nsecondo"},
		{"emoji-only is empty", "😊🟡✅", ""},
		{"plain prose untouched", "Tutto a posto, nessun simbolo.", "Tutto a posto, nessun simbolo."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeForSpeech(c.in); got != c.want {
				t.Errorf("sanitizeForSpeech(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
