package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionContainerArtifactsAreHardened(t *testing.T) {
	root := repoRootForTest(t)
	dockerfile := readProjectFile(t, root, "Dockerfile")
	compose := readProjectFile(t, root, "compose.yaml")
	dockerignore := readProjectFile(t, root, ".dockerignore")

	for _, want := range []string{"FROM golang:", "USER 65532", "ENTRYPOINT [\"/aura\"]"} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing %q:\n%s", want, dockerfile)
		}
	}
	for _, want := range []string{"aura:", "read_only: true", "cap_drop:", "- ALL", "mem_limit:", "cpus:", "healthcheck:"} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing %q", want)
		}
	}
	for _, want := range []string{".git", ".worktrees", "output", ".env"} {
		if !strings.Contains(dockerignore, want) {
			t.Fatalf(".dockerignore missing %q:\n%s", want, dockerignore)
		}
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readProjectFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
