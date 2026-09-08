package agui

import (
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func TestAttachWorkerReportWithoutReasoning(t *testing.T) {
	snap := projectDisplaySnapshot([]llm.Message{
		{Role: llm.RoleUser, Content: "run the task"},
		{Role: llm.RoleAssistant, Content: "launch confirmed"},
		{Role: llm.RoleAssistant, Content: "internal report"},
		{Role: llm.RoleAssistant, Content: "consolidated answer"},
	})
	attachTurnReasoning(&snap, []conversations.TurnReasoning{
		{Seq: 2}, {Seq: 3, WorkerReport: true}, {Seq: 4},
	})
	for i, msg := range snap.Messages {
		if msg.WorkerReport != (i == 2) {
			t.Fatalf("message %d: workerReport=%v", i, msg.WorkerReport)
		}
	}
}
