package agentdef

import (
	"io/fs"
	"testing"
)

func TestSummarizerArchetype_LoadsAtBoot(t *testing.T) {
	reg, err := BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry: %v", err)
	}
	def, ok := reg.Get("summarizer")
	if !ok {
		t.Fatal("summarizer archetype not found")
	}
	if def.DisplayName != "Tool Result Summarizer" || def.Tier != TierWorker {
		t.Fatalf("unexpected summarizer metadata: %+v", def)
	}
	if def.MaxIterations != 1 || def.MaxInputTokens != 16384 || def.MaxOutputTokens != 4096 || def.MaxResultChars != 8000 {
		t.Fatalf("unexpected summarizer limits: %+v", def)
	}
	if len(def.Tools.Named) != 0 || !def.InheritSafety {
		t.Fatalf("unexpected summarizer scope: %+v", def)
	}
}

func TestSummarizerArchetype_PromptByteIdentical(t *testing.T) {
	raw, err := fs.ReadFile(BuiltinFS, "builtin/summarizer/prompt.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	def, err := BuiltinDefinition("summarizer")
	if err != nil {
		t.Fatalf("BuiltinDefinition: %v", err)
	}
	if def.Prompt != string(raw) {
		t.Fatalf("prompt changed: got %d bytes, want %d", len(def.Prompt), len(raw))
	}
}
