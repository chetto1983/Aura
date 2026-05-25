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

func TestOrchestratorArchetype_DelegatesToSummarizer(t *testing.T) {
	reg, err := BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry: %v", err)
	}
	def, ok := reg.Get(DefaultChatArchetype)
	if !ok {
		t.Fatalf("%s archetype not found", DefaultChatArchetype)
	}
	if def.Tier != TierChat {
		t.Fatalf("orchestrator tier = %q, want chat", def.Tier)
	}
	if len(def.Subagents) != 1 || def.Subagents[0].ID != "summarizer" {
		t.Fatalf("orchestrator subagents = %+v, want summarizer", def.Subagents)
	}
}

func TestReflectorArchetype_LoadsAtBoot(t *testing.T) {
	def, err := BuiltinDefinition("reflector")
	if err != nil {
		t.Fatalf("BuiltinDefinition: %v", err)
	}
	if def.DisplayName != "Reflection Extractor" || def.Tier != TierWorker {
		t.Fatalf("unexpected reflector metadata: %+v", def)
	}
	if def.MaxIterations != 1 || def.MaxInputTokens != 16384 || def.MaxOutputTokens != 2048 || def.MaxResultChars != 8000 {
		t.Fatalf("unexpected reflector limits: %+v", def)
	}
	if len(def.Tools.Named) != 0 || !def.InheritSafety {
		t.Fatalf("unexpected reflector scope: %+v", def)
	}
}

func TestSummarizerArchetype_PromptByteIdentical(t *testing.T) {
	assertBuiltinPromptByteIdentical(t, "summarizer")
}

func TestReflectorArchetype_PromptByteIdentical(t *testing.T) {
	assertBuiltinPromptByteIdentical(t, "reflector")
}

func assertBuiltinPromptByteIdentical(t *testing.T, id string) {
	t.Helper()
	raw, err := fs.ReadFile(BuiltinFS, "builtin/"+id+"/prompt.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	def, err := BuiltinDefinition(id)
	if err != nil {
		t.Fatalf("BuiltinDefinition: %v", err)
	}
	if def.Prompt != string(raw) {
		t.Fatalf("prompt changed: got %d bytes, want %d", len(def.Prompt), len(raw))
	}
}
