package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture of plan task C2: a repository declaring three tests, a job that ran one and skipped
// one, and the gate enforcing, as ci.yml's test-ledger job runs it.
func enforceFixture(t *testing.T) (workspace, artifacts string) {
	t.Helper()
	workspace, artifacts = t.TempDir(), t.TempDir()
	runner, err := os.ReadFile(filepath.Join("..", "..", "scripts", "run_script_tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"go.mod":                        "module example.test/fixture\n\ngo 1.27\n",
		"pkg/pkg_test.go":               "package pkg\n\nimport \"testing\"\n\nfunc TestRuns(t *testing.T) {}\n\nfunc TestSkips(t *testing.T) { t.Skip() }\n\nfunc TestNeverRuns(t *testing.T) {}\n",
		"scripts/run_script_tests.sh":   string(runner),
		".github/test_ledger_allow.txt": "# nothing is excused\n",
		".github/excused.txt":           "go | pkg TestNeverRuns | measure | fixture\ngo | pkg TestSkips | platform | fixture\n",
	} {
		write(t, filepath.Join(workspace, rel), content)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(t, filepath.Join(artifacts, "test-ledger-unit-test", "unit-test.jsonl"),
		`{"Action":"pass","Package":"example.test/fixture/pkg","Test":"TestRuns"}`+"\n"+
			`{"Action":"skip","Package":"example.test/fixture/pkg","Test":"TestSkips"}`+"\n")
	return workspace, artifacts
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnforcingRunFailsOnAnUnexecutedAndAnUnexcusedSkippedTest(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	workspace, artifacts := enforceFixture(t)
	args := []string{"check", "--artifacts", artifacts, "--workspace", workspace, "--needs", `{"unit-test":{"result":"success"}}`}

	var red, errs bytes.Buffer
	if code := run(args, &red, &errs); code != 1 {
		t.Fatalf("want exit 1 on a red ledger, got %d: %s", code, errs.String())
	}
	for _, line := range []string{
		"### Violations (2)",
		"- `go` example.test/fixture/pkg TestNeverRuns: declared, and no job executed it",
		"- `go` example.test/fixture/pkg TestSkips: skipped in every job that ran it, and no allowlist entry excuses it",
	} {
		if !strings.Contains(red.String(), line) {
			t.Errorf("the red verdict lacks %q:\n%s", line, red.String())
		}
	}
	if !strings.HasPrefix(red.String(), "## Test ledger\n") {
		t.Errorf("an enforcing verdict is titled without (report-only):\n%s", red.String())
	}

	var green bytes.Buffer
	if code := run(append(args, "--allow", ".github/excused.txt"), &green, &errs); code != 0 {
		t.Fatalf("want exit 0 once both are excused, got %d: %s\n%s", code, errs.String(), green.String())
	}
	if !strings.Contains(green.String(), "### Violations (0)") || !strings.Contains(green.String(), "| go | 3 | 1 | 0 | 2 |") {
		t.Errorf("the green verdict should excuse both and count them:\n%s", green.String())
	}
}
