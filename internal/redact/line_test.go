package redact

import (
	"strings"
	"testing"
)

func TestLineNeutralizesLogForgingAndTerminalControls(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "clean line unchanged", input: "mount mcp server calc-live", want: "mount mcp server calc-live"},
		{name: "newline escaped visibly", input: "ok\nlevel=INFO forged=line", want: `ok\nlevel=INFO forged=line`},
		{name: "crlf escaped visibly", input: "a\r\nb", want: `a\r\nb`},
		{name: "ansi escape dropped", input: "x\x1b[2Jy", want: "x[2Jy"},
		{name: "c1 controls dropped", input: "a\u0085b\u009bc", want: "abc"},
		{name: "nul and del dropped", input: "a\x00b\x7fc", want: "abc"},
		{name: "tab survives", input: "a\tb", want: "a\tb"},
		{name: "credentials still scrubbed", input: "password=hunter2\nnext", want: "password=" + Placeholder + `\nnext`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Line(test.input); got != test.want {
				t.Fatalf("Line(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

// The invariant the call sites rely on: whatever goes in, the output can never span
// more than one log line nor carry a raw control character.
func TestLineOutputNeverCarriesRawControls(t *testing.T) {
	hostile := []string{
		"\n\n\n", "\r\n" + strings.Repeat("\x1b[31m", 10), "\u009b0;evil\x07",
		"multi\nline\rinput\x00with\x1bcontrols",
	}
	for _, input := range hostile {
		got := Line(input)
		if strings.ContainsAny(got, "\n\r\x00\x1b\x07") {
			t.Fatalf("Line(%q) = %q still carries a raw control character", input, got)
		}
	}
}
