//go:build ignore

// musr_live_run_assert_test.go is the RED-phase record for scripts/musr_live_run_assert.go
// (01-06 Task 2, tdd="true"). It black-box-tests the sibling script by shelling out to `go
// run` against the committed fixtures under scripts/testdata/musr_live_run/, exactly the way
// Task 2's own <verify> commands do. It carries //go:build ignore, matching every other
// scripts/*.go file (authula_seed_e2e.go's convention) and keeping `go list ./scripts/...`
// empty (CLAUDE.md: no new package).
//
// MEASURED (2026-09-08): `go test -tags=ignore <these two files>` does NOT work — forcing
// the "ignore" build tag globally also pulls in GOROOT's own generator scripts (mkduff.go,
// gengoarch.go, mklockrank.go, …), which carry the identical `//go:build ignore` constraint
// for an unrelated reason (they are `go generate` producers, excluded from normal builds the
// same way), and those collide into `import cycle not allowed`. That is a toolchain property
// of the shared tag name, not a defect in either file here — "ignore" is the wrong tag to force
// globally. This file is therefore NOT run via `go test`; it is the committed historical record
// of the RED phase. The actual RED/GREEN transition was verified by running the four failing
// fixtures directly against the stub, exactly as each subtest below does:
//
//	go run ./scripts/musr_live_run_assert.go --fixture leaking    # stub: wrongly exit 0
//	go run ./scripts/musr_live_run_assert.go --fixture empty      # stub: wrongly exit 0
//	go run ./scripts/musr_live_run_assert.go --fixture no-overlap # stub: wrongly exit 0
//
// Written BEFORE scripts/musr_live_run_assert.go had its assertion logic: at that point every
// one of those three commands wrongly exited 0 (the stub asserts nothing). That is the RED
// this file records; scripts/musr_live_run_assert.go's implementation commit is the GREEN,
// verified by re-running the identical commands and observing non-zero exits naming the
// right assertion.
package main

import (
	"os/exec"
	"strings"
	"testing"
)

func runFixture(t *testing.T, fixture string) (string, int) {
	t.Helper()
	cmd := exec.Command("go", "run", "./scripts/musr_live_run_assert.go", "--fixture", fixture)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("fixture %s: failed to run: %v (output: %s)", fixture, err, out)
		}
	}
	return string(out), code
}

func TestCleanFixturePasses(t *testing.T) {
	out, code := runFixture(t, "clean")
	if code != 0 {
		t.Fatalf("clean fixture: want exit 0, got %d — output:\n%s", code, out)
	}
}

func TestCleanSwappedFixturePasses(t *testing.T) {
	// Order independence (D-18 must_haves): the assertions do not depend on which
	// conversation completes first. clean-swapped has B finish before A; clean has A finish
	// before B. Both must pass.
	out, code := runFixture(t, "clean-swapped")
	if code != 0 {
		t.Fatalf("clean-swapped fixture: want exit 0, got %d — output:\n%s", code, out)
	}
}

func TestLeakingFixtureFailsNamingCrossRead(t *testing.T) {
	out, code := runFixture(t, "leaking")
	if code == 0 {
		t.Fatalf("leaking fixture: want non-zero exit, got 0 — an assertion set that cannot " +
			"detect a planted cross-read is a gate that would pass a real leak")
	}
	if !strings.Contains(out, "MUSR-A-1a2b3c4d") || !strings.Contains(strings.ToLower(out), "cross") {
		t.Fatalf("leaking fixture: want the failure to name the cross-read assertion and the "+
			"offending token — output:\n%s", out)
	}
}

func TestEmptyTranscriptFixtureFailsNamingCompletion(t *testing.T) {
	out, code := runFixture(t, "empty")
	if code == 0 {
		t.Fatalf("empty fixture: want non-zero exit, got 0")
	}
	if !strings.Contains(strings.ToLower(out), "completion") && !strings.Contains(strings.ToLower(out), "empty") {
		t.Fatalf("empty fixture: want the failure to name the completion assertion, not score "+
			"low — output:\n%s", out)
	}
}

func TestNoOverlapFixtureFailsNamingOverlap(t *testing.T) {
	out, code := runFixture(t, "no-overlap")
	if code == 0 {
		t.Fatalf("no-overlap fixture: want non-zero exit, got 0")
	}
	if !strings.Contains(strings.ToLower(out), "overlap") {
		t.Fatalf("no-overlap fixture: want the failure to name the overlap assertion — output:\n%s", out)
	}
}
