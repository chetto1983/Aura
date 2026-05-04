package release_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoReleaserBuildsSmokesAndArchivesPyodideBundle(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, ".goreleaser.yml"))

	requireContains(t, body, "node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide --with-node-win-x64")
	requireContains(t, body, "go run ./cmd/debug_sandbox --smoke")
	requireContains(t, body, "src: runtime/pyodide/**/*")
	requireContains(t, body, "dst: runtime/pyodide")
}

func TestReleaseWorkflowPreparesPyodideBundleBeforeGoReleaser(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))

	requireContains(t, body, "node-version: '20'")
	requireContains(t, body, "node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide --with-node-win-x64")
	requireOrder(t, body,
		"name: Build Pyodide runtime bundle",
		"name: Run GoReleaser",
	)
}

func TestRuntimeBundleInstallerDocumentsPinnedReleaseInputs(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "runtime", "install-pyodide-bundle.mjs"))

	requireContains(t, body, "0.29.3")
	requireContains(t, body, "pyodide-lock.json")
	requireContains(t, body, "aura-pyodide-manifest.json")
	requireContains(t, body, "aura-pyodide-runner.mjs")
	requireContains(t, body, "node-v22.13.1-win-x64.zip")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func requireContains(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Fatalf("missing %q", needle)
	}
}

func requireOrder(t *testing.T, body string, needles ...string) {
	t.Helper()
	last := -1
	for _, needle := range needles {
		idx := strings.Index(body, needle)
		if idx == -1 {
			t.Fatalf("missing %q", needle)
		}
		if idx <= last {
			t.Fatalf("%q appears out of order", needle)
		}
		last = idx
	}
}
