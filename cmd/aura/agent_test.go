package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// uuidV7Re is the SC#4 UUIDv7 acceptance regex (version nibble 7, variant 8-b).
var uuidV7Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// decodeLines splits dry-run stdout into one decoded Event-shaped map per line.
func decodeLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	evs := make([]map[string]any, 0, len(lines))
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline=%q", i, err, ln)
		}
		evs = append(evs, m)
	}
	return evs
}

func TestDryRun_RequestIDAuto_IsValidUUIDv7_AndStable(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "5")
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "auto", maxSteps: -1, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
	evs := decodeLines(t, buf.String())
	if len(evs) == 0 {
		t.Fatal("expected at least one Event line")
	}
	var first string
	for i, ev := range evs {
		id, _ := ev["request_id"].(string)
		if !uuidV7Re.MatchString(id) {
			t.Fatalf("line %d request_id %q is not a valid UUIDv7", i, id)
		}
		if i == 0 {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("line %d request_id %q != first %q (all lines must share the run id)", i, id, first)
		}
	}
}

func TestDryRun_RequestIDVerbatim_Reproducible(t *testing.T) {
	const fixed = "0192f000-0000-7000-8000-000000000001"
	t.Setenv("AURA_LOOP_MAX_STEPS", "5")
	run := func() []map[string]any {
		var buf bytes.Buffer
		cfg := dryRunConfig{requestID: fixed, maxSteps: -1, maxWallclockSec: -1, dedupWindow: -1}
		if err := dryRun(cfg, &buf); err != nil {
			t.Fatalf("dryRun: %v", err)
		}
		return decodeLines(t, buf.String())
	}
	a, b := run(), run()
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("line counts differ or empty: %d vs %d", len(a), len(b))
	}
	for i := range a {
		ida, _ := a[i]["request_id"].(string)
		idb, _ := b[i]["request_id"].(string)
		if ida != fixed || idb != fixed {
			t.Fatalf("line %d request_id not verbatim: run1=%q run2=%q want=%q", i, ida, idb, fixed)
		}
	}
}

func TestDryRun_MaxStepsOverride_Yields26Lines_LimitMaxSteps(t *testing.T) {
	// CLI flag (non -1) overrides env/default (D-06). 25 steps → 25 step + 1 terminal.
	t.Setenv("AURA_LOOP_MAX_STEPS", "9") // must be overridden by the CLI flag below
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "auto", maxSteps: 25, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
	evs := decodeLines(t, buf.String())
	if len(evs) != 26 {
		t.Fatalf("want 26 lines (25 step + 1 terminal), got %d", len(evs))
	}
	final := evs[len(evs)-1]
	actions, _ := final["actions"].(map[string]any)
	sd, _ := actions["state_delta"].(map[string]any)
	if sd["limit_hit"] != "max_steps" {
		t.Fatalf("final limit_hit: want max_steps, got %v", sd["limit_hit"])
	}
	if sd["termination_reason"] != "budget_exhausted" {
		t.Fatalf("final termination_reason: want budget_exhausted, got %v", sd["termination_reason"])
	}
}

func TestDryRun_InvalidRequestID_Errors(t *testing.T) {
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "not-a-uuid", maxSteps: -1, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err == nil {
		t.Fatal("expected an error for a malformed --request-id")
	}
}

func TestParseDryRunArgs_Defaults_SentinelMinusOne(t *testing.T) {
	cfg, err := parseDryRunArgs(nil)
	if err != nil {
		t.Fatalf("parseDryRunArgs: %v", err)
	}
	if cfg.maxSteps != -1 || cfg.maxWallclockSec != -1 || cfg.dedupWindow != -1 {
		t.Fatalf("unset numeric flags must default to -1 sentinel (D-06), got %+v", cfg)
	}
	if cfg.requestID != "auto" {
		t.Fatalf("default --request-id must be auto, got %q", cfg.requestID)
	}
}

func TestParseDryRunArgs_Overrides(t *testing.T) {
	cfg, err := parseDryRunArgs([]string{"--request-id", "abc", "--max-steps", "7", "--max-wallclock-sec", "11", "--dedup-window", "4"})
	if err != nil {
		t.Fatalf("parseDryRunArgs: %v", err)
	}
	if cfg.requestID != "abc" || cfg.maxSteps != 7 || cfg.maxWallclockSec != 11 || cfg.dedupWindow != 4 {
		t.Fatalf("flag parse mismatch: %+v", cfg)
	}
}
