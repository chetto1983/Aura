package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
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

// TestCacheAudit_AllEqual_Exit0 is the SC#1 positive proof: the real 20-turn
// replay prints exactly 20 `turn NN: <hex>` lines, all identical, and exits 0 —
// runtime-faithful (the real Runner.Turn loop), Postgres-free.
func TestCacheAudit_AllEqual_Exit0(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cacheAuditMain(context.Background(), nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("cacheAuditMain exit %d, want 0 (stderr=%q)", code, errOut.String())
	}
	lines := nonEmptyLines(out.String())
	if len(lines) != auditTurns {
		t.Fatalf("want exactly %d hash lines, got %d:\n%s", auditTurns, len(lines), out.String())
	}
	first := hashOfLine(t, lines[0])
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "turn ") {
			t.Fatalf("line %d not a turn line: %q", i+1, ln)
		}
		if h := hashOfLine(t, ln); h != first {
			t.Fatalf("line %d hash %q != first %q (a drift the gate must catch)", i+1, h, first)
		}
	}
}

// TestCacheAudit_Mutation_Exit1 is the SC#5 NEGATIVE proof: when messages[0]
// drifts between turns the audit exits 1 with the explicit `messages[0] mutated
// at turn N` wording. It drives the reportHashes seam directly with a poisoned
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
	if !strings.Contains(errOut.String(), "messages[0] mutated at turn 3") {
		t.Fatalf("want SC#5 wording 'messages[0] mutated at turn 3', got stderr=%q", errOut.String())
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
		t.Fatalf("malformed turn line: %q", line)
	}
	return hash
}
