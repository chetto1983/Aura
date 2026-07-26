package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func TestDynamicTailInclusionUsesSyntheticIdentity(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()
	if dynamicTailIncluded(nil, id) ||
		dynamicTailIncluded([]llm.Message{
			{Role: llm.RoleUser, DynamicTailID: id},
			{Role: llm.RoleUser, DynamicTailID: id},
		}, id) {
		t.Fatal("missing or duplicate dynamic recall counted as exact inclusion")
	}
	content := FenceDynamicRecall("fact")
	if !dynamicTailIncluded([]llm.Message{
		{Role: llm.RoleUser, Content: content},
		{Role: llm.RoleUser, Content: content},
		{Role: llm.RoleUser, Content: content, DynamicTailID: id},
	}, id) {
		t.Fatal("persisted duplicate content hid the uniquely marked dynamic recall")
	}
}
