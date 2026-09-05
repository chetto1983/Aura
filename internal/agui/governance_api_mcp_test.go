package agui

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/secret"
)

// MCP-row projection edge cases for the GOV-01 list handler, split out of
// governance_api_test.go to keep that file under the 600-LOC cap (CLAUDE.md
// refactor-on-touch). These pin the QUAL-02 stdlib swap in governance_api.go:
// envChips' strings.IndexByte split.
// Shared fixtures/helpers (govServer, doGov, scriptedMCPBoard, boolPtr) live in
// governance_api_test.go — same package, so they are reused, not duplicated.

// TestEnvChips_KeyExtractionAcrossUnionCases pins envChips' split-at-first-'=' behavior
// across the union cases so the strings.IndexByte swap (QUAL-02 T3) stays byte-identical:
// "k=v"→key "k", a secret "K=V"→redacted key with the value dropped, "novalue"→whole
// entry (no '=', no split), "="→empty key (skipped), ""→empty key (skipped).
func TestEnvChips_KeyExtractionAcrossUnionCases(t *testing.T) {
	got := envChips([]string{"PLAIN_SETTING=1", "OPENAI_API_KEY=sk-supersecret", "novalue", "=", ""})
	want := []mcpEnvChip{
		{Key: "PLAIN_SETTING", Redacted: secret.IsSecretEnvKey("PLAIN_SETTING")},
		{Key: "OPENAI_API_KEY", Redacted: secret.IsSecretEnvKey("OPENAI_API_KEY")},
		{Key: "novalue", Redacted: secret.IsSecretEnvKey("novalue")},
	}
	if len(got) != len(want) {
		t.Fatalf("envChips produced %d chips, want %d (a keyless '=' / '' entry must be skipped): %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chip %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The VALUE after the first '=' must never ride along on any chip (T-28-02-01).
	for _, c := range got {
		if strings.Contains(c.Key, "sk-supersecret") || strings.Contains(c.Key, "=") {
			t.Fatalf("env chip leaked the value or '=' separator: %+v", c)
		}
	}
	// The secret KEY is flagged redacted (sanity on the union fixture's redaction branch).
	if !got[1].Redacted {
		t.Fatalf("secret env key OPENAI_API_KEY was not flagged redacted: %+v", got[1])
	}
}
