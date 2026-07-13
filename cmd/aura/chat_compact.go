package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type compactMessageCatalog struct{ unavailable, wait, usage, result string }

var compactMessagesEnglish = compactMessageCatalog{"Compaction unavailable.", "Wait for the current model response before restoring a checkpoint.", "Usage: /compact restore <checkpoint-id>", "Compaction %s: status=%s checkpoint=%s prior=%s activated=%t\n"}
var compactMessagesItalian = compactMessageCatalog{"Compattazione non disponibile.", "Attendi la fine della risposta del modello prima di ripristinare un checkpoint.", "Uso: /compact restore <checkpoint-id>", "Compattazione %s: stato=%s checkpoint=%s precedente=%s attivata=%t\n"}

func compactMessagesForLocale(locale string) compactMessageCatalog {
	if strings.HasPrefix(strings.ToLower(locale), "it") {
		return compactMessagesItalian
	}
	return compactMessagesEnglish
}

type conversationCompactBackend struct{ store *conversations.Store }

func (b conversationCompactBackend) PreviewCompaction(ctx context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	p, err := b.store.PreviewCompaction(ctx, req.ConversationID, req.BranchID)
	return runner.CompactPreview{CheckpointID: p.CheckpointID, PriorCheckpointID: p.PriorCheckpointID, Summary: p.Summary}, err
}

func (b conversationCompactBackend) RestoreCompaction(ctx context.Context, req runner.CompactRequest) error {
	return b.store.RestoreCompaction(ctx, req.ConversationID, req.BranchID, req.CheckpointID, req.OperationID, req.ActorID)
}

func newConversationCompactCoordinator(store *conversations.Store, readers ...runner.CompactionEffectiveReader) *runner.CompactCoordinator {
	if store == nil {
		return runner.NewCompactCoordinator(nil, false)
	}
	if len(readers) > 0 && readers[0] != nil {
		return runner.NewPersistedCompactCoordinator(conversationCompactBackend{store: store}, readers[0])
	}
	return runner.NewCompactCoordinator(conversationCompactBackend{store: store}, false)
}

type compactCommandService interface {
	Preview(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
	Restore(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
}

func dispatchCompactCommand(ctx context.Context, service compactCommandService, conversationID, actorID string, streaming bool, line string, out io.Writer) bool {
	messages := compactMessagesForLocale(os.Getenv("LANG"))
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/compact" {
		return false
	}
	if service == nil || conversationID == "" || actorID == "" {
		_, _ = fmt.Fprintln(out, messages.unavailable)
		return true
	}
	action := "compact"
	if len(fields) > 1 {
		action = strings.ToLower(fields[1])
	}
	checkpointID := ""
	if len(fields) > 2 {
		checkpointID = fields[2]
	}
	if action == "restore" && streaming {
		_, _ = fmt.Fprintln(out, messages.wait)
		return true
	}
	req := runner.CompactRequest{OperationID: uuid.NewString(), ConversationID: conversationID, BranchID: "root", CheckpointID: checkpointID, ActorID: actorID, Trigger: runner.CompactTriggerManual, SafePoint: action == "restore"}
	var result runner.CompactPreview
	var err error
	if action == "restore" {
		if checkpointID == "" {
			_, _ = fmt.Fprintln(out, messages.usage)
			return true
		}
		result, err = service.Restore(ctx, req)
	} else {
		result, err = service.Preview(ctx, req)
	}
	if err != nil {
		_, _ = fmt.Fprintln(out, messages.unavailable)
		return true
	}
	_, _ = fmt.Fprintf(out, messages.result, action, result.Status, result.CheckpointID, result.PriorCheckpointID, result.Activated)
	return true
}
