package sandbox

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessRunnerExecutesInWorkDirAndCollectsArtifacts(t *testing.T) {
	pythonPath, ok := findTestPython(t)
	if !ok {
		return
	}
	workDir := t.TempDir()
	runner, err := NewProcessRunner(ProcessRunnerConfig{
		PythonPath: pythonPath,
		WorkDir:    workDir,
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewProcessRunner() error = %v", err)
	}

	result, err := runner.Execute(context.Background(), strings.TrimSpace(`
import os
from pathlib import Path
print(Path.cwd())
Path(os.environ["AURA_OUT_DIR"], "result.txt").write_text("hello from process", encoding="utf-8")
`), false)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, stderr=%q", result.Stderr)
	}
	if !strings.Contains(result.Stdout, filepath.Clean(workDir)) {
		t.Fatalf("stdout = %q, want workdir %q", result.Stdout, workDir)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if result.Artifacts[0].Name != "result.txt" || string(result.Artifacts[0].Bytes) != "hello from process" {
		t.Fatalf("artifact = %+v", result.Artifacts[0])
	}
}

func TestProcessRunnerReportsPythonAvailability(t *testing.T) {
	pythonPath, ok := findTestPython(t)
	if !ok {
		return
	}
	runner, err := NewProcessRunner(ProcessRunnerConfig{PythonPath: pythonPath, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewProcessRunner() error = %v", err)
	}

	availability := runner.CheckAvailability()
	if !availability.Available {
		t.Fatalf("availability = %+v, want available", availability)
	}
	if availability.Kind != RuntimeKindProcess {
		t.Fatalf("kind = %q, want process", availability.Kind)
	}
}

func TestProcessRunnerExecutesShellCommandInWorkDir(t *testing.T) {
	runner, err := NewProcessRunner(ProcessRunnerConfig{
		WorkDir: t.TempDir(),
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewProcessRunner() error = %v", err)
	}

	command := "pwd"
	if runtime.GOOS == "windows" {
		command = "cd"
	}
	result, err := runner.ExecuteCommand(context.Background(), command, false)
	if err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, stderr=%q", result.Stderr)
	}
	if !strings.Contains(result.Stdout, filepath.Clean(runner.workDir)) {
		t.Fatalf("stdout = %q, want workdir %q", result.Stdout, runner.workDir)
	}
}

func findTestPython(t *testing.T) (string, bool) {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	t.Skip("python not available")
	return "", false
}
