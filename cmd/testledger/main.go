// Command testledger judges a CI run by what its test jobs executed
// (docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.3):
//
//	testledger check --artifacts DIR [--allow FILE] [--needs JSON] [--workspace DIR] [--report-only]
//
// DIR holds the downloaded test-ledger-<job> artifacts and JSON is the workflow's toJSON(needs).
// The Markdown verdict goes to stdout and, under GitHub Actions, to $GITHUB_STEP_SUMMARY. The exit
// status is 1 when the ledger is red, unless --report-only, and 2 when it cannot be judged at all.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/testledger"
)

// summaryViolations keeps the step summary well under GitHub's 1 MiB cap; the log has them all.
const summaryViolations = 500

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		_, _ = fmt.Fprintln(stderr, "usage: testledger check --artifacts DIR [--allow FILE] [--needs JSON] [--workspace DIR] [--report-only]")
		return 2
	}
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	artifacts := flags.String("artifacts", "", "the directory holding the test-ledger-<job> artifacts")
	allow := flags.String("allow", ".github/test_ledger_allow.txt", "the allowlist")
	needs := flags.String("needs", "", "the workflow's toJSON(needs): the jobs the gate waits for")
	workspace := flags.String("workspace", ".", "the repository root the test jobs ran in")
	reportOnly := flags.Bool("report-only", false, "report violations without failing")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	report, err := judge(*artifacts, *allow, *needs, *workspace)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "testledger:", err)
		return 2
	}
	_, _ = fmt.Fprint(stdout, testledger.Summary(report, *reportOnly, 0))
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		if err := appendFile(path, testledger.Summary(report, *reportOnly, summaryViolations)); err != nil {
			_, _ = fmt.Fprintln(stderr, "testledger:", err)
			return 2
		}
	}
	if report.Failed() && !*reportOnly {
		return 1
	}
	return 0
}

func judge(artifacts, allowPath, needs, workspace string) (testledger.Report, error) {
	if artifacts == "" {
		return testledger.Report{}, errors.New("--artifacts is required")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return testledger.Report{}, err
	}
	// The cheap inputs first: reading the tree takes seconds, a bad flag should not.
	arts, err := testledger.LoadArtifacts(artifacts, root)
	if err != nil {
		return testledger.Report{}, err
	}
	entries, err := readAllow(filepath.Join(root, allowPath))
	if err != nil {
		return testledger.Report{}, err
	}
	var jobs []testledger.Job
	if needs != "" {
		if jobs, err = testledger.JobsFromNeeds([]byte(needs), arts.Arrived); err != nil {
			return testledger.Report{}, err
		}
	}
	declared, err := testledger.DeclareTree(root)
	if err != nil {
		return testledger.Report{}, err
	}
	declared.Units = append(declared.Units, arts.Pytest...)
	return testledger.Check(declared, arts.Results, entries, jobs), nil
}

func readAllow(path string) ([]testledger.Entry, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the allowlist path is the gate's own flag, read in its checkout.
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return testledger.ParseAllow(f)
}

func appendFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: $GITHUB_STEP_SUMMARY, set by the runner.
	if err != nil {
		return err
	}
	_, err = f.WriteString(text)
	return errors.Join(err, f.Close())
}
