package agui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/secret"
)

// MCP-row projection edge cases for the GOV-01 list handler, split out of
// governance_api_test.go to keep that file under the 600-LOC cap (CLAUDE.md
// refactor-on-touch). These pin the QUAL-02 stdlib swaps in governance_api.go:
// envChips' strings.IndexByte split and the non-nil-empty NetworkAllowlist copy.
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

// TestGovernanceMCPEmptyAllowlistIsArrayNotNull pins the non-nil-empty allowlist contract
// (Pitfall 4 / T-32-02-AL): a server with no Runtime.Network must marshal
// "networkAllowlist":[] — never null — so the cockpit always sees a list. This guards the
// stringList→inline `append([]string{}, ...)` swap from regressing to a nil-based copy.
func TestGovernanceMCPEmptyAllowlistIsArrayNotNull(t *testing.T) {
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"nonet": {Command: "nonet-bin", Enabled: new(true)}, // no Runtime.Network → empty allowlist
	}}
	s := govServer(GovernanceProviders{MCP: &scriptedMCPBoard{doc: doc}})
	rec := doGov(t, s, http.MethodGet, "/api/governance/mcp")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"networkAllowlist":null`) {
		t.Fatalf("empty Runtime.Network marshalled as null, want []: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"networkAllowlist":[]`) {
		t.Fatalf("empty Runtime.Network must marshal as [], got %s", rec.Body.String())
	}
	// Decode and assert the field is a non-nil zero-length slice (null would decode to nil).
	var resp struct {
		Servers []struct {
			NetworkAllowlist []string `json:"networkAllowlist"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(resp.Servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(resp.Servers))
	}
	if resp.Servers[0].NetworkAllowlist == nil {
		t.Fatalf("networkAllowlist decoded as nil — the wire value was null, want a non-nil empty slice")
	}
	if len(resp.Servers[0].NetworkAllowlist) != 0 {
		t.Fatalf("networkAllowlist len = %d, want 0", len(resp.Servers[0].NetworkAllowlist))
	}
}
