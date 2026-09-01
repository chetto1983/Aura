package main

import (
	"context"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

type chatReasoningMemory struct {
	sink      runner.ReasoningGraphSink
	retention runner.ReasoningRetentionStore
	deletion  runner.ReasoningDeletionStore
}

type tenantMemoryCaptureSink struct {
	clients *arcadedb.TenantClients
}

func (s tenantMemoryCaptureSink) ApplyAcceptedCapture(
	ctx context.Context,
	capture arcadedb.AcceptedCapture,
) error {
	client, err := s.clients.For(ctx, capture.IdentityID)
	if err != nil {
		return err
	}
	return client.ApplyAcceptedCapture(ctx, capture)
}

func buildMemoryCaptureQueue(cfg *config.Config) *runner.MemoryCaptureQueue {
	clients := newChatTenantClients(cfg)
	if clients == nil {
		return nil
	}
	return runner.NewMemoryCaptureQueue(
		tenantMemoryCaptureSink{clients: clients}, runner.MemoryCaptureQueueConfig{},
	)
}

type tenantReasoningMemory struct {
	clients *arcadedb.TenantClients
	policy  arcadedb.ReasoningRetentionPolicy
}

func (s *tenantReasoningMemory) UpsertReasoningTrace(
	ctx context.Context,
	trace arcadedb.ReasoningTrace,
) error {
	trace, err := trace.SetTerminalExpiry(s.policy, time.Time{})
	if err != nil {
		return err
	}
	client, err := s.clients.For(ctx, trace.IdentityID)
	if err != nil {
		return err
	}
	return client.UpsertReasoningTrace(ctx, trace)
}

func (s *tenantReasoningMemory) DeleteExpiredReasoning(
	ctx context.Context,
	identityID string,
	now time.Time,
	limit int,
) (int, error) {
	client, err := s.clients.For(ctx, identityID)
	if err != nil {
		return 0, err
	}
	return client.DeleteExpiredReasoning(ctx, identityID, now, limit)
}

func (s *tenantReasoningMemory) DeleteReasoningBySource(
	ctx context.Context,
	selector arcadedb.ReasoningDeleteSelector,
) (int, error) {
	client, err := s.clients.For(ctx, selector.IdentityID)
	if err != nil {
		return 0, err
	}
	return client.DeleteReasoningBySource(ctx, selector)
}

func newChatReasoningMemory(cfg *config.Config) *chatReasoningMemory {
	if cfg == nil || strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return nil
	}
	clients := newChatTenantClients(cfg)
	if clients == nil {
		return nil
	}
	store := &tenantReasoningMemory{
		clients: clients,
		policy: arcadedb.ReasoningRetentionPolicy{
			SuccessTTL: cfg.Retention.ReasoningSuccessTTL,
			FailedTTL:  cfg.Retention.ReasoningFailedTTL,
		},
	}
	return &chatReasoningMemory{sink: store, retention: store, deletion: store}
}

func wireChatReasoningMemory(deps *runner.Deps, memory *chatReasoningMemory) {
	if deps == nil || memory == nil {
		return
	}
	deps.ReasoningGraphSink = memory.sink
	deps.ReasoningDeletion = memory.deletion
}
