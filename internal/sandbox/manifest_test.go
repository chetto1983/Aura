package sandbox_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

func TestLoadPyodideManifest_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writePyodideBundle(t, dir, nil)

	manifest, manifestPath, err := sandbox.LoadPyodideManifest(dir)
	if err != nil {
		t.Fatalf("LoadPyodideManifest() error = %v", err)
	}
	if manifest.Runtime != string(sandbox.RuntimeKindPyodide) {
		t.Fatalf("runtime = %q, want pyodide", manifest.Runtime)
	}
	if manifest.PyodideVersion != "0.29.3" {
		t.Fatalf("pyodide_version = %q", manifest.PyodideVersion)
	}
	if !strings.HasSuffix(manifestPath, sandbox.PyodideManifestFilename) {
		t.Fatalf("manifestPath = %q", manifestPath)
	}
}

func TestPyodideRunnerDefaultPathUsesWindowsCmdDevRunner(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows dev runner fallback")
	}
	dir := t.TempDir()
	writePyodideBundle(t, dir, nil)
	runnerPath := filepath.Join(dir, "runner", "aura-pyodide-runner.cmd")
	if err := os.MkdirAll(filepath.Dir(runnerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runnerPath, []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{RuntimeDir: dir})
	if err != nil {
		t.Fatalf("NewPyodideRunner() error = %v", err)
	}
	availability := runner.CheckAvailability()
	if !availability.Available {
		t.Fatalf("CheckAvailability() = %+v, want available", availability)
	}
}

func TestProbePyodideBundle_MissingManifest(t *testing.T) {
	probe := sandbox.ProbePyodideBundle(t.TempDir())
	if probe.Valid {
		t.Fatal("probe.Valid = true, want false")
	}
	if !strings.Contains(probe.Detail, "manifest missing") {
		t.Fatalf("detail = %q, want missing-manifest message", probe.Detail)
	}
}

func TestLoadPyodideManifest_MissingRequiredFile(t *testing.T) {
	dir := t.TempDir()
	writePyodideBundle(t, dir, nil)
	if err := os.Remove(filepath.Join(dir, "pyodide.asm.wasm")); err != nil {
		t.Fatal(err)
	}

	_, _, err := sandbox.LoadPyodideManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "required Pyodide file missing") {
		t.Fatalf("expected missing-file error, got %v", err)
	}
}

func TestLoadPyodideManifest_HashMismatch(t *testing.T) {
	dir := t.TempDir()
	writePyodideBundle(t, dir, func(m *sandbox.PyodideManifest) {
		for i := range m.Files {
			if m.Files[i].Path == "repodata.json" {
				m.Files[i].SHA256 = strings.Repeat("0", 64)
			}
		}
	})

	_, _, err := sandbox.LoadPyodideManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected hash-mismatch error, got %v", err)
	}
}

func TestLoadPyodideManifest_VerifiesRequiredRuntimeGroupEvenWhenEntryNotMarkedRequired(t *testing.T) {
	dir := t.TempDir()
	writePyodideBundle(t, dir, func(m *sandbox.PyodideManifest) {
		for i := range m.Files {
			if m.Files[i].Path == "pyodide.mjs" {
				m.Files[i].Required = false
				m.Files[i].SHA256 = strings.Repeat("0", 64)
			}
		}
	})

	_, _, err := sandbox.LoadPyodideManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected required-runtime hash validation, got %v", err)
	}
}

func TestLoadPyodideManifest_RejectsEscapingPath(t *testing.T) {
	dir := t.TempDir()
	writePyodideBundle(t, dir, func(m *sandbox.PyodideManifest) {
		m.Files = append(m.Files, sandbox.PyodideManifestFile{
			Path:     "../outside",
			SHA256:   strings.Repeat("1", 64),
			Required: true,
		})
	})

	_, _, err := sandbox.LoadPyodideManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "escapes runtime dir") {
		t.Fatalf("expected containment error, got %v", err)
	}
}

func TestLoadPyodideManifest_PackageOmission(t *testing.T) {
	dir := t.TempDir()
	writePyodideBundle(t, dir, func(m *sandbox.PyodideManifest) {
		m.SmokeImports = m.SmokeImports[:len(m.SmokeImports)-1]
	})

	_, _, err := sandbox.LoadPyodideManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "missing required imports") {
		t.Fatalf("expected missing-import error, got %v", err)
	}
	if !strings.Contains(err.Error(), "rich") {
		t.Fatalf("missing-import error = %v, want rich listed", err)
	}
}

func writePyodideBundle(t *testing.T, dir string, mutate func(*sandbox.PyodideManifest)) {
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
			SHA256:   sha256Hex(content),
			Required: true,
		})
	}
	if mutate != nil {
		mutate(&manifest)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sandbox.PyodideManifestFilename), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
