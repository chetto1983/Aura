package main

import (
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

func newChatConversationProjector(
	_ *config.Config,
	_ runner.ConversationProjectionSource,
) *runner.ConversationProjector {
	return nil
}
