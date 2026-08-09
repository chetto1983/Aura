// Ported from NousResearch/hermes-agent `tests/agent/test_verification_stop.py`
// (MIT, commit 9d4ef04ed), case for case. The ledger those tests drive through
// record_terminal_result/mark_workspace_edited is unported, so it is a fake here
// -- which is what the VerificationLedger interface exists for.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLedger answers for the workspaces it was given and reports every other
// directory as "not a project", which is how the real detector behaves outside a
// recognised root.
type fakeLedger struct {
	facts    map[string]ProjectFacts
	statuses map[string]VerificationStatus
	asked    []string
}

func (f *fakeLedger) ProjectFactsFor(cwd string) ProjectFacts {
	f.asked = append(f.asked, cwd)
	return f.facts[cwd]
}

func (f *fakeLedger) VerificationStatusFor(_, cwd string) VerificationStatus {
	if status, ok := f.statuses[cwd]; ok {
		return status
	}
	return VerificationStatus{Status: "unverified"}
}

func nodeProject(commands ...string) ProjectFacts {
	return ProjectFacts{Found: true, VerifyCommands: commands}
}

func TestVerifyOnStopEnvCanEnable(t *testing.T) {
	// Env "1" forces ON regardless of the configured value.
	t.Setenv("AURA_AGENT_VERIFY_ON_STOP_ENABLED", "1")
	if !verifyOnStopEnabled("off") {
		t.Fatal("env must win over the configured value")
	}
}

func TestVerifyOnStopEnvCanDisable(t *testing.T) {
	t.Setenv("AURA_AGENT_VERIFY_ON_STOP_ENABLED", "false")
	if verifyOnStopEnabled("on") {
		t.Fatal("env must win over the configured value")
	}
}

func TestVerifyOnStopAutoIsOnForEverySurface(t *testing.T) {
	// "auto" resolves ON everywhere, because Aura's agent has no delivery surface to
	// be aware of: Telegram is a wrapper over the same AG-UI fanout, not a channel the
	// agent behaves differently for. This pins the decision, not a waiting room --
	// making the gate surface-dependent would be two agents wearing one name, and the
	// operator's off-switch is AURA_AGENT_VERIFY_ON_STOP_ENABLED.
	for _, configured := range []string{"", "auto", "unrecognised"} {
		if !verifyOnStopEnabled(configured) {
			t.Fatalf("configured %q must resolve ON: the gate does not vary by surface", configured)
		}
	}
}

func TestVerifyOnStopExplicitConfigWins(t *testing.T) {
	os.Unsetenv("AURA_AGENT_VERIFY_ON_STOP_ENABLED")
	if verifyOnStopEnabled("off") {
		t.Fatal("an explicit off must disable the gate")
	}
	if !verifyOnStopEnabled("yes") {
		t.Fatal("an explicit on must enable the gate")
	}
}

func TestNudgeChecksAllEditedWorkspaces(t *testing.T) {
	// Two edited projects: the first already passed, the second has not. The
	// nudge must be about the one that still needs proof, not the first seen.
	passed := filepath.Join("workspace", "a", "src")
	pending := filepath.Join("workspace", "b", "src")
	ledger := &fakeLedger{
		facts: map[string]ProjectFacts{
			passed:  nodeProject("pnpm test"),
			pending: nodeProject("pnpm lint"),
		},
		statuses: map[string]VerificationStatus{
			passed:  {Status: "passed"},
			pending: {Status: "unverified"},
		},
	}

	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger:    ledger,
		SessionID: "s1",
		ChangedPaths: []string{
			filepath.Join(passed, "app.ts"),
			filepath.Join(pending, "app.ts"),
		},
		MaxAttempts: 2,
	})

	if !ok {
		t.Fatal("a workspace without fresh evidence must nudge")
	}
	if !strings.Contains(nudge, "fresh passing verification evidence") {
		t.Fatalf("nudge does not state the gate's reason: %s", nudge)
	}
	if !strings.Contains(nudge, "pnpm lint") {
		t.Fatalf("nudge must name the PENDING workspace's command, got: %s", nudge)
	}
}

func TestPassedWorkspaceDoesNotNudge(t *testing.T) {
	cwd := filepath.Join("workspace", "src")
	ledger := &fakeLedger{
		facts:    map[string]ProjectFacts{cwd: nodeProject("go test ./...")},
		statuses: map[string]VerificationStatus{cwd: {Status: "passed"}},
	}

	if _, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{filepath.Join(cwd, "app.go")}, MaxAttempts: 2,
	}); ok {
		t.Fatal("a passing workspace must satisfy the gate")
	}
}

func TestNoSuiteNudgeUsesCanonicalTempDir(t *testing.T) {
	// A symlinked temp directory named in the nudge would send the agent's later
	// file operations somewhere other than where it wrote, so the nudge must name
	// the resolved path.
	real := t.TempDir()
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("resolve real temp dir: %v", err)
	}

	cwd := filepath.Join("workspace", "src")
	ledger := &fakeLedger{
		// Found, but with NO canonical command -- the branch that asks for a script.
		facts: map[string]ProjectFacts{cwd: {Found: true}},
	}

	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{filepath.Join(cwd, "app.go")},
		MaxAttempts:  2, TempDir: linked,
	})

	if !ok {
		t.Fatal("a project with no canonical command must still nudge")
	}
	if !strings.Contains(nudge, resolvedReal) {
		t.Fatalf("nudge must name the resolved temp dir %q, got: %s", resolvedReal, nudge)
	}
	if strings.Contains(nudge, linked) {
		t.Fatalf("nudge must not name the symlink %q, got: %s", linked, nudge)
	}
}

func TestNudgeAttemptsAreBounded(t *testing.T) {
	cwd := filepath.Join("workspace", "src")
	ledger := &fakeLedger{facts: map[string]ProjectFacts{cwd: nodeProject("go test ./...")}}

	if _, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{filepath.Join(cwd, "app.go")},
		Attempts:     2, MaxAttempts: 2,
	}); ok {
		t.Fatal("the attempt budget must bound the gate")
	}
}

func TestUnknownProjectDoesNotNudge(t *testing.T) {
	// The detector recognises nothing, so there is no project to verify.
	if _, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: &fakeLedger{}, SessionID: "s1",
		ChangedPaths: []string{filepath.Join("elsewhere", "app.go")}, MaxAttempts: 2,
	}); ok {
		t.Fatal("a directory the detector does not know must not nudge")
	}
}

func TestNilLedgerDoesNotNudge(t *testing.T) {
	// The Go equivalent of the original's ImportError guard: no ledger, no policy.
	if _, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		SessionID: "s1", ChangedPaths: []string{filepath.Join("w", "app.go")}, MaxAttempts: 2,
	}); ok {
		t.Fatal("a nil ledger must disable the gate, not panic or nudge")
	}
}

func TestDocOnlyEditDoesNotNudge(t *testing.T) {
	// Fix "C" in the original: a SKILL.md or README.md edit must never demand a
	// verification script, even on an unverified workspace.
	cwd := "docs"
	ledger := &fakeLedger{facts: map[string]ProjectFacts{cwd: nodeProject("go test ./...")}}

	if _, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{
			filepath.Join(cwd, "SKILL.md"),
			filepath.Join(cwd, "notes.txt"),
			filepath.Join(cwd, "LICENSE"),
		},
		MaxAttempts: 2,
	}); ok {
		t.Fatal("a documentation-only turn has nothing to verify")
	}
}

func TestMixedDocAndCodeEditStillNudges(t *testing.T) {
	cwd := "src"
	ledger := &fakeLedger{facts: map[string]ProjectFacts{cwd: nodeProject("go test ./...")}}
	doc := filepath.Join(cwd, "README.md")
	code := filepath.Join(cwd, "app.go")

	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{doc, code}, MaxAttempts: 2,
	})

	if !ok {
		t.Fatal("a turn that touched code must nudge")
	}
	// The doc path is filtered out of the reported set; the code path remains.
	if !strings.Contains(nudge, code) {
		t.Fatalf("nudge must report the code path, got: %s", nudge)
	}
	if strings.Contains(nudge, doc) {
		t.Fatalf("nudge must not report the doc path, got: %s", nudge)
	}
}

func TestIsNonCodePathClassification(t *testing.T) {
	// The original's table, including the asymmetry that looks like a bug and is
	// not: README has no extension and is NOT in the prose-filename set, so it is
	// treated as verifiable; LICENSE is in that set.
	for path, want := range map[string]bool{
		"docs/SKILL.md":  true,
		"README":         false,
		"LICENSE":        true,
		"src/app.ts":     false,
		"config.yaml":    false,
		"run_agent.py":   false,
		"notes.TXT":      true,
		"data/table.csv": true,
		"":               false,
	} {
		if got := isNonCodePath(path); got != want {
			t.Errorf("isNonCodePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestChangedPathsAreBoundedInTheNudge(t *testing.T) {
	cwd := "src"
	ledger := &fakeLedger{facts: map[string]ProjectFacts{cwd: nodeProject("go test ./...")}}
	paths := make([]string, 0, maxChangedPathsInNudge+3)
	for i := 0; i < cap(paths); i++ {
		paths = append(paths, filepath.Join(cwd, string(rune('a'+i))+".go"))
	}

	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1", ChangedPaths: paths, MaxAttempts: 2,
	})

	if !ok {
		t.Fatal("expected a nudge")
	}
	if !strings.Contains(nudge, "... and 3 more") {
		t.Fatalf("nudge must say how many paths it elided, got: %s", nudge)
	}
}

func TestStatusDetailBoundsTheEvidenceSummary(t *testing.T) {
	status := VerificationStatus{
		Status: "failed",
		Evidence: &VerificationEvidence{
			CanonicalCommand: "go test ./...",
			Command:          "ignored when canonical is present",
			OutputSummary:    strings.Repeat("x", maxStatusSummary+500),
		},
	}

	detail := statusDetail(status)

	if !strings.Contains(detail, "go test ./...") {
		t.Fatalf("the canonical command must be named: %s", detail[:80])
	}
	if strings.Contains(detail, "ignored when canonical is present") {
		t.Fatal("canonical command must win over command")
	}
	if !strings.HasSuffix(detail, "... [truncated]") {
		t.Fatal("an overlong summary must be truncated and say so")
	}
}
