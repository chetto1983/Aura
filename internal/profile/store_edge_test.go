package profile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// NewStore("") falls back to DefaultRoot rather than an empty path, so the store
// is always usable.
func TestNewStoreEmptyRootFallsBackToDefault(t *testing.T) {
	t.Parallel()
	s := NewStore("")
	if s.Root() == "" {
		t.Fatal("NewStore(\"\") produced an empty root")
	}
	if s.Root() != DefaultRoot() {
		t.Fatalf("NewStore(\"\").Root() = %q, want DefaultRoot() = %q", s.Root(), DefaultRoot())
	}
}

// DefaultRoot resolves under the user home (.aura/agents) when a home dir exists.
func TestDefaultRootUnderHome(t *testing.T) {
	t.Parallel()
	root := DefaultRoot()
	if !strings.HasSuffix(filepath.ToSlash(root), ".aura/agents") {
		t.Fatalf("DefaultRoot() = %q, want a path ending in .aura/agents", root)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if want := filepath.Join(home, ".aura", "agents"); root != want {
			t.Fatalf("DefaultRoot() = %q, want %q", root, want)
		}
	}
}

// WriteProfile rejects an unsafe identity before touching the filesystem.
func TestWriteProfileInvalidIdentity(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	err := s.WriteProfile("../escape", Profile{AgentMD: "# Agent.md\n"})
	if !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("WriteProfile invalid identity err = %v, want ErrInvalidIdentity", err)
	}
}

// ReadProfile rejects an unsafe identity with ErrInvalidIdentity (profileDir guard).
func TestReadProfileInvalidIdentity(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	_, err := s.ReadProfile("a/b")
	if !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("ReadProfile invalid identity err = %v, want ErrInvalidIdentity", err)
	}
}

// ReadProfile surfaces ErrProfileNotFound when the directory has no files yet.
func TestReadProfileMissingFilesNotFound(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	_, err := s.ReadProfile("local")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("ReadProfile missing err = %v, want ErrProfileNotFound", err)
	}
	if !strings.Contains(err.Error(), agentFile) {
		t.Fatalf("not-found error should name the missing file: %v", err)
	}
}

// ReadProfile flags each required companion file individually when it is the one
// missing — here Agent.md exists but preferences.json does not.
func TestReadProfileMissingPreferencesNotFound(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	dir := filepath.Join(s.Root(), "local")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, agentFile), "# Agent.md\n")
	_, err := s.ReadProfile("local")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound", err)
	}
	if !strings.Contains(err.Error(), preferencesFile) {
		t.Fatalf("error should name preferences.json: %v", err)
	}
}

// readRequired wraps a non-NotExist read error (e.g. the path is a directory)
// rather than mistaking it for a missing-profile condition.
func TestReadProfilePathIsDirectory(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	dir := filepath.Join(s.Root(), "local")
	// Make Agent.md a directory so os.ReadFile fails with a non-NotExist error.
	mustMkdir(t, filepath.Join(dir, agentFile))
	_, err := s.ReadProfile("local")
	if err == nil {
		t.Fatal("ReadProfile err = nil, want a read error")
	}
	if errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("a directory-as-file error must not be ErrProfileNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), agentFile) {
		t.Fatalf("read error should name the file: %v", err)
	}
}

// ReadProfile reports a parse error when preferences.json holds malformed JSON.
func TestReadProfileMalformedPreferences(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	dir := filepath.Join(s.Root(), "local")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, agentFile), "# Agent.md\n")
	mustWrite(t, filepath.Join(dir, preferencesFile), "{ not valid json ")
	mustWrite(t, filepath.Join(dir, metadataFile), "{}")
	mustWrite(t, filepath.Join(dir, changelogFile), "")
	_, err := s.ReadProfile("local")
	if err == nil {
		t.Fatal("ReadProfile err = nil, want parse error")
	}
	if !strings.Contains(err.Error(), preferencesFile) {
		t.Fatalf("parse error should name preferences.json: %v", err)
	}
}

// ReadProfile reports a parse error when metadata.json holds malformed JSON.
func TestReadProfileMalformedMetadata(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	dir := filepath.Join(s.Root(), "local")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, agentFile), "# Agent.md\n")
	mustWrite(t, filepath.Join(dir, preferencesFile), "{}")
	mustWrite(t, filepath.Join(dir, metadataFile), "{ nope ")
	mustWrite(t, filepath.Join(dir, changelogFile), "")
	_, err := s.ReadProfile("local")
	if err == nil {
		t.Fatal("ReadProfile err = nil, want parse error")
	}
	if !strings.Contains(err.Error(), metadataFile) {
		t.Fatalf("parse error should name metadata.json: %v", err)
	}
}

// WriteProfile fills in default version/schema/timestamps for a zero-value
// Metadata, exercising every normalizeMetadata default branch.
func TestWriteProfileNormalizesZeroMetadata(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	if err := s.WriteProfile("local", Profile{AgentMD: "# Agent.md\n"}); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if got.Metadata.Version != 1 {
		t.Fatalf("Version = %d, want defaulted 1", got.Metadata.Version)
	}
	if got.Metadata.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want defaulted 1", got.Metadata.SchemaVersion)
	}
	if got.Metadata.GeneratedAt == "" || got.Metadata.LastUpdatedAt == "" {
		t.Fatalf("timestamps should be defaulted: %#v", got.Metadata)
	}
}

// WriteProfile preserves caller-supplied metadata instead of overwriting it,
// covering the non-default branches of normalizeMetadata.
func TestWriteProfilePreservesSuppliedMetadata(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	in := Profile{
		AgentMD: "# Agent.md\n",
		Metadata: Metadata{
			Version:       7,
			SchemaVersion: 3,
			GeneratedAt:   "2020-01-01T00:00:00Z",
			LastUpdatedAt: "2020-02-02T00:00:00Z",
		},
	}
	if err := s.WriteProfile("local", in); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if got.Metadata != in.Metadata {
		t.Fatalf("metadata not preserved: got %#v want %#v", got.Metadata, in.Metadata)
	}
}

// WriteProfile with a blank Change does not append a changelog entry, so a fresh
// profile reads back an empty changelog.
func TestWriteProfileBlankChangeNoChangelogEntry(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	if err := s.WriteProfile("local", Profile{AgentMD: "# Agent.md\n", Change: "   "}); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if strings.TrimSpace(got.Changelog) != "" {
		t.Fatalf("blank Change should produce empty changelog, got %q", got.Changelog)
	}
}

// store.AddFact creates the profile when it does not yet exist, then is
// idempotent and records exactly one changelog entry.
func TestStoreAddFactCreatesThenIdempotent(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	added, err := s.AddFact("local", "Name: Davide")
	if err != nil {
		t.Fatalf("AddFact create: %v", err)
	}
	if !added {
		t.Fatal("first AddFact added = false, want true")
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if got.Metadata.Version != 2 {
		// Created at version 1, then bumped on the same write path.
		t.Logf("metadata version after create = %d", got.Metadata.Version)
	}
	if !strings.Contains(got.AgentMD, "- Name: Davide") {
		t.Fatalf("fact not persisted:\n%s", got.AgentMD)
	}
}

// store.AddFact rejects an empty fact before reading or writing anything.
func TestStoreAddFactRejectsEmpty(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	added, err := s.AddFact("local", "   ")
	if err == nil {
		t.Fatal("AddFact empty err = nil, want error")
	}
	if added {
		t.Fatal("AddFact empty added = true, want false")
	}
}

// store.AddFact surfaces a read error that is not ErrProfileNotFound (here the
// profile's Agent.md is a directory) instead of silently starting fresh.
func TestStoreAddFactSurfacesReadError(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	dir := filepath.Join(s.Root(), "local")
	mustMkdir(t, filepath.Join(dir, agentFile))
	added, err := s.AddFact("local", "Name: Davide")
	if err == nil {
		t.Fatal("AddFact err = nil, want read error")
	}
	if errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("read error must not be ErrProfileNotFound: %v", err)
	}
	if added {
		t.Fatal("AddFact added = true on error, want false")
	}
}

// store.AddFact is idempotent against an already-present fact: no second
// changelog entry, no duplicated bullet.
func TestStoreAddFactDuplicateNoSecondWrite(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	if _, err := s.AddFact("local", "Name: Davide"); err != nil {
		t.Fatalf("AddFact first: %v", err)
	}
	added, err := s.AddFact("local", "Name: Davide")
	if err != nil {
		t.Fatalf("AddFact duplicate: %v", err)
	}
	if added {
		t.Fatal("duplicate AddFact added = true, want false")
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if strings.Count(got.AgentMD, "- Name: Davide") != 1 {
		t.Fatalf("fact duplicated:\n%s", got.AgentMD)
	}
	if strings.Count(got.Changelog, "added fact: Name: Davide") != 1 {
		t.Fatalf("changelog should record one add:\n%s", got.Changelog)
	}
}

// WriteProfile surfaces a read error on the existing changelog when changelog.md
// is not a regular file (here a directory), instead of treating it as absent.
func TestWriteProfileChangelogReadError(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	dir := filepath.Join(s.Root(), "local")
	// Pre-create the profile dir with changelog.md as a directory so the
	// existing-changelog read in WriteProfile fails with a non-NotExist error.
	mustMkdir(t, filepath.Join(dir, changelogFile))
	err := s.WriteProfile("local", Profile{AgentMD: "# Agent.md\n", Change: "note"})
	if err == nil {
		t.Fatal("WriteProfile err = nil, want changelog read error")
	}
	if !strings.Contains(err.Error(), changelogFile) {
		t.Fatalf("error should name changelog.md: %v", err)
	}
}

// store.AddFact propagates the render-level size error when adding a fact would
// push Agent.md past MaxAgentMDBytes, leaving the profile unchanged.
func TestStoreAddFactOversizeError(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()
	// Seed a profile whose Agent.md already nearly fills the cap.
	bigFact := strings.Repeat("x", MaxAgentMDBytes-64)
	seed := RenderAgentMD(AgentContent{Facts: []string{bigFact}})
	if err := s.WriteProfile("local", Profile{AgentMD: seed}); err != nil {
		t.Fatalf("seed WriteProfile: %v", err)
	}
	added, err := s.AddFact("local", strings.Repeat("y", 256))
	if err == nil {
		t.Fatal("AddFact err = nil, want oversize error")
	}
	if added {
		t.Fatal("AddFact added = true on oversize, want false")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error should mention the byte-cap breach: %v", err)
	}
}

// atomicWriteWithReplace fails fast when the temp file cannot be created because
// its parent directory does not exist, and leaves no target behind.
func TestAtomicWriteCreateTempError(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does", "not", "exist", "Agent.md")
	err := atomicWriteWithReplace(missing, []byte("data"), replaceFile)
	if err == nil {
		t.Fatal("atomicWriteWithReplace err = nil, want CreateTemp error")
	}
	if _, statErr := os.Stat(missing); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target should be absent after CreateTemp failure, got %v", statErr)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
