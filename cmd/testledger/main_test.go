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
	for _, bad := range [][]string{
		nil,
		{"verify"},
		{"check"},
		{"check", "--artifacts", filepath.Join(artifacts, "missing"), "--workspace", "../.."},
		{"check", "--artifacts", artifacts, "--workspace", "../..", "--allow", "no/such/file"},
		{"check", "--artifacts", artifacts, "--workspace", "../..", "--needs", "{"},
		{"check", "--unknown-flag"},
	} {
		if code := run(bad, &out, &errs); code != 2 {
			t.Errorf("%v: want exit 2, got %d", bad, code)
		}
	}
}
