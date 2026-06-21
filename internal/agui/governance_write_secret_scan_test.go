package agui

// governance_write_secret_scan_test.go is the held-out "no raw secret anywhere"
// backstop (SPEC Prohibition #1 / T-29-05-01). It drives a FULL governance-write run —
// an MCP install carrying a real secret env, an env-edit that submits a real secret and
// then re-submits the redacted ${KEY} placeholder, and a skill install whose source
// string carries a secret — against the httptest server with the global slog default
// handler captured, then greps EVERY captured log line AND every /api/governance/*
// response body for the seeded secret values and asserts ZERO matches. The fakes hold
// the REAL secret internally (so a broken redaction WOULD leak it on the wire), which is
// what makes the held-out meaningful rather than vacuous.
//
// It also carries the redacted-placeholder PROPERTY backstop in this package's reach by
// asserting the four-state env merge preserves every secret across a table (the wire-side
// of the property; the value-level property over many keys lives in
// internal/mcp/manager TestSetServerEnvPreservesAllSecrets).

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// seededSecrets are the distinct secret VALUES driven through the full run. None of them
// may appear in any captured log line or any /api/governance/* response body. Each is a
// realistic shape (OpenAI key, bearer token, Postgres DSN, password) so the grep exercises
// the token/DSN/password patterns the SPEC names.
var seededSecrets = []string{
	"sk-realsecret-0xCAFEBABE",                        // OPENAI_API_KEY
	"Bearer-eyJ0b2tlbiI6InNlY3JldCJ9",                 // AUTH_TOKEN
	"postgres://u:p4ssw0rd-DSN@db.internal:5432/prod", // DATABASE_URL (DSN)
	"hunter2-the-password",                            // SERVICE_PASSWORD
	"skill-source-secret-9f8e7d",                      // a secret embedded in a skill source string
}

// secretScanFakeMCP is an MCPWriteProvider whose results carry the REAL stored secret env
// internally, so the handler's key-only redaction is what keeps the value off the wire. If
// the handler ever echoed Server.Env verbatim, the seeded value would leak and the scan
// would catch it.
type secretScanFakeMCP struct {
	*fakeMCPWrite
}

// secretScanFakeSkills is a SkillsWriteProvider whose install echoes the source (which
// carries a secret) into the SkillsInstallInfo.Source field. The handler must sanitize the
// source before it crosses the wire / a log; this fake makes a leak observable.
type secretScanFakeSkills struct {
	*fakeSkillsWrite
}

// TestGovernanceWriteSecretScan drives the full MCP-install + env-edit + skill-install run
// with the slog default handler captured, then asserts ZERO seeded secret values appear in
// any captured log line OR any /api/governance/* response body.
func TestGovernanceWriteSecretScan(t *testing.T) {
	// Capture the global slog default handler for the duration of the run (the manager /
	// provider code logs through slog.Default()). Restored on cleanup.
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	mcpFake := &secretScanFakeMCP{fakeMCPWrite: newFakeMCPWrite()}
	skillsFake := &secretScanFakeSkills{fakeSkillsWrite: newFakeSkillsWrite()}

	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{MCP: mcpFake, Skills: skillsFake})

	var responses []string

	// (1) MCP install carrying a real secret env (OPENAI_API_KEY + a non-secret flag).
	installBody := `{"name":"leaky","command":"leaky-bin","env":["OPENAI_API_KEY=` + seededSecrets[0] + `","AUTH_TOKEN=` + seededSecrets[1] + `","PUBLIC_FLAG=on"]}`
	r1 := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp", installBody, true)
	if r1.Code != http.StatusOK {
		t.Fatalf("install status=%d: %s", r1.Code, r1.Body.String())
	}
	responses = append(responses, r1.Body.String())

	// Seed a DSN + password directly into the stored config so the env-edit echo path has a
	// secret to (not) leak.
	mcpFake.doc.MCPServers["leaky"] = mcp.ManagedServer{
		Command: "leaky-bin",
		Env: []string{
			"OPENAI_API_KEY=" + seededSecrets[0],
			"DATABASE_URL=" + seededSecrets[2],
			"SERVICE_PASSWORD=" + seededSecrets[3],
			"PUBLIC_FLAG=on",
		},
	}

	// (2a) env-edit submitting a REAL new secret (rotation) — the value must not echo back.
	editBody := `{"env":["OPENAI_API_KEY=` + seededSecrets[0] + `","DATABASE_URL=${DATABASE_URL}","SERVICE_PASSWORD=${SERVICE_PASSWORD}","PUBLIC_FLAG=off"]}`
	r2 := doGovWrite(t, s, http.MethodPatch, "/api/governance/mcp/leaky/env", editBody, true)
	if r2.Code != http.StatusOK {
		t.Fatalf("env-edit status=%d: %s", r2.Code, r2.Body.String())
	}
	responses = append(responses, r2.Body.String())

	// (2b) env-edit re-submitting the redacted ${KEY} placeholders (preserve) — again no value.
	editBody2 := `{"env":["OPENAI_API_KEY=${OPENAI_API_KEY}","DATABASE_URL=${DATABASE_URL}","SERVICE_PASSWORD=${SERVICE_PASSWORD}","PUBLIC_FLAG=off"]}`
	r3 := doGovWrite(t, s, http.MethodPatch, "/api/governance/mcp/leaky/env", editBody2, true)
	if r3.Code != http.StatusOK {
		t.Fatalf("env-edit(placeholder) status=%d: %s", r3.Code, r3.Body.String())
	}
	responses = append(responses, r3.Body.String())

	// Confirm the stored secrets actually survived the placeholder edit (the preserve path),
	// so the scan is not trivially clean because the secrets were silently dropped.
	if v, _ := envValueTest(mcpFake.doc.MCPServers["leaky"].Env, "DATABASE_URL"); v != seededSecrets[2] {
		t.Fatalf("DATABASE_URL not preserved through the placeholder edit: got %q", v)
	}
	if v, _ := envValueTest(mcpFake.doc.MCPServers["leaky"].Env, "SERVICE_PASSWORD"); v != seededSecrets[3] {
		t.Fatalf("SERVICE_PASSWORD not preserved through the placeholder edit: got %q", v)
	}

	// (3) skill install whose source string carries a secret.
	skillBody := `{"source":"owner/repo?token=` + seededSecrets[4] + `"}`
	r4 := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/install", skillBody, true)
	if r4.Code != http.StatusOK {
		t.Fatalf("skill-install status=%d: %s", r4.Code, r4.Body.String())
	}
	responses = append(responses, r4.Body.String())

	// --- The held-out grep: ZERO seeded secret values in any response body or log line ---
	allResponses := strings.Join(responses, "\n----\n")
	for _, secretVal := range seededSecrets {
		if strings.Contains(allResponses, secretVal) {
			t.Errorf("LEAK: seeded secret value %q appears in a /api/governance/* response body", redactForReport(secretVal))
		}
	}
	logs := logBuf.String()
	for _, secretVal := range seededSecrets {
		if strings.Contains(logs, secretVal) {
			t.Errorf("LEAK: seeded secret value %q appears in a captured log line", redactForReport(secretVal))
		}
	}

	// Belt: the generic token/DSN/password shape patterns must also be absent from the wire
	// (the response should carry only KEY + a redaction marker). We scan the responses for the
	// distinctive secret substrings rather than the broad words (which legitimately appear as
	// KEY names like AUTH_TOKEN), so this asserts no VALUE crosses.
	for _, frag := range []string{"sk-real", "p4ssw0rd", "hunter2", "eyJ0b2tlbiI", "skill-source-secret"} {
		if strings.Contains(allResponses, frag) {
			t.Errorf("LEAK: secret fragment %q crossed the wire", frag)
		}
	}

	t.Logf("secret-scan: %d responses + %d bytes of logs scanned over a full install+edit+skill-install run; zero secret values found",
		len(responses), len(logs))
}

// redactForReport masks a secret in a failure message so the test output itself does not
// echo the leaked value verbatim.
func redactForReport(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "…" + s[len(s)-3:]
}

// TestSetServerEnvPreservesAllSecretsWire is the wire-level companion of the value-level
// property test: for a table of secret keys, submitting the redacted ${KEY} placeholder
// preserves the stored value through the handler, and the value never appears on the wire.
// (The exhaustive property over many random keys lives in
// internal/mcp/manager TestSetServerEnvPreservesAllSecrets.)
func TestSetServerEnvPreservesAllSecretsWire(t *testing.T) {
	cases := []struct{ key, val string }{
		{"OPENAI_API_KEY", "sk-aaa-111"},
		{"AUTH_TOKEN", "tok-bbb-222"},
		{"DATABASE_URL", "postgres://u:ccc-333@h/db"},
		{"SERVICE_PASSWORD", "ddd-444-pass"},
		{"PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----eee555"},
		{"SESSION_SECRET", "sess-fff-666"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, fake := govWriteServer(newFakeMCPWrite())
			fake.doc.MCPServers["srv"] = mcp.ManagedServer{Command: "srv-bin", Env: []string{c.key + "=" + c.val}}

			body := `{"env":["` + c.key + `=${` + c.key + `}"]}`
			rec := doGovWrite(t, s, http.MethodPatch, "/api/governance/mcp/srv/env", body, true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
			}
			// Stored value preserved exactly (never the placeholder text).
			if got, _ := envValueTest(fake.doc.MCPServers["srv"].Env, c.key); got != c.val {
				t.Fatalf("stored %s = %q, want %q (placeholder must preserve, never overwrite)", c.key, got, c.val)
			}
			// Value never crossed the wire.
			if strings.Contains(rec.Body.String(), c.val) {
				t.Fatalf("secret value for %s leaked on the wire: %s", c.key, rec.Body.String())
			}
		})
	}
}

// ensure the fakes satisfy the provider interfaces (compile-time guard).
var (
	_ MCPWriteProvider    = (*secretScanFakeMCP)(nil)
	_ SkillsWriteProvider = (*secretScanFakeSkills)(nil)
)
