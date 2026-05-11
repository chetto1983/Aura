package release_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoReleaserDoesNotArchiveLegacyRuntimeBundle(t *testing.T) {
	root := repoRoot(t)
	body, ok := readOptionalFile(t, filepath.Join(root, ".goreleaser.yml"))
	if !ok {
		t.Skip(".goreleaser.yml is not tracked in this Docker-first branch")
	}

	requireNotContains(t, body, "install-pyo"+"dide-bundle")
	requireNotContains(t, body, filepath.ToSlash(filepath.Join("runtime", "pyo"+"dide")))
}

func TestReleaseWorkflowDoesNotPrepareLegacyRuntimeBundle(t *testing.T) {
	root := repoRoot(t)
	body, ok := readOptionalFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if !ok {
		t.Skip("legacy desktop release workflow is not tracked in this branch")
	}

	requireContains(t, body, "workflow_dispatch:")
	requireNotContains(t, body, "tags:")
	requireContains(t, body, "Legacy desktop binary release")
	requireNotContains(t, body, "install-pyo"+"dide-bundle")
	requireNotContains(t, body, "Pyo"+"dide runtime bundle")
	requireNotContains(t, body, filepath.ToSlash(filepath.Join("runtime", "pyo"+"dide")))
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
	requireContains(t, body, "docker/setup-qemu-action@v4")
	requireContains(t, body, "docker/setup-buildx-action@v4")
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

func TestRuntimeBundleInstallerRemoved(t *testing.T) {
	root := repoRoot(t)
	installer := filepath.Join(root, "runtime", "install-pyo"+"dide-bundle.mjs")
	if _, err := os.Stat(installer); !os.IsNotExist(err) {
		t.Fatalf("%s exists or stat failed: %v", filepath.ToSlash(filepath.Join("runtime", filepath.Base(installer))), err)
	}
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

func readOptionalFile(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data), true
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
