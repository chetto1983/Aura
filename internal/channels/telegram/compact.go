package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type compactService interface {
	Preview(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
	Restore(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
}

func dispatchCompact(ctx context.Context, service compactService, conversationID, actorID string, streaming bool, arg string) string {
	if service == nil || conversationID == "" || actorID == "" {
		return "Compattazione non disponibile."
	}
	fields := strings.Fields(strings.TrimSpace(arg))
	action := "compact"
	if len(fields) > 0 {
		action = strings.ToLower(fields[0])
	}
	checkpointID := ""
	if len(fields) > 1 {
		checkpointID = fields[1]
	}
	if action == "restore" && streaming {
		return "Attendi la fine della risposta del modello prima di ripristinare un checkpoint."
	}
	req := runner.CompactRequest{OperationID: uuid.NewString(), ConversationID: conversationID, BranchID: "root", CheckpointID: checkpointID, ActorID: actorID, Trigger: runner.CompactTriggerManual, SafePoint: action == "restore"}
	var result runner.CompactPreview
	var err error
	if action == "restore" {
		if checkpointID == "" {
			return "Uso: /compact restore <checkpoint-id>"
		}
		result, err = service.Restore(ctx, req)
	} else {
		result, err = service.Preview(ctx, req)
	}
	if err != nil {
		return "Compattazione non disponibile, riprova."
	}
	return fmt.Sprintf("Compattazione %s: stato %s, checkpoint %s, precedente %s.", action, result.Status, result.CheckpointID, result.PriorCheckpointID)
}
