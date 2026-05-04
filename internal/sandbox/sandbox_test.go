package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
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

func TestExecute_Integration(t *testing.T) {
	runnerPath := sandbox.FindRunnerPath()
	if runnerPath == "" {
		t.Skip("runner not found")
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: "python3",
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
