package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MemoryBatchOperationType is one of the four HARN-05 mutation variants.
type MemoryBatchOperationType string

const (
	// MemoryBatchUpsertFact adds a fact or enriches an exact duplicate's provenance.
	MemoryBatchUpsertFact MemoryBatchOperationType = "upsert_fact"
	// MemoryBatchSupersedeFact closes one resolved fact before adding its replacement.
	MemoryBatchSupersedeFact MemoryBatchOperationType = "supersede_fact"
	// MemoryBatchMergeEntities folds one entity into another.
	MemoryBatchMergeEntities MemoryBatchOperationType = "merge_entities"
	// MemoryBatchForget removes matching fact evidence or facts.
	MemoryBatchForget MemoryBatchOperationType = "forget"
)

// MemoryBatchActor is derived by the authenticated host, never from model input.
type MemoryBatchActor struct {
	IdentityID string
	WriterRole WriterRole
}

// MemoryBatchMerge names the entity folded into the surviving target.
type MemoryBatchMerge struct {
	Source string
	Target string
}

// MemoryBatchOperation is a tagged union. Exactly the payload matching Type
// must be present.
type MemoryBatchOperation struct {
	Type   MemoryBatchOperationType
	Fact   *Fact
	Merge  *MemoryBatchMerge
	Forget *ForgetFilter
}

// MemoryBatchRequest carries one identity-bound idempotency key and ordered
// heterogeneous operations.
type MemoryBatchRequest struct {
	IdempotencyKey string
	Operations     []MemoryBatchOperation
}

// MemoryBatchOperationResult is the bounded outcome of one operation.
type MemoryBatchOperationResult struct {
	Type       MemoryBatchOperationType `json:"type"`
	Statement  string                   `json:"statement,omitempty"`
	Superseded int                      `json:"superseded,omitempty"`
	Moved      int                      `json:"moved,omitempty"`
	Dropped    int                      `json:"dropped,omitempty"`
	Facts      int                      `json:"facts,omitempty"`
	Entities   int                      `json:"entities,omitempty"`
}

// MemoryBatchResult is stored in the same transaction as the graph mutation.
type MemoryBatchResult struct {
	RequestHash string                       `json:"request_hash"`
	Applied     int                          `json:"applied"`
	Replayed    bool                         `json:"replayed"`
	Operations  []MemoryBatchOperationResult `json:"operations"`
}

// MemoryBatchError reports the deterministic first failing operation. Index is
// zero-based; -1 identifies an envelope or committed-receipt conflict.
type MemoryBatchError struct {
	Index int
	Code  string
	Err   error
}

func (e *MemoryBatchError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("arcadedb: memory batch operation %d (%s): %v; live state unchanged", e.Index, e.Code, e.Err)
}

func (e *MemoryBatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CompiledMemoryBatch is the normalized, request-hashed plan applied to an
// isolated working snapshot.
type CompiledMemoryBatch struct {
	// RequestHash is the canonical hash bound to the idempotency receipt.
	RequestHash string
	// Operations are normalized and safe to apply to an isolated snapshot.
	Operations []MemoryBatchOperation
}

var errMemoryBatchNotImplemented = errors.New("arcadedb: memory batch not implemented")

// CompileMemoryBatch validates and normalizes the complete request before a
// transaction is opened.
func CompileMemoryBatch(MemoryBatchRequest, MemoryLimits) (CompiledMemoryBatch, error) {
	return CompiledMemoryBatch{}, errMemoryBatchNotImplemented
}

type memoryBatchFact struct {
	RID       string
	Fact      Fact
	Sources   []FactSource
	ValidFrom time.Time
	ValidTo   time.Time
	CreatedAt time.Time
	ExpiredAt time.Time
	FactKey   string
	Embedding any
}

type memoryBatchState struct {
	Entities map[string]string
	Facts    map[string]memoryBatchFact
}

type memoryBatchReceipt struct {
	RequestHash string
	Result      MemoryBatchResult
}

type memoryBatchTransaction interface {
	LoadReceipt(context.Context, string) (*memoryBatchReceipt, error)
	LoadState(context.Context) (memoryBatchState, error)
	Persist(context.Context, memoryBatchState, memoryBatchState) error
	SaveReceipt(context.Context, string, memoryBatchReceipt) error
	Commit(context.Context) error
	Rollback(context.Context)
}

type memoryBatchBackend interface {
	Begin(context.Context, string) (memoryBatchTransaction, error)
}

// ApplyMemoryBatch applies a complete request through one identity-scoped
// ArcadeDB transaction.
func (c *Client) ApplyMemoryBatch(
	ctx context.Context,
	actor MemoryBatchActor,
	request MemoryBatchRequest,
	now time.Time,
) (MemoryBatchResult, error) {
	return applyMemoryBatch(ctx, actor, request, now, c.memoryLimits(), clientMemoryBatchBackend{client: c})
}

func applyMemoryBatch(
	context.Context,
	MemoryBatchActor,
	MemoryBatchRequest,
	time.Time,
	MemoryLimits,
	memoryBatchBackend,
) (MemoryBatchResult, error) {
	return MemoryBatchResult{}, errMemoryBatchNotImplemented
}

type clientMemoryBatchBackend struct {
	client *Client
}

func (clientMemoryBatchBackend) Begin(context.Context, string) (memoryBatchTransaction, error) {
	return nil, errMemoryBatchNotImplemented
}
