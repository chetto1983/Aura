package knowledge

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/boundedbuffer"
)

// TestKnowledgeHelperProcess is not a real test: the hang test re-execs the test binary
// into it as a killable child that blocks forever without writing stdout, so a parent's
// ReadBytes hangs until the parent kills it. It runs non-verbose (no -test.v), so the
// test framework emits nothing to stdout while it blocks.
func TestKnowledgeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_KNOWLEDGE_HELPER") != "1" {
		return
	}
	select {} //nolint:revive // intentionally block until the parent kills this child
}

// TestCypherHungSubprocessFailsFastNoLeak is the Wave 0.3 regression: a Cypher read
// against a subprocess that never responds must fail within the hang ceiling (killing
// the subprocess) instead of blocking ReadBytes forever under mu, and must not leak the
// reader goroutine (the package's sole goleak TestMain enforces the no-leak half). A
// follow-up call then also fails fast — proving other graph ops proceed rather than
// wedging forever on mu, the whole point of the fix.
func TestCypherHungSubprocessFailsFastNoLeak(t *testing.T) {
	helper := exec.Command(os.Args[0], "-test.run=^TestKnowledgeHelperProcess$") //nolint:gosec // re-exec of our own test binary
	helper.Env = append(os.Environ(), "GO_WANT_KNOWLEDGE_HELPER=1")
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := helper.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Wait() })

	c := &Client{
		cmd:         helper,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdout),
		stderr:      boundedbuffer.New(0),
		callTimeout: 40 * time.Millisecond,
	}

	start := time.Now()
	if _, err := c.Cypher(context.Background(), "RETURN 1", nil, false); err == nil {
		t.Fatal("expected a hang-timeout error from the unresponsive subprocess")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Cypher did not fail fast on the hang: took %v (want << hang ceiling)", elapsed)
	}
	// The subprocess was killed on the hang; a second call must also fail fast (not wedge
	// on mu), proving the shared client no longer deadlocks every subsequent graph op.
	if _, err := c.Cypher(context.Background(), "RETURN 2", nil, false); err == nil {
		t.Fatal("expected the post-kill Cypher call to fail fast, not hang")
	}
}
