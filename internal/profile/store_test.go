package profile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreWriteUpdateRead(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()

	first := Profile{
		AgentMD:     "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses.\n",
		Preferences: Preferences{Lang: "it", ResponseLength: "short"},
		Metadata:    Metadata{Version: 1, OnboardingCompleted: true},
		Change:      "initial onboarding profile",
	}
	if err := s.WriteProfile("local", first); err != nil {
		t.Fatalf("initial WriteProfile: %v", err)
	}
	second := first
	second.AgentMD = "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses.\n- Prefer short technical answers.\n"
	second.Metadata.Version = 2
	second.Change = "added response-length preference"
	if err := s.WriteProfile("local", second); err != nil {
		t.Fatalf("update WriteProfile: %v", err)
	}

	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if !strings.Contains(got.AgentMD, "Prefer short technical answers.") {
		t.Fatalf("Agent.md missing updated preference:\n%s", got.AgentMD)
	}
	if got.Preferences.Lang != "it" || got.Preferences.ResponseLength != "short" {
		t.Fatalf("preferences did not round-trip: %#v", got.Preferences)
	}
	if got.Metadata.Version != 2 {
		t.Fatalf("metadata version = %d, want 2", got.Metadata.Version)
	}
	if !strings.Contains(got.Changelog, "initial onboarding profile") ||
		!strings.Contains(got.Changelog, "added response-length preference") {
		t.Fatalf("changelog did not preserve both entries:\n%s", got.Changelog)
	}
	assertNoTempFiles(t, filepath.Join(s.Root(), "local"))
}

func TestStoreRejectsUnsafeIdentityBeforeWrite(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	for _, id := range []string{"", "../evil", `..\evil`, "a/b", `a\b`, ".hidden", "local..evil"} {
		t.Run(id, func(t *testing.T) {
			err := s.WriteProfile(id, Profile{AgentMD: "# Agent.md\n"})
			if !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("WriteProfile(%q): want ErrInvalidIdentity, got %v", id, err)
			}
		})
	}
	entries, err := os.ReadDir(s.Root())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadDir root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unsafe identities created files: %#v", entries)
	}
}

func TestAtomicWriteCleansTempOnReplaceFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Agent.md")
	errSentinel := errors.New("replace failed")
	err := atomicWriteWithReplace(path, []byte("new"), func(_, _ string) error {
		return errSentinel
	})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("atomicWriteWithReplace: want sentinel, got %v", err)
	}
	assertNoTempFiles(t, dir)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target after failed replace: want absent, got %v", err)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("left temp file behind: %s", entry.Name())
		}
	}
}
