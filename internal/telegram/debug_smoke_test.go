package telegram

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestDebugTextSmokeResultFromMessagesDetectsExecuteCodeAnd5050(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Name: "execute_code",
				Arguments: map[string]any{
					"code": "print(sum(range(1, 101)))",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content:    "exit_code: 0\nelapsed_ms: 42\n\n5050",
		},
		{
			Role:    "assistant",
			Content: "The result is 5050.",
		},
	})

	if !result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = false, want true")
	}
	if !result.Contains5050 {
		t.Fatal("Contains5050 = false, want true")
	}
	if result.FinalText != "The result is 5050." {
		t.Fatalf("FinalText = %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0] != "execute_code" {
		t.Fatalf("ToolCalls = %v", result.ToolCalls)
	}
}

func TestDebugTextSmokeResultFromMessagesDetectsArtifactMetadata(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "make artifact", []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Name: "execute_code",
				Arguments: map[string]any{
					"code": "open('/tmp/aura_out/aura_artifact.txt','w').write('hello')",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content:    "exit_code: 0\nelapsed_ms: 42\n\nwrote file\n\nartifacts:\n- aura_artifact.txt (5 bytes, text/plain; charset=utf-8, delivered=true, persisted=true, source_id=src_0123456789abcdef)",
		},
	})

	if !result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = false, want true")
	}
	if !result.ContainsArtifactMetadata {
		t.Fatal("ContainsArtifactMetadata = false, want true")
	}
	if len(result.ArtifactFilenames) != 1 || result.ArtifactFilenames[0] != "aura_artifact.txt" {
		t.Fatalf("ArtifactFilenames = %v", result.ArtifactFilenames)
	}
	if len(result.ArtifactSourceIDs) != 1 || result.ArtifactSourceIDs[0] != "src_0123456789abcdef" {
		t.Fatalf("ArtifactSourceIDs = %v", result.ArtifactSourceIDs)
	}
}

func TestDebugDocumentSendsAfterReturnsOnlyNewSends(t *testing.T) {
	b := &Bot{}
	b.recordDebugDocumentSend("old.txt", []byte("old"), "old")
	after := b.debugDocSeq.Load()
	b.recordDebugDocumentSend("aura_artifact.txt", []byte("hello"), "caption")

	sends := b.debugDocumentSendsAfter(after)
	if len(sends) != 1 {
		t.Fatalf("sends = %d, want 1: %+v", len(sends), sends)
	}
	if sends[0].Filename != "aura_artifact.txt" || sends[0].SizeBytes != 5 || sends[0].Caption != "caption" {
		t.Fatalf("send = %+v", sends[0])
	}
}

func TestDebugTextSmokeResultFromMessagesReportsMissingTool(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{Role: "assistant", Content: "5050"},
	})

	if result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = true, want false")
	}
	if !result.Contains5050 {
		t.Fatal("Contains5050 = false, want true from final text")
	}
}
