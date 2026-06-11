package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/profile"
)

// TestCacheStats_ParseSince covers the --since flag contract: valid durations
// (both `--since=1h` and `--since 24h` forms) parse; a missing or unparseable
// flag is a usage error (exit 64) WITHOUT touching the DB (T-06-02).
func TestCacheStats_ParseSince(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"equals-form", []string{"--since=1h"}, false},
		{"space-form", []string{"--since", "24h"}, false},
		{"minutes", []string{"--since=90m"}, false},
		{"missing", []string{}, true},
		{"empty", []string{"--since="}, true},
		{"bogus", []string{"--since=bogus"}, true},
		{"negative", []string{"--since=-1h"}, true},
		{"zero", []string{"--since=0s"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			_, code := parseSince(tc.args, &errOut)
			if tc.wantErr {
				if code != exitUsage {
					t.Fatalf("args %v: want exit %d, got %d", tc.args, exitUsage, code)
				}
				if errOut.Len() == 0 {
					t.Fatalf("args %v: want a diagnostic on stderr", tc.args)
				}
				return
			}
			if code != 0 {
				t.Fatalf("args %v: want exit 0, got %d (stderr=%q)", tc.args, code, errOut.String())
			}
		})
	}
}

// TestCacheStats_HitRate asserts the divide-by-zero guard: zero prompt tokens
// reads "n/a", never a divide.
func TestCacheStats_HitRate(t *testing.T) {
	if got := hitRate(0, 0); got != "n/a" {
		t.Fatalf("hitRate(0,0) = %q, want n/a", got)
	}
	if got := hitRate(80, 100); got != "80.0%" {
		t.Fatalf("hitRate(80,100) = %q, want 80.0%%", got)
	}
}

// TestCacheAudit_AllEqual_Exit0 is the SC#1 positive proof: the real 20-turn replay
// prints one `request NN: <hex>` (messages[0]) line per LLM request, all identical, and
// exits 0. Phase 14 adds the `messages1 NN:` profile/skills block and `skillman NN:`
// manifest-in-Description streams over the same replay; each must also be internally
// byte-stable. Runtime-faithful (the real Runner.Turn loop), Postgres-free.
func TestCacheAudit_AllEqual_Exit0(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cacheAuditMain(context.Background(), nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("cacheAuditMain exit %d, want 0 (stderr=%q)", code, errOut.String())
	}
	wantRequests := cacheFixtureRequestCount(t)
	// Each stream (messages[0] "request", messages[1] "messages1", skill manifest
	// "skillman") must have exactly wantRequests internally-identical hashes.
	for _, prefix := range []string{"request", "messages1", "skillman"} {
		lines := linesWithPrefix(out.String(), prefix+" ")
		if len(lines) != wantRequests {
			t.Fatalf("stream %q: want exactly %d hash lines, got %d:\n%s", prefix, wantRequests, len(lines), out.String())
		}
		first := hashOfLine(t, lines[0])
		for i, ln := range lines {
			if h := hashOfLine(t, ln); h != first {
				t.Fatalf("stream %q line %d hash %q != first %q (a drift the gate must catch)", prefix, i+1, h, first)
			}
		}
	}
}

func TestCacheAudit_ProfileContextShape(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	turns, code := loadFixtures(filepath.Join(root, auditFixtureDir), new(bytes.Buffer))
	if code != 0 {
		t.Fatalf("loadFixtures exit %d", code)
	}

	var errOut bytes.Buffer
	reqs, code := replayAudit(context.Background(), turns, &errOut)
	if code != 0 {
		t.Fatalf("replayAudit exit %d (stderr=%q)", code, errOut.String())
	}
	if len(reqs) == 0 || len(reqs[0].Messages) < 2 {
		t.Fatalf("audit replay emitted no messages[1] profile block: %+v", reqs)
	}

	first := reqs[0]
	if !strings.Contains(first.Messages[0].Content, "Agent.md") ||
		!strings.Contains(first.Messages[0].Content, "messages[1]") {
		t.Fatalf("messages[0] must explain Agent.md profile context doctrine:\n%s", first.Messages[0].Content)
	}
	if strings.Contains(first.Messages[0].Content, profile.ProfileBlockStart) ||
		strings.Contains(first.Messages[0].Content, "Name: Cache Audit") {
		t.Fatalf("Agent.md profile contents must not leak into messages[0]:\n%s", first.Messages[0].Content)
	}
	if first.Messages[1].Role != llm.RoleUser {
		t.Fatalf("messages[1] role = %q, want user", first.Messages[1].Role)
	}
	profileAt := strings.Index(first.Messages[1].Content, profile.ProfileBlockStart)
	skillsAt := strings.Index(first.Messages[1].Content, "Active skill instructions (always-on):")
	if profileAt < 0 || skillsAt < 0 {
		t.Fatalf("messages[1] must include profile and always-on skill content:\n%s", first.Messages[1].Content)
	}
	if profileAt > skillsAt {
		t.Fatalf("messages[1] must be profile-first, got:\n%s", first.Messages[1].Content)
	}
	if !strings.Contains(first.Messages[1].Content, "Name: Cache Audit") {
		t.Fatalf("messages[1] missing deterministic Agent.md content:\n%s", first.Messages[1].Content)
	}
}

// linesWithPrefix returns the non-empty stdout lines whose text starts with prefix.
func linesWithPrefix(s, prefix string) []string {
	var out []string
	for _, ln := range nonEmptyLines(s) {
		if strings.HasPrefix(ln, prefix) {
			out = append(out, ln)
		}
	}
	return out
}

// TestCacheAudit_Mutation_Exit1 is the SC#5 NEGATIVE proof: when messages[0]
// drifts between requests the audit exits 1 with the explicit `messages[0] mutated
// at request N` wording. It drives the reportHashes seam directly with a poisoned
// request list so the contract is proven without a real prefix bug.
func TestCacheAudit_Mutation_Exit1(t *testing.T) {
	reqs := []llm.Request{
		{Messages: []llm.Message{{Role: llm.RoleSystem, Content: "stable prefix"}}},
		{Messages: []llm.Message{{Role: llm.RoleSystem, Content: "stable prefix"}}},
		{Messages: []llm.Message{{Role: llm.RoleSystem, Content: "POISONED prefix"}}},
	}
	var out, errOut bytes.Buffer
	code := reportHashes(reqs, &out, &errOut)
	if code != exitMutation {
		t.Fatalf("reportHashes exit %d, want %d (mutation)", code, exitMutation)
	}
	if !strings.Contains(errOut.String(), "messages[0] mutated at request 3") {
		t.Fatalf("want SC#5 wording 'messages[0] mutated at request 3', got stderr=%q", errOut.String())
	}
}

func TestCacheAudit_ProfileMutationMessages1Only_Exit1(t *testing.T) {
	system := llm.Message{Role: llm.RoleSystem, Content: "stable prefix"}
	skills := "\n\nActive skill instructions (always-on):\n\nfixed skill body"
	profileV1 := profile.RenderContextBlock("# Agent.md\n\n## Facts\n- Name: Cache Audit\n") + skills
	profileV2 := profile.RenderContextBlock("# Agent.md\n\n## Facts\n- Name: Cache Audit\n- Updated preference\n") + skills
	reqs := []llm.Request{
		{Messages: []llm.Message{system, {Role: llm.RoleUser, Content: profileV1}}},
		{Messages: []llm.Message{system, {Role: llm.RoleUser, Content: profileV1}}},
		{Messages: []llm.Message{system, {Role: llm.RoleUser, Content: profileV2}}},
	}

	var out, errOut bytes.Buffer
	code := reportHashes(reqs, &out, &errOut)
	if code != exitMutation {
		t.Fatalf("reportHashes exit %d, want %d (messages[1] mutation)", code, exitMutation)
	}
	if strings.Contains(errOut.String(), "messages[0] mutated") {
		t.Fatalf("profile mutation must leave messages[0] stable, got stderr=%q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "messages[1] profile/skills block mutated at request 3") {
		t.Fatalf("want messages[1] profile drift wording, got stderr=%q", errOut.String())
	}

	requestLines := linesWithPrefix(out.String(), "request ")
	if len(requestLines) != len(reqs) {
		t.Fatalf("want %d messages[0] hash lines, got %d:\n%s", len(reqs), len(requestLines), out.String())
	}
	firstRequestHash := hashOfLine(t, requestLines[0])
	for _, line := range requestLines[1:] {
		if got := hashOfLine(t, line); got != firstRequestHash {
			t.Fatalf("messages[0] hash changed on profile update: %q != %q", got, firstRequestHash)
		}
	}
}

func TestCacheAudit_MissingMessages0_Exit2(t *testing.T) {
	var out, errOut bytes.Buffer
	code := reportHashes([]llm.Request{{}}, &out, &errOut)
	if code != exitFixture {
		t.Fatalf("reportHashes exit %d, want %d (missing messages[0])", code, exitFixture)
	}
	if !strings.Contains(errOut.String(), "missing messages[0]") {
		t.Fatalf("want missing messages[0] diagnostic, got stderr=%q", errOut.String())
	}
}

// TestCacheAudit_CorruptFixture_Exit2 asserts a missing or unparseable fixture is
// exit 2 (fixture corrupt) — never a silent pass.
func TestCacheAudit_CorruptFixture_Exit2(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir() // empty — turn-01.json is absent
		var errOut bytes.Buffer
		if _, code := loadFixtures(dir, &errOut); code != exitFixture {
			t.Fatalf("missing fixture: want exit %d, got %d", exitFixture, code)
		}
	})
	t.Run("malformed-json", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, 1, "{ this is not json")
		var errOut bytes.Buffer
		if _, code := loadFixtures(dir, &errOut); code != exitFixture {
			t.Fatalf("malformed fixture: want exit %d, got %d", exitFixture, code)
		}
	})
	t.Run("empty-user", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, 1, `{"user":"","responses":[{"text":"x"}]}`)
		var errOut bytes.Buffer
		if _, code := loadFixtures(dir, &errOut); code != exitFixture {
			t.Fatalf("empty-user fixture: want exit %d, got %d", exitFixture, code)
		}
	})
	t.Run("empty-response", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, 1, `{"user":"u","responses":[{}]}`)
		var errOut bytes.Buffer
		if _, code := loadFixtures(dir, &errOut); code != exitFixture {
			t.Fatalf("empty-response fixture: want exit %d, got %d", exitFixture, code)
		}
	})
	t.Run("text-and-tool-calls", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, 1, `{"user":"u","responses":[{"text":"x","tool_calls":[{"id":"c1","name":"current_time","arguments":"{}"}]}]}`)
		var errOut bytes.Buffer
		if _, code := loadFixtures(dir, &errOut); code != exitFixture {
			t.Fatalf("text-and-tool-calls fixture: want exit %d, got %d", exitFixture, code)
		}
	})
	t.Run("invalid-tool-call", func(t *testing.T) {
		dir := t.TempDir()
		writeFixture(t, dir, 1, `{"user":"u","responses":[{"tool_calls":[{"id":"c1","name":"current_time","arguments":"not-json"}]}]}`)
		var errOut bytes.Buffer
		if _, code := loadFixtures(dir, &errOut); code != exitFixture {
			t.Fatalf("invalid-tool-call fixture: want exit %d, got %d", exitFixture, code)
		}
	})
}

// TestCacheAudit_FixturesIncludeToolCalls verifies the shipped fixtures exercise
// at least two tool-call turns (a tool round is where a future slice could poison
// the prefix). It reads the real fixtures the audit replays.
func TestCacheAudit_FixturesIncludeToolCalls(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	turns, code := loadFixtures(filepath.Join(root, auditFixtureDir), new(bytes.Buffer))
	if code != 0 {
		t.Fatalf("loadFixtures exit %d", code)
	}
	toolRounds := 0
	for _, ft := range turns {
		for _, r := range ft.Responses {
			if len(r.ToolCalls) > 0 {
				toolRounds++
			}
		}
	}
	if toolRounds < 2 {
		t.Fatalf("want >=2 tool-call rounds in fixtures, got %d", toolRounds)
	}
}

func cacheFixtureRequestCount(t *testing.T) int {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	turns, code := loadFixtures(filepath.Join(root, auditFixtureDir), new(bytes.Buffer))
	if code != 0 {
		t.Fatalf("loadFixtures exit %d", code)
	}
	return expectedAuditRequests(turns)
}

func writeFixture(t *testing.T, dir string, n int, content string) {
	t.Helper()
	path := filepath.Join(dir, "turn-01.json")
	if n != 1 {
		path = filepath.Join(dir, "turn-"+twoDigit(n)+".json")
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

func hashOfLine(t *testing.T, line string) string {
	t.Helper()
	_, hash, ok := strings.Cut(line, ": ")
	if !ok {
		t.Fatalf("malformed request line: %q", line)
	}
	return hash
}
