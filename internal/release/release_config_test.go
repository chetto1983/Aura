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

	requireContains(t, body, "workflow_dispatch:")
	requireNotContains(t, body, "tags:")
	requireContains(t, body, "Legacy desktop binary release")
	requireContains(t, body, "node-version: '20'")
	requireContains(t, body, "node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide --with-node-win-x64")
	requireOrder(t, body,
		"name: Build Pyodide runtime bundle",
		"name: Run GoReleaser",
	)
}

func TestDockerImageWorkflowPublishesGHCRImageOnTags(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, ".github", "workflows", "docker-image.yml"))

	requireContains(t, body, "tags:")
	requireContains(t, body, "- 'v*'")
	requireContains(t, body, "workflow_dispatch:")
	requireContains(t, body, "packages: write")
	requireContains(t, body, "REGISTRY: ghcr.io")
	requireContains(t, body, "IMAGE_NAME: chetto1983/aura")
	requireContains(t, body, "actions/checkout@v6")
	requireContains(t, body, "actions/setup-node@v6")
	requireContains(t, body, "node-version: '24'")
	requireContains(t, body, "cache-dependency-path: web/package-lock.json")
	requireContains(t, body, "node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide")
	requireOrder(t, body,
		"name: Build Pyodide runtime bundle",
		"name: Build and push Docker image",
	)
	requireContains(t, body, "docker/login-action@v4")
	requireContains(t, body, "registry: ${{ env.REGISTRY }}")
	requireContains(t, body, "password: ${{ secrets.GITHUB_TOKEN }}")
	requireContains(t, body, "docker/metadata-action@v6")
	requireContains(t, body, "type=ref,event=tag")
	requireContains(t, body, "type=raw,value=latest")
	requireContains(t, body, "docker/build-push-action@v7")
	requireContains(t, body, "platforms: linux/amd64,linux/arm64")
	requireContains(t, body, "push: true")
	requireContains(t, body, "provenance: true")
	requireContains(t, body, "sbom: true")
}

func TestComposeImageOverrideUsesPublishedImage(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "compose.image.yaml"))

	requireContains(t, body, "ghcr.io/chetto1983/aura:latest")
	requireContains(t, body, "build: null")
	requireContains(t, body, "pull_policy: always")
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

func requireNotContains(t *testing.T, body, needle string) {
	t.Helper()
	if strings.Contains(body, needle) {
		t.Fatalf("unexpected %q", needle)
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
