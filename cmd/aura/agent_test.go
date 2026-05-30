package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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

func TestParseDryRunArgs_BadFlag_Errors(t *testing.T) {
	if _, err := parseDryRunArgs([]string{"--max-steps", "not-an-int"}); err == nil {
		t.Fatal("expected an error for a non-integer --max-steps")
	}
}

func TestDryRun_CLIMaxSteps_OverridesEnv_D06(t *testing.T) {
	// CLI flag (3) must win over the env (9): 3 step + 1 terminal = 4 lines (D-06).
	t.Setenv("AURA_LOOP_MAX_STEPS", "9")
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "auto", maxSteps: 3, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
	if got := len(decodeLines(t, buf.String())); got != 4 {
		t.Fatalf("CLI --max-steps=3 must override env=9: want 4 lines, got %d", got)
	}
}

func TestDryRun_EnvMaxSteps_WhenFlagUnset_D06(t *testing.T) {
	// Flag unset (-1) → env (7) wins over the builtin default (D-06): 7 + 1 = 8 lines.
	t.Setenv("AURA_LOOP_MAX_STEPS", "7")
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "auto", maxSteps: -1, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
	if got := len(decodeLines(t, buf.String())); got != 8 {
		t.Fatalf("unset flag must fall through to env=7: want 8 lines, got %d", got)
	}
}

func TestDryRun_PreservesOperatorExemptTools(t *testing.T) {
	// An operator-set exemption must survive: the dry-run tool is added to the env
	// allowlist, not replacing it. After WR-04 the process env is never mutated, so
	// the var is unchanged by construction; the merge happens in
	// agent.ExemptToolsFromEnv (asserted directly in agent's own tests). Here we
	// confirm (a) the run still completes on the hard step cap with an operator
	// exemption present, and (b) dryRun leaves the env untouched.
	t.Setenv("AURA_LOOP_MAX_STEPS", "3")
	t.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", "search")
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "auto", maxSteps: -1, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
	if got := len(decodeLines(t, buf.String())); got != 4 {
		t.Fatalf("max_steps=3 with the noop tool exempt → 4 lines (3 step + 1 terminal), got %d", got)
	}
	if got := os.Getenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS"); got != "search" {
		t.Fatalf("dryRun must NOT mutate the process env (WR-04), got %q", got)
	}
}

func TestDryRun_MalformedEnv_FailsFast(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "garbage")
	var buf bytes.Buffer
	cfg := dryRunConfig{requestID: "auto", maxSteps: -1, maxWallclockSec: -1, dedupWindow: -1}
	if err := dryRun(cfg, &buf); err == nil {
		t.Fatal("expected NewBudgetFromEnv to fail-fast on a malformed AURA_LOOP_MAX_STEPS")
	}
}

func TestRunAgent_DryRun_HappyPath_WritesToStdout(t *testing.T) {
	// runAgent's success path returns normally (it only os.Exits on error). Capture
	// os.Stdout to confirm it emits Event lines through the dispatcher.
	t.Setenv("AURA_LOOP_MAX_STEPS", "2")
	out := captureStdout(t, func() { runAgent([]string{"dry-run", "--max-steps", "2"}) })
	if got := len(decodeLines(t, out)); got != 3 {
		t.Fatalf("runAgent dry-run --max-steps 2: want 3 lines, got %d", got)
	}
}

// TestRunAgent_ExitPaths drives the os.Exit(1) branches via a re-exec subprocess
// so the dispatcher's usage/error handling is covered without killing the test
// process.
func TestRunAgent_ExitPaths(t *testing.T) {
	if os.Getenv("AURA_TEST_RUNAGENT_EXIT") != "" {
		runAgent(strings.Split(os.Getenv("AURA_TEST_RUNAGENT_ARGS"), "|"))
		return
	}
	cases := map[string]string{
		"no-sub":      "",
		"bad-sub":     "frobnicate",
		"bad-flag":    "dry-run|--max-steps|nope",
		"bad-request": "dry-run|--request-id|not-a-uuid",
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run", "TestRunAgent_ExitPaths") //nolint:gosec // re-exec self
			cmd.Env = append(os.Environ(), "AURA_TEST_RUNAGENT_EXIT=1", "AURA_TEST_RUNAGENT_ARGS="+args)
			err := cmd.Run()
			if err == nil {
				t.Fatalf("expected a non-zero exit for args %q", args)
			}
		})
	}
}
