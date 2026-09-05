package idroot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateIdentity(t *testing.T) {
	t.Parallel()
	valid := []string{"local", "davide", "a", "id-1.2_3", strings.Repeat("x", 64)}
	for _, id := range valid {
		if err := ValidateIdentity(id); err != nil {
			t.Errorf("ValidateIdentity(%q) = %v, want nil", id, err)
		}
	}
	invalid := []string{"", "../evil", `..\evil`, "a/b", `a\b`, ".hidden", "local..evil", "-lead", strings.Repeat("x", 65), "spa ce"}
	for _, id := range invalid {
		if err := ValidateIdentity(id); !errors.Is(err, ErrInvalidIdentity) {
			t.Errorf("ValidateIdentity(%q) = %v, want ErrInvalidIdentity", id, err)
		}
	}
}

func TestRootIdentityDirContainsUnderRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir, err := RootIdentityDir(root, "local")
	if err != nil {
		t.Fatalf("RootIdentityDir: %v", err)
	}
	want := filepath.Join(root, "local")
	if dir != want {
		t.Fatalf("RootIdentityDir = %q, want %q", dir, want)
	}
}

func TestRootIdentityDirRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, id := range []string{"", "../evil", `..\evil`, "a/b", "local..evil"} {
		if _, err := RootIdentityDir(root, id); !errors.Is(err, ErrInvalidIdentity) {
			t.Errorf("RootIdentityDir(%q) = %v, want ErrInvalidIdentity", id, err)
		}
	}
}

func TestContainedDirRejectsEscapeBehindValidate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Drive the containment guard directly with traversal that ValidateIdentity
	// would reject upstream, proving the defense-in-depth escape check.
	for _, id := range []string{".", "..", "../../evil"} {
		if _, err := containedDir(root, id); !errors.Is(err, ErrInvalidIdentity) {
			t.Errorf("containedDir(%q) = %v, want ErrInvalidIdentity", id, err)
		}
	}
	if _, err := containedDir(root, "safe"); err != nil {
		t.Errorf("containedDir(safe) = %v, want nil", err)
	}
}

// TestContainedDirUnresolvableRoot covers the one reachable failure in containedDir:
// filepath.Abs consults the working directory for a RELATIVE root, so a removed cwd makes
// it fail. Not parallel — t.Chdir is process-wide, and it restores the original dir on
// cleanup (the original still exists; only the temp one is gone).
func TestContainedDirUnresolvableRoot(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove cwd: %v", err)
	}
	if _, err := containedDir("relative-root", "local"); err == nil {
		t.Fatal("containedDir with an unresolvable relative root = nil error, want a failure")
	}
}
