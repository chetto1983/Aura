package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type compactMessageCatalog struct{ unavailable, wait, usage, retry, result string }

var compactMessagesItalian = compactMessageCatalog{"Compattazione non disponibile.", "Attendi la fine della risposta del modello prima di ripristinare un checkpoint.", "Uso: /compact restore <checkpoint-id>", "Compattazione non disponibile, riprova.", "Compattazione %s: stato %s, checkpoint %s, precedente %s."}

type compactService interface {
	Preview(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
	Restore(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
}

func dispatchCompact(ctx context.Context, service compactService, conversationID, actorID string, streaming bool, arg string) string {
	if service == nil || conversationID == "" || actorID == "" {
		return compactMessagesItalian.unavailable
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
		return compactMessagesItalian.wait
	}
	req := runner.CompactRequest{OperationID: uuid.NewString(), ConversationID: conversationID, BranchID: "root", CheckpointID: checkpointID, ActorID: actorID, Trigger: runner.CompactTriggerManual, SafePoint: action == "restore"}
	var result runner.CompactPreview
	var err error
	if action == "restore" {
		if checkpointID == "" {
			return compactMessagesItalian.usage
		}
		result, err = service.Restore(ctx, req)
	} else {
		result, err = service.Preview(ctx, req)
	}
	if err != nil {
		return compactMessagesItalian.retry
	}
	return fmt.Sprintf(compactMessagesItalian.result, action, result.Status, result.CheckpointID, result.PriorCheckpointID)
}
