package idempotency

import (
	"strings"
	"testing"
	"time"
)

func previewResult(preview string) ReplayResult {
	return ReplayResult{Preview: preview, ExpiresAt: time.Now().Add(time.Hour)}
}

// TestReplayPreviewAcceptsRealToolOutput pins the preview contract against what tools actually
// produce. The preview shared the validator that guards HTTP header values and the sidecar
// path, which rejects every unicode control character — correct there (CRLF injection, a
// path), fatal here: a shell result almost always ends in "\n", so practically every
// shell_exec completion was refused with "invalid idempotency replay preview: value contains
// control characters".
//
// The damage was not a failed write. The command had ALREADY run when the completion was
// validated, so the effect happened, its record was rejected, and the agent was handed an
// error for work that had succeeded — the worst way to fail, because the retry then repeats
// the effect.
func TestReplayPreviewAcceptsRealToolOutput(t *testing.T) {
	for _, tc := range []struct{ name, preview string }{
		{"trailing newline, what every shell command returns", "hello\n"},
		{"multi-line output", "total 8\ndrwxr-xr-x 2 root root\n"},
		{"tabs, as ls -l and go test emit", "PASS\tok\tgithub.com/x\t0.4s\n"},
		{"carriage return from a progress line", "downloading 50%\rdownloading 100%\n"},
		{"ANSI colour, which a coloured build emits", "\x1b[32mPASS\x1b[0m\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := previewResult(tc.preview).Validate(); err != nil {
				t.Fatalf("preview %q rejected: %v", tc.preview, err)
			}
		})
	}
}

// TestReplayPreviewStillRejectsWhatCannotBeStored keeps the two real constraints. NUL is not a
// style preference: Postgres cannot hold U+0000 in a text column, so a preview carrying one
// could never be persisted and must fail at the boundary rather than at the INSERT.
func TestReplayPreviewStillRejectsWhatCannotBeStored(t *testing.T) {
	if err := previewResult("ok\x00fail").Validate(); err == nil {
		t.Fatal("a NUL byte must be rejected: a text column cannot store it")
	} else if !strings.Contains(err.Error(), "NUL") {
		t.Errorf("error = %v, want it to name the NUL byte", err)
	}

	oversized := previewResult(strings.Repeat("x", MaxReplayPreviewBytes+1))
	if err := oversized.Validate(); err == nil {
		t.Fatal("the byte bound must still hold")
	}
}

// TestReplayHeadersStillRejectControlCharacters proves the loosening is scoped to the preview:
// a header value carrying CRLF is still refused, because that one IS an injection vector.
func TestReplayHeadersStillRejectControlCharacters(t *testing.T) {
	r := ReplayResult{
		Preview:   "ok\n",
		Headers:   map[string]string{"Content-Type": "text/plain\r\nX-Injected: yes"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := r.Validate(); err == nil {
		t.Fatal("a header value with CRLF must still be rejected")
	}
}
