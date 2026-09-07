package swarm

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/idempotency"
)

func workerOperationContext(ctx context.Context, rc RunConfig, idx int, goal string) (context.Context, string, error) {
	if rc.ChildID != "" {
		// Durable jobs already have an independent operation, fenced by lease generation.
		return ctx, rc.ChildID, nil
	}
	parent, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return ctx, fmt.Sprintf("w%d", idx+1), nil
	}
	key := delegationIdempotencyKey(parent.Key.IdentityID, rc.ConvID,
		string(parent.Key.Scope)+":"+parent.Key.Key, idx, goal)
	childID := delegationChildID(key, idx)
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Version           string `json:"version"`
		WorkerKey         string `json:"worker_key"`
		ParentFingerprint string `json:"parent_fingerprint"`
	}{"swarm-worker-v1", key, idempotency.FingerprintHex(parent.Fingerprint)})
	if err != nil {
		return nil, childID, err
	}
	workerCtx, err := idempotency.WithOperation(ctx, idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: parent.Key.IdentityID, Scope: idempotency.ScopeSwarmDelegation, Key: key},
		Fingerprint: fingerprint,
		Correlation: parent.Correlation,
	})
	return workerCtx, childID, err
}
