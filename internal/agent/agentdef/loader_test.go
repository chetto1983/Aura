package agentdef

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoader_BuiltinEmpty_NoError(t *testing.T) {
	defs, err := (&Loader{BuiltinFS: fstest.MapFS{}}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("defs = %d, want 0", len(defs))
	}
}

func TestLoader_UserDirMissing_NoError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	defs, err := (&Loader{UserDir: missing}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("defs = %d, want 0", len(defs))
	}
}

func TestLoader_MalformedJSON_LineCol(t *testing.T) {
	_, err := (&Loader{BuiltinFS: fstest.MapFS{
		"bad/agent.json": {Data: []byte("{\n  bad")},
	}}).Load(context.Background())
	if err == nil {
		t.Fatal("Load succeeded, want parse error")
	}
	text := err.Error()
	if !strings.Contains(text, "bad/agent.json") || !strings.Contains(text, ":2:") {
		t.Fatalf("err = %q, want path and line/col", text)
	}
}

func TestLoader_UserOverridesBuiltin_BySlug(t *testing.T) {
	userDir := t.TempDir()
	writeFile(t, filepath.Join(userDir, "writer", "agent.json"), `{
		"id": "writer",
		"when_to_use": "new guidance",
		"prompt": "new prompt"
	}`)
	defs, err := (&Loader{
		BuiltinFS: fstest.MapFS{
			"writer/agent.json": {Data: []byte(`{"id":"writer","when_to_use":"old guidance","prompt":"old prompt"}`)},
		},
		UserDir: userDir,
	}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("defs = %d, want 1", len(defs))
	}
	if defs[0].WhenToUse != "new guidance" || defs[0].Prompt != "new prompt" {
		t.Fatalf("override not applied: %+v", defs[0])
	}
	if !strings.Contains(defs[0].Source, "user:") {
		t.Fatalf("Source = %q, want user source", defs[0].Source)
	}
}

func TestLoader_DefaultsApplied(t *testing.T) {
	defs, err := (&Loader{BuiltinFS: fstest.MapFS{
		"worker/agent.json": {Data: []byte(`{"id":"worker","when_to_use":"when","prompt":"do work"}`)},
	}}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := defs[0]
	if def.Tier != TierWorker {
		t.Fatalf("Tier = %q, want %q", def.Tier, TierWorker)
	}
	if def.MaxIterations != DefaultMaxIterations || def.MaxResultChars != DefaultMaxResultChars {
		t.Fatalf("defaults = iterations %d chars %d", def.MaxIterations, def.MaxResultChars)
	}
	if def.Prompt != "do work" {
		t.Fatalf("Prompt = %q", def.Prompt)
	}
}

func TestLoader_PromptPathRelativeToDefinition(t *testing.T) {
	defs, err := (&Loader{BuiltinFS: fstest.MapFS{
		"worker/agent.json": {Data: []byte(`{"id":"worker","when_to_use":"when","prompt_path":"prompt.md"}`)},
		"worker/prompt.md":  {Data: []byte("prompt from file\n")},
	}}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if defs[0].Prompt != "prompt from file\n" {
		t.Fatalf("Prompt = %q", defs[0].Prompt)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
