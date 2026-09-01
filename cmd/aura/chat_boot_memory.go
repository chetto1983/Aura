package main

import (
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

type chatReasoningMemory struct {
	sink      runner.ReasoningGraphSink
	retention runner.ReasoningRetentionStore
}

func newChatReasoningMemory(*config.Config) *chatReasoningMemory {
	return nil
}
