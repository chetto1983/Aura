package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestInspectAdaptiveBenchmarkGitBindsTrackedStagedAndUntrackedFiles(
	t *testing.T,
) {
	t.Parallel()
	root := t.TempDir()
	for path, content := range map[string]string{
		"tracked.go": "tracked",
		"staged.go":  "staged",
		"new.go":     "untracked",
	} {
		if err := os.WriteFile(
			filepath.Join(root, path),
			[]byte(content),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	commands := &adaptiveBenchmarkGitCommandFake{
		outputs: [][]byte{
			[]byte("0123456789abcdef0123456789abcdef01234567\n"),
			[]byte("tracked.go\x00"),
			[]byte("staged.go\x00"),
			[]byte("new.go\x00"),
		},
	}
	got, err := inspectAdaptiveBenchmarkGit(
		t.Context(),
		commands,
		root,
	)
	if err != nil {
		t.Fatalf("inspectAdaptiveBenchmarkGit: %v", err)
	}
	if !got.Dirty ||
		got.Revision != "0123456789abcdef0123456789abcdef01234567" ||
		len(got.ChangedFiles) != 3 {
		t.Fatalf("git provenance=%#v", got)
	}
	for index, path := range []string{"new.go", "staged.go", "tracked.go"} {
		sum := sha256.Sum256([]byte(map[string]string{
			"new.go": "untracked", "staged.go": "staged",
			"tracked.go": "tracked",
		}[path]))
		if got.ChangedFiles[index].Path != path ||
			got.ChangedFiles[index].SHA256 !=
				hex.EncodeToString(sum[:]) {
			t.Fatalf("changed file %d=%#v", index, got.ChangedFiles[index])
		}
	}
	wantCalls := [][]string{
		{"git", "-C", root, "rev-parse", "HEAD"},
		{"git", "-C", root, "diff", "--name-only", "-z"},
		{"git", "-C", root, "diff", "--cached", "--name-only", "-z"},
		{"git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z"},
	}
	if !slices.EqualFunc(
		commands.calls,
		wantCalls,
		func(left, right []string) bool {
			return slices.Equal(left, right)
		},
	) {
		t.Fatalf("commands=%v want=%v", commands.calls, wantCalls)
	}
}

func TestInspectAdaptiveBenchmarkGitRejectsUnsafeChangedPath(t *testing.T) {
	t.Parallel()
	commands := &adaptiveBenchmarkGitCommandFake{
		outputs: [][]byte{
			[]byte("0123456789abcdef0123456789abcdef01234567\n"),
			[]byte("../secret\x00"),
			nil,
			nil,
		},
	}
	if provenance, err := inspectAdaptiveBenchmarkGit(
		t.Context(),
		commands,
		t.TempDir(),
	); err == nil {
		t.Fatalf("accepted provenance %#v", provenance)
	}
}

type adaptiveBenchmarkGitCommandFake struct {
	outputs [][]byte
	calls   [][]string
}

func (fake *adaptiveBenchmarkGitCommandFake) CombinedOutput(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	call := append([]string{name}, args...)
	fake.calls = append(fake.calls, call)
	index := len(fake.calls) - 1
	return append([]byte(nil), fake.outputs[index]...), nil
}
