//go:build ignore

// musr_live_run_assert.go is the blocking machine-checkable half of the 01-06 two-identity
// live run (D-18): scripts/musr_live_run.sh invokes it as the final step and propagates its
// exit status, so THIS is what decides whether the phase closes — the rubric written in
// docs/runbooks/two-identity-live-run.md is recorded as phase evidence and gates nothing
// (internal/agenteval/case.go's position: a gate that needs a model to decide whether it
// passed cannot be trusted to gate the model).
//
// STUB — RED phase (01-06 Task 2). Flags parse and fixtures resolve, but no assertion runs
// yet: every invocation exits 0. scripts/musr_live_run_assert_test.go's leaking/empty/
// no-overlap subtests are therefore failing against this file, which is the point — the next
// commit replaces this stub with the real assertion logic (GREEN).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	fixture := flag.String("fixture", "", "run against a committed fixture under scripts/testdata/musr_live_run/<name> instead of --transcripts")
	transcripts := flag.String("transcripts", "artifacts/musr-live-run", "directory holding transcript-a.jsonl, transcript-b.jsonl, timings.jsonl")
	flag.Parse()

	dir := *transcripts
	if *fixture != "" {
		dir = filepath.Join("scripts", "testdata", "musr_live_run", *fixture)
	}
	fmt.Fprintf(os.Stderr, "STUB: would assert transcripts under %s (not implemented yet)\n", dir)
	os.Exit(0)
}
