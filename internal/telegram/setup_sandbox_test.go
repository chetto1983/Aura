package telegram

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/tools"
)

func TestSetupSandboxRuntime_Disabled(t *testing.T) {
	mgr, health := setupSandboxRuntime(&config.Config{SandboxEnabled: false}, slog.Default())

	if mgr != nil {
		t.Fatal("manager = non-nil, want nil")
	}
	if health.Enabled {
		t.Fatal("health.Enabled = true, want false")
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindUnavailable) {
		t.Fatalf("RuntimeKind = %q", health.RuntimeKind)
	}
	if health.Detail != "sandbox disabled" {
		t.Fatalf("Detail = %q", health.Detail)
	}
}

func TestSetupSandboxRuntime_MissingBundleKeepsExecuteCodeDisabled(t *testing.T) {
	runtimeDir := t.TempDir()

	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:    true,
		SandboxRuntimeDir: runtimeDir,
		SandboxTimeoutSec: 15,
	}, slog.Default())

	if mgr != nil {
		t.Fatal("manager = non-nil, want nil")
	}
	if tools.NewExecuteCodeTool(mgr) != nil {
		t.Fatal("execute_code registered with unavailable runtime")
	}
	if !health.Enabled {
		t.Fatal("health.Enabled = false, want true")
	}
	if health.Available {
		t.Fatal("health.Available = true, want false")
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindUnavailable) {
		t.Fatalf("RuntimeKind = %q", health.RuntimeKind)
	}
	if !strings.Contains(health.Detail, "manifest missing") {
		t.Fatalf("Detail = %q, want manifest diagnostic", health.Detail)
	}
}

func TestSetupSandboxRuntime_ValidBundleMissingRunnerKeepsExecuteCodeDisabled(t *testing.T) {
	runtimeDir := t.TempDir()
	writeTelegramTestPyodideBundle(t, runtimeDir)

	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:    true,
		SandboxRuntimeDir: runtimeDir,
		SandboxTimeoutSec: 15,
	}, slog.Default())

	if mgr != nil {
		t.Fatal("manager = non-nil, want nil")
	}
	if health.Available {
		t.Fatal("health.Available = true, want false")
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindPyodide) {
		t.Fatalf("RuntimeKind = %q", health.RuntimeKind)
	}
	if !strings.Contains(health.Detail, "runner missing") {
		t.Fatalf("Detail = %q, want runner diagnostic", health.Detail)
	}
}

func TestSetupSandboxRuntime_UnhealthyBundleReportsUnavailable(t *testing.T) {
	runtimeDir := t.TempDir()
	writeTelegramTestPyodideBundle(t, runtimeDir)
	if err := os.WriteFile(filepath.Join(runtimeDir, "pyodide.asm.wasm"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:    true,
		SandboxRuntimeDir: runtimeDir,
		SandboxTimeoutSec: 15,
	}, slog.Default())

	if mgr != nil {
		t.Fatal("manager = non-nil, want nil")
	}
	if health.Available {
		t.Fatal("health.Available = true, want false")
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindUnavailable) {
		t.Fatalf("RuntimeKind = %q, want unavailable", health.RuntimeKind)
	}
	if !strings.Contains(health.Detail, "sha256 mismatch") {
		t.Fatalf("Detail = %q, want hash diagnostic", health.Detail)
	}
}

func TestSetupSandboxRuntime_HealthyBundleEnablesExecuteCode(t *testing.T) {
	runtimeDir := t.TempDir()
	writeTelegramTestPyodideBundle(t, runtimeDir)
	runnerPath := defaultTelegramTestRunnerPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(runnerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runnerPath, []byte("runner"), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:    true,
		SandboxRuntimeDir: runtimeDir,
		SandboxTimeoutSec: 21,
	}, slog.Default())

	if mgr == nil {
		t.Fatal("manager = nil, want configured manager")
	}
	if tools.NewExecuteCodeTool(mgr) == nil {
		t.Fatal("execute_code not registered with healthy runtime")
	}
	if !health.Available {
		t.Fatalf("health.Available = false, detail=%q", health.Detail)
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindPyodide) {
		t.Fatalf("RuntimeKind = %q", health.RuntimeKind)
	}
	if health.Runtime != runtimeDir {
		t.Fatalf("Runtime = %q, want %q", health.Runtime, runtimeDir)
	}
	if !strings.Contains(health.Detail, "available") {
		t.Fatalf("Detail = %q, want available diagnostic", health.Detail)
	}
}

func TestSetupSandboxRuntime_ContainerModeEnablesExecuteCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"detail":"ready"}`))
	}))
	defer server.Close()

	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:     true,
		SandboxRuntimeMode: "container",
		SandboxRuntimeURL:  server.URL,
		SandboxTimeoutSec:  21,
	}, slog.Default())

	if mgr == nil {
		t.Fatal("manager = nil, want configured manager")
	}
	if tools.NewExecuteCodeTool(mgr) == nil {
		t.Fatal("execute_code not registered with container runtime")
	}
	if !health.Available {
		t.Fatalf("health.Available = false, detail=%q", health.Detail)
	}
	if health.Runtime != server.URL {
		t.Fatalf("Runtime = %q, want %q", health.Runtime, server.URL)
	}
	if !strings.Contains(health.Detail, "ready") {
		t.Fatalf("Detail = %q, want ready diagnostic", health.Detail)
	}
}

func TestSetupSandboxRuntime_ContainerModeUnavailableDisablesExecuteCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:     true,
		SandboxRuntimeMode: "container",
		SandboxRuntimeURL:  server.URL,
		SandboxTimeoutSec:  21,
	}, slog.Default())

	if mgr != nil {
		t.Fatal("manager = non-nil, want nil")
	}
	if health.Available {
		t.Fatal("health.Available = true, want false")
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindUnavailable) {
		t.Fatalf("RuntimeKind = %q, want unavailable", health.RuntimeKind)
	}
}

func TestSetupSandboxRuntime_ProcessModeEnablesExecuteCode(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	workDir := t.TempDir()
	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:     true,
		SandboxRuntimeMode: "process",
		SandboxTimeoutSec:  21,
		WorkspaceRoot:      workDir,
	}, slog.Default())

	if mgr == nil {
		t.Fatal("manager = nil, want configured manager")
	}
	if tools.NewExecuteCodeTool(mgr) == nil {
		t.Fatal("execute_code not registered with process runtime")
	}
	if !health.Available {
		t.Fatalf("health.Available = false, detail=%q", health.Detail)
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindProcess) {
		t.Fatalf("RuntimeKind = %q, want process", health.RuntimeKind)
	}
	if health.Runtime != workDir {
		t.Fatalf("Runtime = %q, want %q", health.Runtime, workDir)
	}
}

func writeTelegramTestPyodideBundle(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"pyodide.mjs":       "pyodide loader",
		"pyodide.asm.wasm":  "wasm bytes",
		"python_stdlib.zip": "stdlib bytes",
		"repodata.json":     `{"packages":{}}`,
	}
	manifest := sandbox.PyodideManifest{
		SchemaVersion:  1,
		Runtime:        string(sandbox.RuntimeKindPyodide),
		PyodideVersion: "0.29.3",
		SmokeImports:   append([]string(nil), sandbox.RequiredPyodideImports...),
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		manifest.Files = append(manifest.Files, sandbox.PyodideManifestFile{
			Path:     path,
			SHA256:   telegramTestSHA256(content),
			Required: true,
		})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sandbox.PyodideManifestFilename), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func defaultTelegramTestRunnerPath(runtimeDir string) string {
	name := "aura-pyodide-runner"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(runtimeDir, "runner", name)
}

func telegramTestSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
