package runner

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestEveryAgentConstructionResolvesFromTheTurnIdentity is the assumption-delta
// invariant Task 3 (02-05-PLAN.md) requires: it reads the six named files below
// and asserts the boot-time client fallback pattern this plan closes
// (T-02-08b/D-11/CRED-07 — `client, cfg := rc.Client, rc.LLM` read as a DEFAULT,
// overridden only `if rc.Runtime != nil`, and the composition-root capture that
// fed it, `Client: chat.client`) is absent from every one of them.
//
// The files are named as an explicit list, not discovered by a directory walk:
// an explicit list fails LOUDLY when a file is renamed or a seventh
// agent-construction site is added, whereas a walk would silently stop covering
// the missing one — the whole point of an invariant test is that a reviewer
// adding a new construction site sees this list and has to extend it, rather
// than the test quietly widening its own coverage. If this test goes red, do
// NOT weaken the pattern to make it pass — either the site the pattern found is
// a real regression of the closed fallback, or the file legitimately needs a
// new line added to workerLLMFallbackFiles/compositionRootCaptureFiles below.
func TestEveryAgentConstructionResolvesFromTheTurnIdentity(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootForTest(t)

	fallbackPattern := regexp.MustCompile(`client,\s*cfg\s*:=\s*(rc|deps)\.(Client|LLM)`)
	workerLLMFallbackFiles := []string{
		filepath.Join("internal", "swarm", "swarm.go"),
		filepath.Join("internal", "swarm", "delegation_run.go"),
		filepath.Join("internal", "cron", "handlers", "handler.go"),
		filepath.Join("internal", "cron", "handlers", "agentjob.go"),
	}
	for _, rel := range workerLLMFallbackFiles {
		src := readSourceForTest(t, repoRoot, rel)
		if fallbackPattern.Match(src) {
			t.Errorf("%s still contains the boot-time client fallback pattern `client, cfg := rc.Client, rc.LLM` / `client, cfg := deps.Client, deps.LLM` — T-02-08b's fail-open path has returned", rel)
		}
	}

	// The seventh construction site is the INTERACTIVE runner itself, and its
	// regression shape is different from the six above: there is no `rc.Client` to
	// read, only turnLocked quietly reverting to r.llmSnapshot — which falls through
	// to the process-wide deployment snapshot. So this leg asserts the POSITIVE
	// invariant instead of the absence of a pattern. buildAgent's own r.llmSnapshot
	// call is legitimate and deliberately not forbidden here: turnLocked has already
	// seeded the resolved snapshot onto ctx by then, and every reader below inherits
	// that one decision rather than taking its own.
	turnSeam := filepath.Join("internal", "runner", "runner.go")
	if !regexp.MustCompile(`r\.turnLLMSnapshot\(ctx\)`).Match(readSourceForTest(t, repoRoot, turnSeam)) {
		t.Errorf("%s no longer resolves the turn snapshot through turnLLMSnapshot — the interactive turn has fallen back to the process-wide deployment client (CRED-07/D-11)", turnSeam)
	}

	capturePattern := regexp.MustCompile(`Client:\s*chat\.client`)
	compositionRootCaptureFiles := []string{
		filepath.Join("cmd", "aura", "serve_delegation.go"),
		filepath.Join("cmd", "aura", "serve_dispatch.go"),
	}
	for _, rel := range compositionRootCaptureFiles {
		src := readSourceForTest(t, repoRoot, rel)
		if capturePattern.Match(src) {
			t.Errorf("%s still writes chat.client into a RunConfig/AgentDeps template — the boot-time deployment-key capture T-02-08b closes has returned", rel)
		}
	}
}

// repoRootForTest resolves the repository root from this test file's own
// absolute path (internal/runner/agent_construction_invariant_test.go is
// exactly two directories below the root), so the test works regardless of
// `go test`'s working directory.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve this test file's own path via runtime.Caller")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func readSourceForTest(t *testing.T, repoRoot, rel string) []byte {
	t.Helper()
	path := filepath.Join(repoRoot, rel)
	src, err := os.ReadFile(path) //nolint:gosec // fixed in-repo source path, test-only
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return src
}
