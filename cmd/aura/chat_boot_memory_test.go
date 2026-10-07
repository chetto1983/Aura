package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
)

func TestWireChatTurnRecallBindsMemoryOnlyWhenConfigured(t *testing.T) {
	decisions := &conversations.Store{}
	var without runner.Deps
	wireChatTurnRecall(&without, nil, decisions)
	if without.TurnDecisions == nil || without.TurnRecall != nil {
		t.Fatalf("without memory: decisions %v, recall %v; want decisions only", without.TurnDecisions, without.TurnRecall)
	}
	var with runner.Deps
	wireChatTurnRecall(&with, new(arcadedb.TenantClients), decisions)
	if _, ok := with.TurnRecall.(tenantTurnRecall); !ok {
		t.Fatalf("with memory: recall = %T, want tenantTurnRecall", with.TurnRecall)
	}
}
