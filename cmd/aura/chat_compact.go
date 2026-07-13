package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type conversationCompactBackend struct{ store *conversations.Store }

func (b conversationCompactBackend) PreviewCompaction(ctx context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	p, err := b.store.PreviewCompaction(ctx, req.ConversationID, req.BranchID)
	return runner.CompactPreview{CheckpointID: p.CheckpointID, PriorCheckpointID: p.PriorCheckpointID, Summary: p.Summary}, err
}

func (b conversationCompactBackend) RestoreCompaction(ctx context.Context, req runner.CompactRequest) error {
	return b.store.RestoreCompaction(ctx, req.ConversationID, req.BranchID, req.CheckpointID, req.OperationID, req.ActorID)
}

func newConversationCompactCoordinator(store *conversations.Store) *runner.CompactCoordinator {
	if store == nil {
		return runner.NewCompactCoordinator(nil, false)
	}
	return runner.NewCompactCoordinator(conversationCompactBackend{store: store}, false)
}

type compactCommandService interface {
	Preview(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
	Restore(context.Context, runner.CompactRequest) (runner.CompactPreview, error)
}

func dispatchCompactCommand(ctx context.Context, service compactCommandService, conversationID, actorID string, streaming bool, line string, out io.Writer) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/compact" {
		return false
	}
	if service == nil || conversationID == "" || actorID == "" {
		_, _ = fmt.Fprintln(out, "Compaction unavailable.")
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
		_, _ = fmt.Fprintln(out, "Wait for the current model response before restoring a checkpoint.")
		return true
	}
	req := runner.CompactRequest{OperationID: uuid.NewString(), ConversationID: conversationID, BranchID: "root", CheckpointID: checkpointID, ActorID: actorID, Trigger: runner.CompactTriggerManual, SafePoint: action == "restore"}
	var result runner.CompactPreview
	var err error
	if action == "restore" {
		if checkpointID == "" {
			_, _ = fmt.Fprintln(out, "Usage: /compact restore <checkpoint-id>")
			return true
		}
		result, err = service.Restore(ctx, req)
	} else {
		result, err = service.Preview(ctx, req)
	}
	if err != nil {
		_, _ = fmt.Fprintln(out, "Compaction unavailable.")
		return true
	}
	_, _ = fmt.Fprintf(out, "Compaction %s: status=%s checkpoint=%s prior=%s activated=%t\n", action, result.Status, result.CheckpointID, result.PriorCheckpointID, result.Activated)
	return true
}
