package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// scheduledMessageDrafts stores a scheduled message's draft against the trusted tool the bridge
// mounted for its channel, found by recipe and never by name (Registry.ByTrustedRecipe). It is
// the task tool's taskMessageDrafts seam: the tools package cannot import messagedrafts.
type scheduledMessageDrafts struct {
	store *messagedrafts.Store
	reg   *tools.Registry
}

// scheduledMessageChannels maps a message's channel to the recipe tool that sends it, and to
// the words the operator reads when that tool is not mounted.
var scheduledMessageChannels = map[string]struct{ recipe, tool, missing string }{
	"whatsapp": {"recipe:whatsapp", "send_message", "WhatsApp is not connected"},
	"email":    {"recipe:calendar", "calendar", "email is not connected"},
}

func (d scheduledMessageDrafts) CreateScheduledMessageDraft(ctx context.Context, in tools.ScheduledMessageDraft) (string, error) {
	channel, ok := scheduledMessageChannels[in.Channel]
	if !ok {
		return "", fmt.Errorf("scheduled message: unknown channel %q", in.Channel)
	}
	tool, ok := d.reg.ByTrustedRecipe(channel.recipe, channel.tool)
	if !ok {
		return "", errors.New(channel.missing)
	}
	spec := tool.Spec()
	target, ok := messagedrafts.TrustedOutboundTarget(spec, in.Args)
	if !ok {
		return "", fmt.Errorf("scheduled message: %s does not send these arguments", spec.Name)
	}
	draft, err := d.store.Create(ctx, messagedrafts.DraftInput{
		IdentityID: identityctx.IdentityID(ctx), ConversationID: in.ConversationID, ToolCallID: in.ToolCallID,
		Target: target, RegisteredToolName: spec.Name, OriginalArgs: in.Args,
		ExpiresAt:       in.SendAt.Add(tools.ScheduledMessageGrace),
		ScheduledTaskID: in.TaskID, SendAt: in.SendAt,
	})
	if err != nil {
		return "", err
	}
	return draft.ID, nil
}
