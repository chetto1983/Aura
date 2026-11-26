package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	artifacts := t.TempDir()
	if err := os.MkdirAll(filepath.Join(artifacts, "test-ledger-unit-test"), 0o755); err != nil {
		t.Fatal(err)
	}
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	needs := `{"unit-test":{"result":"success"},"web-e2e":{"result":"failure"}}`
	args := []string{"check", "--artifacts", artifacts, "--workspace", "../..", "--needs", needs}

	var out, errs bytes.Buffer
	if code := run(append(args, "--report-only"), &out, &errs); code != 0 {
		t.Fatalf("report-only exits 0 on a red ledger, got %d: %s", code, errs.String())
	}
	for _, want := range []string{"## Test ledger (report-only)", "`job` web-e2e: ended failure", "`job` web-e2e: uploaded no test-ledger artifact"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the summary lacks %q", want)
		}
	}
	written, err := os.ReadFile(summary)
	if err != nil || !strings.Contains(string(written), "## Test ledger") {
		t.Errorf("$GITHUB_STEP_SUMMARY: %v, %q", err, written)
	}
	if code := run(args, &out, &errs); code != 1 {
		t.Errorf("an enforcing run exits 1 on a red ledger, got %d", code)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no subcommand", nil},
		{"an unknown subcommand", []string{"verify"}},
		{"no --artifacts", []string{"check"}},
		{"a missing artifacts directory", []string{"check", "--artifacts", filepath.Join(artifacts, "missing"), "--workspace", "../.."}},
		{"a missing allowlist", []string{"check", "--artifacts", artifacts, "--workspace", "../..", "--allow", "no/such/file"}},
		{"malformed needs", []string{"check", "--artifacts", artifacts, "--workspace", "../..", "--needs", "{"}},
		{"an unknown flag", []string{"check", "--unknown-flag"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errs bytes.Buffer
			if code := run(tc.args, &out, &errs); code != 2 {
				t.Errorf("%v: want exit 2, got %d", tc.args, code)
			}
		})
	}
}
