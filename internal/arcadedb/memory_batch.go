package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
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

// CompileMemoryBatch validates and normalizes the complete request before a
// transaction is opened.
func CompileMemoryBatch(request MemoryBatchRequest, limits MemoryLimits) (CompiledMemoryBatch, error) {
	limits = limits.normalized()
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey == "" {
		return CompiledMemoryBatch{}, memoryBatchEnvelopeError(
			"invalid_idempotency_key", fmt.Errorf("idempotency key must be non-empty"))
	}
	if err := validateRuneLimit(
		"memory batch idempotency key", request.IdempotencyKey, limits.SourceRunIDRunes,
	); err != nil {
		return CompiledMemoryBatch{}, memoryBatchEnvelopeError("invalid_idempotency_key", err)
	}
	if len(request.Operations) == 0 {
		return CompiledMemoryBatch{}, memoryBatchEnvelopeError(
			"empty_batch", fmt.Errorf("operations must be non-empty"))
	}
	if len(request.Operations) > limits.MaintenanceBatch {
		return CompiledMemoryBatch{}, memoryBatchEnvelopeError("batch_too_large", fmt.Errorf(
			"operations exceeds %d items", limits.MaintenanceBatch))
	}

	normalized := make([]MemoryBatchOperation, len(request.Operations))
	for index, operation := range request.Operations {
		compiled, err := compileMemoryBatchOperation(operation, limits)
		if err != nil {
			return CompiledMemoryBatch{}, memoryBatchOperationError(index, "malformed_operation", err)
		}
		normalized[index] = compiled
	}
	canonical := MemoryBatchRequest{IdempotencyKey: request.IdempotencyKey, Operations: normalized}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return CompiledMemoryBatch{}, memoryBatchEnvelopeError("malformed_request", err)
	}
	digest := sha256.Sum256(encoded)
	return CompiledMemoryBatch{
		RequestHash: hex.EncodeToString(digest[:]), Operations: normalized,
	}, nil
}

func compileMemoryBatchOperation(
	operation MemoryBatchOperation,
	limits MemoryLimits,
) (MemoryBatchOperation, error) {
	payloads := 0
	if operation.Fact != nil {
		payloads++
	}
	if operation.Merge != nil {
		payloads++
	}
	if operation.Forget != nil {
		payloads++
	}
	if payloads != 1 {
		return MemoryBatchOperation{}, fmt.Errorf("exactly one operation payload is required")
	}

	switch operation.Type {
	case MemoryBatchUpsertFact, MemoryBatchSupersedeFact:
		if operation.Fact == nil || operation.Merge != nil || operation.Forget != nil {
			return MemoryBatchOperation{}, fmt.Errorf("%s requires only a fact payload", operation.Type)
		}
		fact := normalizeFact(*operation.Fact)
		if operation.Type == MemoryBatchUpsertFact && (fact.Supersedes || fact.TargetFactKey != "") {
			return MemoryBatchOperation{}, fmt.Errorf("upsert_fact cannot carry supersede controls")
		}
		fact.Supersedes = operation.Type == MemoryBatchSupersedeFact
		if err := fact.validate(limits); err != nil {
			return MemoryBatchOperation{}, err
		}
		return MemoryBatchOperation{Type: operation.Type, Fact: &fact}, nil
	case MemoryBatchMergeEntities:
		if operation.Merge == nil || operation.Fact != nil || operation.Forget != nil {
			return MemoryBatchOperation{}, fmt.Errorf("merge_entities requires only a merge payload")
		}
		source, target, err := normalizeMergeEntities(operation.Merge.Source, operation.Merge.Target)
		if err != nil {
			return MemoryBatchOperation{}, err
		}
		if err := validateRuneLimit("merge source", source, limits.EntityRunes); err != nil {
			return MemoryBatchOperation{}, err
		}
		if err := validateRuneLimit("merge target", target, limits.EntityRunes); err != nil {
			return MemoryBatchOperation{}, err
		}
		return MemoryBatchOperation{
			Type: operation.Type, Merge: &MemoryBatchMerge{Source: source, Target: target},
		}, nil
	case MemoryBatchForget:
		if operation.Forget == nil || operation.Fact != nil || operation.Merge != nil {
			return MemoryBatchOperation{}, fmt.Errorf("forget requires only a forget payload")
		}
		filter := normalizeForgetFilter(*operation.Forget)
		if filter.DryRun {
			return MemoryBatchOperation{}, fmt.Errorf("forget dry_run is not a mutation and is not valid in a batch")
		}
		if err := filter.validate(limits); err != nil {
			return MemoryBatchOperation{}, err
		}
		if _, _, err := filter.where(); err != nil {
			return MemoryBatchOperation{}, err
		}
		return MemoryBatchOperation{Type: operation.Type, Forget: &filter}, nil
	default:
		return MemoryBatchOperation{}, fmt.Errorf("unknown operation type %q", operation.Type)
	}
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
	IdentityID     string
	IdempotencyKey string
	RequestHash    string
	Result         MemoryBatchResult
}

type memoryBatchTransaction interface {
	LoadReceipt(context.Context, string) (*memoryBatchReceipt, error)
	LoadState(context.Context) (memoryBatchState, error)
	Persist(context.Context, memoryBatchState, memoryBatchState) error
	SaveReceipt(context.Context, string, memoryBatchReceipt) error
	Commit(context.Context) error
	Rollback(context.Context)
}

// memoryBatchCreatedStatements lists the statements this batch may write as NEW
// facts. Merge and forget operations never create one, so they contribute nothing
// to embed. A supersede still writes its replacement fact, so it counts.
func memoryBatchCreatedStatements(compiled CompiledMemoryBatch) []string {
	statements := make([]string, 0, len(compiled.Operations))
	for _, operation := range compiled.Operations {
		if operation.Fact == nil {
			continue
		}
		statements = append(statements, operation.Fact.Statement)
	}
	return statements
}

type memoryBatchBackend interface {
	Begin(context.Context, string) (memoryBatchTransaction, error)
	// EmbedStatements vectorizes the statements the batch is about to create, in
	// one call, before the transaction opens. Fail-soft: a missing key means that
	// statement has no vector, never that the batch should fail.
	EmbedStatements(context.Context, []string) map[string][]float64
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
	ctx context.Context,
	actor MemoryBatchActor,
	request MemoryBatchRequest,
	now time.Time,
	limits MemoryLimits,
	backend memoryBatchBackend,
) (MemoryBatchResult, error) {
	actor.IdentityID = strings.TrimSpace(actor.IdentityID)
	if actor.IdentityID == "" {
		return MemoryBatchResult{}, memoryBatchEnvelopeError(
			"unauthenticated", fmt.Errorf("authenticated identity must be non-empty"))
	}
	if actor.WriterRole != WriterParent && actor.WriterRole != WriterWorker {
		return MemoryBatchResult{}, memoryBatchEnvelopeError(
			"unauthorized_actor", fmt.Errorf("writer role must be %q or %q", WriterParent, WriterWorker))
	}
	compiled, err := CompileMemoryBatch(request, limits)
	if err != nil {
		return MemoryBatchResult{}, err
	}
	if err := authorizeMemoryBatch(actor, compiled); err != nil {
		return MemoryBatchResult{}, err
	}

	// Embed BEFORE the identity lock and the transaction: the embedder is an HTTP
	// sidecar, and one round trip serves every attempt because the vector depends
	// only on the statement text, which conflict retries do not change.
	embeddings := backend.EmbedStatements(ctx, memoryBatchCreatedStatements(compiled))

	unlock := memoryBatchIdentityLocks.lock(actor.IdentityID)
	defer unlock()
	receiptKey := memoryBatchReceiptKey(actor.IdentityID, request.IdempotencyKey)
	var lastErr error
	for attempt := 0; attempt <= maxWriteConflictRetries; attempt++ {
		result, retry, err := applyMemoryBatchAttempt(
			ctx, actor, request, compiled, now, limits, receiptKey, backend, embeddings,
		)
		if !retry {
			return result, err
		}
		lastErr = err
		if attempt == maxWriteConflictRetries {
			break
		}
		if err := waitMemoryBatchRetry(ctx, attempt+1); err != nil {
			return MemoryBatchResult{}, err
		}
	}
	return MemoryBatchResult{}, fmt.Errorf(
		"arcadedb: memory batch exhausted conflict retries: %w", lastErr)
}

func applyMemoryBatchAttempt(
	ctx context.Context,
	actor MemoryBatchActor,
	request MemoryBatchRequest,
	compiled CompiledMemoryBatch,
	now time.Time,
	limits MemoryLimits,
	receiptKey string,
	backend memoryBatchBackend,
	embeddings map[string][]float64,
) (MemoryBatchResult, bool, error) {
	tx, err := backend.Begin(ctx, actor.IdentityID)
	if err != nil {
		wrapped := fmt.Errorf("arcadedb: begin memory batch: %w", err)
		return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, false), wrapped
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	receipt, err := tx.LoadReceipt(ctx, receiptKey)
	if err != nil {
		wrapped := fmt.Errorf("arcadedb: read memory batch receipt: %w", err)
		return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, false), wrapped
	}
	if receipt != nil {
		if receipt.IdentityID != actor.IdentityID || receipt.RequestHash != compiled.RequestHash {
			return MemoryBatchResult{}, false, memoryBatchEnvelopeError(
				"idempotency_conflict", fmt.Errorf("idempotency key is already bound to a different request"))
		}
		if err := tx.Commit(ctx); err != nil {
			wrapped := fmt.Errorf("arcadedb: commit memory batch replay: %w", err)
			return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, true), wrapped
		}
		committed = true
		result := receipt.Result
		result.Replayed = true
		return result, false, nil
	}

	live, err := tx.LoadState(ctx)
	if err != nil {
		wrapped := fmt.Errorf("arcadedb: load memory batch state: %w", err)
		return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, false), wrapped
	}
	working := cloneMemoryBatchState(live)
	operationResults, err := applyCompiledMemoryBatch(working, compiled, now, limits, embeddings)
	if err != nil {
		return MemoryBatchResult{}, false, err
	}
	if err := tx.Persist(ctx, live, working); err != nil {
		wrapped := fmt.Errorf("arcadedb: persist memory batch: %w", err)
		return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, false), wrapped
	}
	result := MemoryBatchResult{
		RequestHash: compiled.RequestHash,
		Applied:     len(compiled.Operations),
		Operations:  operationResults,
	}
	if err := tx.SaveReceipt(ctx, receiptKey, memoryBatchReceipt{
		IdentityID: actor.IdentityID, IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		RequestHash: compiled.RequestHash, Result: result,
	}); err != nil {
		wrapped := fmt.Errorf("arcadedb: save memory batch receipt: %w", err)
		return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, false), wrapped
	}
	if err := tx.Commit(ctx); err != nil {
		wrapped := fmt.Errorf("arcadedb: commit memory batch: %w", err)
		return MemoryBatchResult{}, retryMemoryBatchFailure(ctx, err, true), wrapped
	}
	committed = true
	return result, false, nil
}

func retryMemoryBatchFailure(ctx context.Context, err error, commit bool) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if isTransientWriteConflict(err) {
		return true
	}
	if !commit {
		return false
	}
	var serverErr *ServerError
	return !errors.As(err, &serverErr)
}

func waitMemoryBatchRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(writeConflictBackoff(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("arcadedb: memory batch retry: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func authorizeMemoryBatch(actor MemoryBatchActor, compiled CompiledMemoryBatch) error {
	for index, operation := range compiled.Operations {
		switch operation.Type {
		case MemoryBatchUpsertFact, MemoryBatchSupersedeFact:
			if operation.Fact.Source.WriterRole != actor.WriterRole {
				return memoryBatchOperationError(index, "unauthorized_actor", fmt.Errorf(
					"fact writer role %q does not match authenticated actor %q",
					operation.Fact.Source.WriterRole, actor.WriterRole))
			}
			if operation.Type == MemoryBatchSupersedeFact && actor.WriterRole != WriterParent {
				return memoryBatchOperationError(index, "unauthorized_actor", fmt.Errorf(
					"worker may not supersede facts"))
			}
		case MemoryBatchMergeEntities, MemoryBatchForget:
			if actor.WriterRole != WriterParent {
				return memoryBatchOperationError(index, "unauthorized_actor", fmt.Errorf(
					"worker may not %s", operation.Type))
			}
		}
	}
	return nil
}

func memoryBatchEnvelopeError(code string, err error) *MemoryBatchError {
	return &MemoryBatchError{Index: -1, Code: code, Err: err}
}

func memoryBatchOperationError(index int, code string, err error) *MemoryBatchError {
	return &MemoryBatchError{Index: index, Code: code, Err: err}
}

func memoryBatchReceiptKey(identityID, idempotencyKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(identityID) + "\x00" + strings.TrimSpace(idempotencyKey)))
	return hex.EncodeToString(digest[:])
}

const memoryBatchLockStripes = 256

type memoryBatchLocks struct {
	stripes [memoryBatchLockStripes]sync.Mutex
}

func (locks *memoryBatchLocks) lock(identityID string) func() {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(identityID))
	lock := &locks.stripes[hash.Sum32()%memoryBatchLockStripes]
	lock.Lock()
	return lock.Unlock
}

var memoryBatchIdentityLocks memoryBatchLocks

type clientMemoryBatchBackend struct {
	client *Client
}
