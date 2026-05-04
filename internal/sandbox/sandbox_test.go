package sandbox_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

func TestNewManager_MissingRunner(t *testing.T) {
	_, err := sandbox.NewManager(sandbox.Config{
		RunnerPath: "/nonexistent/runner.py",
	})
	if err == nil {
		t.Fatal("expected error for missing runner")
	}
}

func TestNewManager_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	runnerPath := filepath.Join(tmpDir, "sandbox_runner.py")
	if err := os.WriteFile(runnerPath, []byte("print('{}')"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: "python3",
		RunnerPath: runnerPath,
		Timeout:    0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNewManager_EmptyPythonPathDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	runnerPath := filepath.Join(tmpDir, "sandbox_runner.py")
	if err := os.WriteFile(runnerPath, []byte("print('{}')"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: "",
		RunnerPath: runnerPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestFindRunnerPath(t *testing.T) {
	path := sandbox.FindRunnerPath()
	if path == "" {
		t.Fatal("expected to find runner path in development or production")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("runner path %s does not exist: %v", path, err)
	}
}

func TestValidateCode_RejectsEmptyAndOversize(t *testing.T) {
	mgr := newValidationManager(t)

	if err := mgr.ValidateCode(" \n\t "); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty-code error, got %v", err)
	}

	largeCode := strings.Repeat("x", 100_001)
	if err := mgr.ValidateCode(largeCode); err == nil || !strings.Contains(err.Error(), "exceeds 100KB") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestValidateCode_RejectsForbiddenCallsByAST(t *testing.T) {
	mgr := newValidationManager(t)

	tests := []string{
		`eval("1 + 1")`,
		`eval ("1 + 1")`,
		`exec("print(1)")`,
		`compile("1", "<x>", "exec")`,
		`__import__("os")`,
		`builtins.__import__("os")`,
		`globals()["x"]`,
		`locals()`,
		`getattr(object(), "__class__")`,
		`setattr(object(), "x", 1)`,
		`delattr(object(), "x")`,
		`input("name?")`,
	}

	for _, code := range tests {
		if err := mgr.ValidateCode(code); err == nil || !strings.Contains(err.Error(), "forbidden construct") {
			t.Fatalf("ValidateCode(%q) expected forbidden construct error, got %v", code, err)
		}
	}
}

func TestValidateCode_AllowsForbiddenWordsInCommentsAndStrings(t *testing.T) {
	mgr := newValidationManager(t)

	code := `
# eval("1") and __import__("os") are just documentation here.
print("this string mentions eval(1), exec('x'), and globals()")
`
	if err := mgr.ValidateCode(code); err != nil {
		t.Fatalf("expected harmless comments and strings to pass, got %v", err)
	}
}

func TestValidateCode_RejectsSyntaxErrors(t *testing.T) {
	mgr := newValidationManager(t)

	err := mgr.ValidateCode("def broken(:\n    pass\n")
	if err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("expected syntax error, got %v", err)
	}
}

func TestExecute_Integration(t *testing.T) {
	runnerPath := sandbox.FindRunnerPath()
	if runnerPath == "" {
		t.Skip("runner not found")
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		RunnerPath: runnerPath,
	})
	if err != nil {
		t.Skipf("sandbox not available: %v", err)
	}
	if !mgr.IsAvailable() {
		t.Skip("Isola not installed")
	}

	result, err := mgr.Execute(context.Background(), "print('hello from sandbox')", false)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok, got stderr=%s", result.Stderr)
	}
	if result.Stdout != "hello from sandbox\n" {
		t.Fatalf("expected 'hello from sandbox\\n', got %q", result.Stdout)
	}
}

func newValidationManager(t *testing.T) *sandbox.Manager {
	t.Helper()

	tmpDir := t.TempDir()
	runnerPath := filepath.Join(tmpDir, "sandbox_runner.py")
	if err := os.WriteFile(runnerPath, []byte("print('{}')"), 0644); err != nil {
		t.Fatal(err)
	}

	pythonPath := findTestPython(t)
	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: pythonPath,
		RunnerPath: runnerPath,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return mgr
}

func findTestPython(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("python not available")
	return ""
}
