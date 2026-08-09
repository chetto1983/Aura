// Tests for the two ways LedgerAdapter can fail to answer, both of which run on the
// turn's own goroutine at a voluntary termination: it must not block there, and it must
// not turn its own failure into a demand for proof.
//
// No Postgres here on purpose. The error path is reached through identityUUID, which
// rejects a non-UUID identity BEFORE the store ever touches its pool, so the whole
// failure contract is exercised with no database and no build tag.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// unreadableAdapter is a real LedgerAdapter over a real project directory whose ledger
// read always fails: the identity is not a UUID.
func unreadableAdapter(t *testing.T) (LedgerAdapter, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return LedgerAdapter{Store: &EvidenceStore{}, IdentityID: "not-a-uuid"}, root
}

func TestVerificationReadIsBounded(t *testing.T) {
	// context.Background() would let a stalled Postgres or an exhausted pool hold the
	// turn open forever -- the wedge the gate is required never to cause.
	ctx, cancel := verificationReadContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the ledger read context carries no deadline: an unresponsive pool would wedge the turn")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > verificationReadTimeout {
		t.Fatalf("deadline in %s, want (0, %s]", remaining, verificationReadTimeout)
	}
}

func TestUnreadableLedgerSaysNothingRatherThanUnverified(t *testing.T) {
	adapter, root := unreadableAdapter(t)

	// The project IS recognised, so the answer comes from the failed read, not from the
	// detector declining.
	if facts := adapter.ProjectFactsFor(root); !facts.Found {
		t.Fatalf("test setup: %q must be detected as a project", root)
	}
	status := adapter.VerificationStatusFor("s1", root)
	if status.Status != StatusNotApplicable {
		t.Fatalf("status = %q, want %q: an outage must not be reported as a workspace needing proof",
			status.Status, StatusNotApplicable)
	}
}

func TestUnreadableLedgerDoesNotNudge(t *testing.T) {
	// The end of the same chain: a database the gate could not ask must produce silence,
	// not verificationMaxAttempts nudges per turn for the whole duration of the outage.
	adapter, root := unreadableAdapter(t)

	if _, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: adapter, SessionID: "s1",
		ChangedPaths: []string{filepath.Join(root, "app.go")},
		MaxAttempts:  verificationMaxAttempts,
	}); ok {
		t.Fatal("an unreadable ledger must not nudge: nothing was learned about this workspace")
	}
}

func TestUnreadableWorkspaceIsSearchedPast(t *testing.T) {
	// Two edited projects, the first unreadable: the nudge must still be about the second,
	// which genuinely has no evidence. A status that says nothing must not stop the search
	// any more than "passed" does.
	unreadable, unreadableRoot := unreadableAdapter(t)
	pending := t.TempDir()
	ledger := &splitLedger{
		unreadable:     unreadable,
		unreadableRoot: unreadableRoot,
		pendingRoot:    pending,
	}

	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{
			filepath.Join(unreadableRoot, "app.go"),
			filepath.Join(pending, "app.go"),
		},
		MaxAttempts: verificationMaxAttempts,
	})

	if !ok {
		t.Fatal("the second workspace still needs proof, so the turn must nudge")
	}
	if !strings.Contains(nudge, pending) {
		t.Fatalf("nudge must be about the workspace that needs proof, got: %s", nudge)
	}
}

// splitLedger answers for two roots: one through the real unreadable adapter, one as a
// recognised project with no evidence.
type splitLedger struct {
	unreadable                  LedgerAdapter
	unreadableRoot, pendingRoot string
}

func (l *splitLedger) ProjectFactsFor(cwd string) ProjectFacts {
	switch cwd {
	case l.unreadableRoot:
		return l.unreadable.ProjectFactsFor(cwd)
	case l.pendingRoot:
		return ProjectFacts{Found: true, Root: cwd, VerifyCommands: []string{"go test ./..."}}
	}
	return ProjectFacts{}
}

func (l *splitLedger) VerificationStatusFor(sessionID, cwd string) VerificationStatus {
	if cwd == l.unreadableRoot {
		return l.unreadable.VerificationStatusFor(sessionID, cwd)
	}
	return VerificationStatus{Status: StatusUnverified}
}
