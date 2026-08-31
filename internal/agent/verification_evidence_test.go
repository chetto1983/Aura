// Ported from NousResearch/hermes-agent `tests/agent/test_verification_evidence.py`
// (MIT, commit 9d4ef04ed). The three classification cases port directly; the two that
// drive the SQLite ledger (staleness by session, retention expiry) belong with the
// Postgres store, and `test_windows_backslash_ad_hoc_script_path_is_matched` is NOT
// ported on purpose -- it exercises the posix=False split that only a Windows host can
// produce, and Aura runs on Linux in a container.
package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLintAndTypecheckAreNotReportedAsFullTests(t *testing.T) {
	// A node project whose canonical commands come from package.json scripts.
	commands := []string{"pnpm run lint", "pnpm run test"}

	lint, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "pnpm run lint", CWD: "/w", Root: "/w",
		SessionID: "s1", VerifyCommands: commands,
	})
	if !ok {
		t.Fatal("pnpm run lint must classify")
	}
	if lint.Kind != "lint" {
		t.Errorf("kind = %q, want lint", lint.Kind)
	}
	if lint.Scope != "full" {
		t.Errorf("scope = %q, want full -- no target argument was given", lint.Scope)
	}

	test, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "pnpm run test -- tests/button.test.tsx", CWD: "/w", Root: "/w",
		SessionID: "s1", VerifyCommands: commands,
	})
	if !ok {
		t.Fatal("pnpm run test must classify")
	}
	if test.Kind != "test" {
		t.Errorf("kind = %q, want test", test.Kind)
	}
	// The whole reason scope exists: this run proved one file, not the suite.
	if test.Scope != "targeted" {
		t.Errorf("scope = %q, want targeted", test.Scope)
	}
}

func TestShellWrappersMatchButEchoDoesNot(t *testing.T) {
	commands := []string{"scripts/run_tests.sh"}

	wrapped, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "env CI=1 bash scripts/run_tests.sh tests/test_widget.py",
		CWD:     "/w", Root: "/w", SessionID: "s1", VerifyCommands: commands,
	})
	if !ok {
		t.Fatal("a script invoked through env + bash is still the project's own suite")
	}
	if wrapped.CanonicalCommand != "scripts/run_tests.sh" {
		t.Errorf("canonical = %q, want scripts/run_tests.sh", wrapped.CanonicalCommand)
	}
	if wrapped.Scope != "targeted" {
		t.Errorf("scope = %q, want targeted", wrapped.Scope)
	}

	// Naming the script is not running it.
	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "echo scripts/run_tests.sh tests/test_widget.py",
		CWD:     "/w", Root: "/w", SessionID: "s1", VerifyCommands: commands,
	}); ok {
		t.Fatal("echo must not count as verification")
	}
}

func TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite(t *testing.T) {
	// POSIX-only by subject, not by convenience. The classifier reads commands that ran
	// through shell_exec, which always executes inside the Linux box, so every path it
	// can ever see is POSIX. This test builds one with filepath.Join, and on Windows that
	// yields backslashes which the shell tokenizer correctly eats as escapes — measured:
	// "python C:\Users\...\aura-check.py" tokenizes to
	// ["python" "C:UsersAppDataLocalTempaura-check.py"]. The tokenizer is right; the path
	// is the thing that cannot occur. Guarded as result_test.go guards its own
	// platform-unrepresentable assertion.
	if runtime.GOOS == "windows" {
		t.Skip("shell_exec runs POSIX commands inside the box; a Windows-native path is not a case the classifier can see")
	}
	script := filepath.Join(os.TempDir(), "aura-ad-hoc-check.py")

	evidence, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "python " + script, CWD: "/w", Root: "/w",
		SessionID: "s1", Output: "ok",
		// EMPTY on purpose: a disposable script only stands in when the project
		// has no canonical command of its own.
		VerifyCommands: nil,
	})

	if !ok {
		t.Fatal("a prefixed script under the temp dir must classify as ad-hoc evidence")
	}
	if evidence.CanonicalCommand != adHocCanonical {
		t.Errorf("canonical = %q, want %q", evidence.CanonicalCommand, adHocCanonical)
	}
	if evidence.Kind != adHocKind {
		t.Errorf("kind = %q, want %q", evidence.Kind, adHocKind)
	}
	if evidence.Scope != "targeted" {
		t.Errorf("scope = %q -- an ad-hoc script can never be full scope", evidence.Scope)
	}
	if evidence.Status != "passed" {
		t.Errorf("status = %q, want passed", evidence.Status)
	}
}

func TestAdHocScriptIsIgnoredWhenTheProjectHasItsOwnSuite(t *testing.T) {
	// The gate the original states as `match is None and not verify_commands`: a
	// project with a real suite must not have it displaced by a scratch script.
	script := filepath.Join(os.TempDir(), "aura-verify-check.py")

	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "python " + script, CWD: "/w", Root: "/w", SessionID: "s1",
		VerifyCommands: []string{"go test ./..."},
	}); ok {
		t.Fatal("an ad-hoc script must not stand in for a project that has a suite")
	}
}

func TestAdHocScriptInsideTheRepoIsNotEvidence(t *testing.T) {
	// All three conditions must hold. A prefixed script that lives under the project
	// root is source code, however it is named.
	root := t.TempDir()
	script := filepath.Join(root, "aura-verify-check.py")

	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "python " + script, CWD: root, Root: root, SessionID: "s1",
	}); ok {
		t.Fatal("a script under the project root is not a disposable check")
	}
}

func TestFailingCommandIsStillEvidence(t *testing.T) {
	// The ledger records what was PROVED, not only what passed -- a red suite is a
	// fact about the workspace and the policy reads it as not-passed.
	evidence, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "go test ./...", CWD: "/w", Root: "/w", SessionID: "s1",
		ExitCode: 1, VerifyCommands: []string{"go test"},
	})
	if !ok {
		t.Fatal("a failing suite is still classified")
	}
	if evidence.Status != "failed" {
		t.Errorf("status = %q, want failed", evidence.Status)
	}
	if evidence.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", evidence.ExitCode)
	}
}

func TestPytestEquivalences(t *testing.T) {
	// Without the equivalence table a project whose canonical command is `pytest`
	// would refuse to recognise the `uv run pytest` the agent actually typed.
	for _, command := range []string{
		"pytest",
		"python -m pytest",
		"python3 -m pytest",
		"uv run pytest",
		"poetry run pytest",
		"pipenv run pytest",
	} {
		if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
			Command: command, CWD: "/w", Root: "/w", SessionID: "s1",
			VerifyCommands: []string{"pytest"},
		}); !ok {
			t.Errorf("%q must be recognised as pytest", command)
		}
	}
}

func TestCommandInAnySegmentCounts(t *testing.T) {
	// Segments are split on the list operators, so a suite that ran after an
	// unrelated cd is still the evidence it is.
	evidence, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "cd /w && go test ./...", CWD: "/w", Root: "/w", SessionID: "s1",
		VerifyCommands: []string{"go test"},
	})
	if !ok {
		t.Fatal("a command after && must still be found")
	}
	if evidence.Scope != "targeted" {
		// `./...` contains a slash, so the original's looksLikeTarget calls it a
		// target -- a whole-repo run reads as "targeted". Pinned deliberately: it is
		// surprising, and it is what the source does, so a change here is a change in
		// behaviour and not a typo fix.
		t.Errorf("scope = %q, want targeted for ./...", evidence.Scope)
	}
}

func TestCleanTokenAsymmetryIsPreserved(t *testing.T) {
	// Only the NEEDLE is cleaned, so the match is one-directional. This is the exact
	// behaviour a first draft of the port got wrong by cleaning both sides.
	dotted := []string{"./scripts/test.sh"}
	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "scripts/test.sh", CWD: "/w", Root: "/w", SessionID: "s1",
		VerifyCommands: dotted,
	}); !ok {
		t.Error("a canonical `./scripts/test.sh` must match a typed `scripts/test.sh`")
	}
	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "./scripts/test.sh", CWD: "/w", Root: "/w", SessionID: "s1",
		VerifyCommands: []string{"scripts/test.sh"},
	}); ok {
		t.Error("and NOT the other way round -- the typed token is never cleaned")
	}
}

func TestSummarizeOutputKeepsHeadAndTailAndSaysWhatItDropped(t *testing.T) {
	head := strings.Repeat("A", 50)
	tail := strings.Repeat("Z", 50)
	long := head + strings.Repeat("m", maxOutputSummaryChars*2) + tail

	summary := summarizeOutput(long)

	if !strings.HasPrefix(summary, head) {
		t.Error("the head must survive: a failure's cause is usually at the top")
	}
	if !strings.HasSuffix(summary, tail) {
		t.Error("the tail must survive: the verdict is usually at the bottom")
	}
	if !strings.Contains(summary, "chars omitted") {
		t.Error("the summary must say how much it dropped")
	}
	if short := summarizeOutput("  green  "); short != "green" {
		t.Errorf("a short output is trimmed and kept whole, got %q", short)
	}
}

func TestEmptyCommandIsNotEvidence(t *testing.T) {
	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "   ", CWD: "/w", Root: "/w", SessionID: "s1",
		VerifyCommands: []string{"go test ./..."},
	}); ok {
		t.Fatal("an empty command classifies as nothing")
	}
}

func TestCanonicalCommandContainingDotSlashNeverMatches(t *testing.T) {
	// A latent defect INHERITED from the source, pinned rather than silently fixed.
	//
	// _clean_token strips a leading "./" from the needle and never from the typed
	// token, so a project whose canonical command is literally `go test ./...` gets
	// the needle ["go","test","..."] -- which only a typed `go test ...` could match,
	// and nobody types that. The project's own suite therefore goes unrecognised.
	//
	// Reproduced deliberately: a port that quietly repaired this would make every
	// future diff against hermes-agent lie about what the two do. If it is to be
	// fixed it should be fixed upstream, or here with a comment saying we diverged.
	if _, ok := ClassifyVerificationCommand(ClassifyVerificationCommandInput{
		Command: "go test ./...", CWD: "/w", Root: "/w", SessionID: "s1",
		VerifyCommands: []string{"go test ./..."},
	}); ok {
		t.Fatal("behaviour changed: the inherited ./-cleaning asymmetry no longer bites")
	}
}
