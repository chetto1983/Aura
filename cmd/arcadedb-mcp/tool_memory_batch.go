package main

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// MemoryBatchFactInput carries one bounded fact mutation without host authority fields.
type MemoryBatchFactInput struct {
	Subject           string                      `json:"subject"`
	SubjectKind       string                      `json:"subject_kind,omitempty"`
	Predicate         string                      `json:"predicate"`
	Object            string                      `json:"object"`
	ObjectKind        string                      `json:"object_kind,omitempty"`
	Statement         string                      `json:"statement"`
	ValidFrom         string                      `json:"valid_from,omitempty"`
	ValidTo           string                      `json:"valid_to,omitempty"`
	SupersedesFactKey string                      `json:"supersedes_fact_key,omitempty"`
	Source            MemoryUpsertFactWriteSource `json:"source"`
}

// MemoryBatchMergeInput names an entity folded into a surviving target.
type MemoryBatchMergeInput struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// MemoryBatchForgetInput names destructive fact evidence to remove.
type MemoryBatchForgetInput struct {
	SourceRunID        string `json:"source_run_id,omitempty"`
	Entity             string `json:"entity,omitempty"`
	Subject            string `json:"subject,omitempty"`
	Predicate          string `json:"predicate,omitempty"`
	Object             string `json:"object,omitempty"`
	KeepOrphanEntities bool   `json:"keep_orphan_entities,omitempty"`
}

// MemoryBatchOperationInput is the model-facing four-variant tagged union.
type MemoryBatchOperationInput struct {
	Type   string                  `json:"type"`
	Fact   *MemoryBatchFactInput   `json:"fact,omitempty"`
	Merge  *MemoryBatchMergeInput  `json:"merge,omitempty"`
	Forget *MemoryBatchForgetInput `json:"forget,omitempty"`
}

// MemoryBatchInput carries one idempotency key and ordered operations.
type MemoryBatchInput struct {
	IdempotencyKey string                      `json:"idempotency_key"`
	Operations     []MemoryBatchOperationInput `json:"operations"`
}

// MemoryBatchOutput exposes only the committed final result.
type MemoryBatchOutput struct {
	Applied    int                                   `json:"applied"`
	Replayed   bool                                  `json:"replayed"`
	Operations []arcadedb.MemoryBatchOperationResult `json:"operations"`
}

type memoryBatchApply func(
	context.Context,
	*arcadedb.Client,
	arcadedb.MemoryBatchActor,
	arcadedb.MemoryBatchRequest,
	time.Time,
) (arcadedb.MemoryBatchResult, error)

var errMemoryBatchToolNotImplemented = errors.New("memory_batch tool is not implemented")

func addMemoryBatchTool(*mcp.Server, *tenants, clock) {}

func memoryBatchHandler(*tenants, clock, memoryBatchApply) mcp.ToolHandlerFor[MemoryBatchInput, MemoryBatchOutput] {
	return func(context.Context, *mcp.CallToolRequest, MemoryBatchInput) (*mcp.CallToolResult, MemoryBatchOutput, error) {
		return nil, MemoryBatchOutput{}, errMemoryBatchToolNotImplemented
	}
}
