package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileAddFactThenShow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agents")
	t.Setenv("AURA_PROFILE_DIR", root)
	t.Setenv("OPENROUTER_API_KEY", "")

	addOut := captureStdout(t, func() {
		profileAddFact([]string{"--identity", "local", "I prefer Italian responses"})
	})
	if !strings.Contains(addOut, `ok: added fact to profile "local"`) {
		t.Fatalf("add-fact output = %q", addOut)
	}

	showOut := captureStdout(t, func() {
		profileShow([]string{"--identity", "local"})
	})
	for _, want := range []string{"Agent.md", "- Facts", "  - I prefer Italian responses"} {
		if !strings.Contains(showOut, want) {
			t.Fatalf("show output missing %q:\n%s", want, showOut)
		}
	}

	changelog, err := os.ReadFile(filepath.Join(root, "local", "changelog.md"))
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	if !strings.Contains(string(changelog), "added fact: I prefer Italian responses") {
		t.Fatalf("changelog missing add entry:\n%s", changelog)
	}
}

func TestProfileShowJSON(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agents")
	t.Setenv("AURA_PROFILE_DIR", root)
	t.Setenv("OPENROUTER_API_KEY", "")

	profileAddFact([]string{"--identity", "local", "I prefer concise answers"})
	out := captureStdout(t, func() {
		profileShow([]string{"--identity", "local", "--json"})
	})
	if !strings.Contains(out, `"AgentMD"`) || !strings.Contains(out, "I prefer concise answers") {
		t.Fatalf("json output missing profile fields:\n%s", out)
	}
}
