package telegram

import (
	"testing"

	"github.com/aura/aura/internal/orchestration"
)

func TestRuntimeToolsetForTurnRemovesSearchMemoryWhenDocumentCapsuleExists(t *testing.T) {
	got, err := runtimeToolsetForTurn(orchestration.ToolsetDocument, turnRetrievalCapsule{
		Text:                 "## Retrieval Capsule\n\n### Evidence\nx",
		HasEvidence:          true,
		SuppressSearchMemory: true,
	}).Tools(orchestration.ToolsetContext{
		Toolset: orchestration.ToolsetDocument,
		Availability: orchestration.Availability{
			Swarm:          true,
			WorkspaceFiles: true,
		},
	})
	if err != nil {
		t.Fatalf("runtimeToolsetForTurn: %v", err)
	}
	if containsString(got, "search_memory") {
		t.Fatalf("document capsule toolset exposed search_memory: %+v", got)
	}
	for _, want := range []string{"create_docx", "create_xlsx", "create_pdf"} {
		if !containsString(got, want) {
			t.Fatalf("document capsule toolset missing %q: %+v", want, got)
		}
	}
}

func TestRuntimeToolsetForTurnKeepsSearchMemoryWithoutCapsule(t *testing.T) {
	got, err := runtimeToolsetForTurn(orchestration.ToolsetDocument, turnRetrievalCapsule{}).Tools(orchestration.ToolsetContext{
		Toolset: orchestration.ToolsetDocument,
	})
	if err != nil {
		t.Fatalf("runtimeToolsetForTurn: %v", err)
	}
	if !containsString(got, "search_memory") {
		t.Fatalf("document toolset without capsule should expose search_memory: %+v", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
