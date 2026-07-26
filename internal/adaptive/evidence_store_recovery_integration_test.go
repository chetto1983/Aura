//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/google/uuid"
)

func TestEvidenceStoreAdmissionAllowsOnlyExactOperationRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(
		t,
		ctx,
		"aura_evidence_exact_retry",
		shippedMigrationSteps(t),
	)
	ownerID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		ownerID,
		"evidence-exact-retry-"+ownerID.String(),
	); err != nil {
		t.Fatalf("seed evidence owner: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability)
		 VALUES ($1, 'adaptive.evidence.seal')`,
		ownerID,
	); err != nil {
		t.Fatalf("grant evidence sealer capability: %v", err)
	}
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save evidence snapshot: %v", err)
	}
	cohort, artifacts := admissionEvidenceFixture(t, ownerID, snapshot)
	if err := NewCohortStore(pool, NewStore(pool, StoreConfig{})).
		Save(ctx, cohort); err != nil {
		t.Fatalf("save evidence cohort: %v", err)
	}
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	store := NewEvidenceStore(pool)
	sealer := EvidenceSealer{
		ActorID:    ownerID,
		Capability: "adaptive.evidence.seal",
	}
	operationA := admissionOperationContext(
		t,
		ctx,
		pool,
		"admission-operation-a",
		ownerID,
		cohort.ID(),
		artifacts,
	)
	first, err := store.SealCanaryAdmission(
		operationA,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	)
	if err != nil {
		t.Fatalf("seal operation A: %v", err)
	}
	firstCanonical, err := first.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize operation A admission: %v", err)
	}
	assertAdmissionRowCounts(t, ctx, migrationPool, ownerID, cohort.ID(), 7, 1)

	operation, ok := idempotency.OperationFromContext(operationA)
	if !ok {
		t.Fatal("admission operation context is unavailable")
	}
	registry := idempotency.New(pool, idempotency.Config{})
	if token, recovered, err := registry.RecoverExpired(
		ctx,
		idempotency.BeginRequest{
			Operation:   operation.Key,
			Fingerprint: operation.Fingerprint,
		},
	); err != nil || recovered || token != 0 {
		t.Fatalf(
			"recover unexpired admission operation = %d/%t, %v",
			token, recovered, err,
		)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE aura.idempotency_operations
		    SET lease_expires_at = clock_timestamp() - interval '1 second'
		  WHERE identity_id = $1
		    AND operation_scope = $2
		    AND operation_key = $3`,
		operation.Key.IdentityID,
		operation.Key.Scope,
		operation.Key.Key,
	); err != nil {
		t.Fatalf("expire crashed admission operation: %v", err)
	}
	changedFingerprint := operation.Fingerprint
	changedFingerprint[0] ^= 0xff
	if token, recovered, err := registry.RecoverExpired(
		ctx,
		idempotency.BeginRequest{
			Operation:   operation.Key,
			Fingerprint: changedFingerprint,
		},
	); err != nil || recovered || token != 0 {
		t.Fatalf(
			"recover changed admission operation = %d/%t, %v",
			token, recovered, err,
		)
	}
	recoveredToken, recovered, err := registry.RecoverExpired(
		ctx,
		idempotency.BeginRequest{
			Operation:   operation.Key,
			Fingerprint: operation.Fingerprint,
		},
	)
	if err != nil || !recovered {
		t.Fatalf(
			"recover crashed admission operation = %d/%t, %v",
			recoveredToken, recovered, err,
		)
	}
	staleResult := idempotency.ReplayResult{
		Body:       []byte(`{"stale":true}`),
		StatusCode: 200,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	}
	if err := registry.Complete(
		ctx,
		idempotency.CompleteRequest{
			Operation: operation.Key, Fingerprint: operation.Fingerprint,
			ClaimToken: operation.ClaimToken, Result: staleResult,
		},
	); !errors.Is(err, idempotency.ErrStaleTransition) {
		t.Fatalf("pre-recovery claimant Complete error = %v, want stale", err)
	}
	if err := registry.MarkIndeterminate(
		ctx,
		operation.Key,
		operation.Fingerprint,
		operation.ClaimToken,
	); !errors.Is(err, idempotency.ErrStaleTransition) {
		t.Fatalf(
			"pre-recovery claimant MarkIndeterminate error = %v, want stale",
			err,
		)
	}
	if _, err := store.SealCanaryAdmission(
		operationA,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	); err == nil {
		t.Fatal("pre-recovery claimant sealed admission after takeover")
	}
	operationA, err = idempotency.WithClaimToken(
		operationA,
		recoveredToken,
	)
	if err != nil {
		t.Fatalf("attach recovered claim token: %v", err)
	}

	retried, err := store.SealCanaryAdmission(
		operationA,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	)
	if err != nil {
		t.Fatalf("retry exact operation A: %v", err)
	}
	retriedCanonical, err := retried.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize retried operation A admission: %v", err)
	}
	if !bytes.Equal(firstCanonical, retriedCanonical) {
		t.Fatal("exact operation A retry changed admission bytes")
	}
	assertAdmissionRowCounts(t, ctx, migrationPool, ownerID, cohort.ID(), 7, 1)
	replayExpiresAt := time.Now().UTC().Add(time.Hour)
	if err := registry.Complete(
		ctx,
		idempotency.CompleteRequest{
			Operation:   operation.Key,
			Fingerprint: operation.Fingerprint,
			ClaimToken:  recoveredToken,
			Result: idempotency.ReplayResult{
				Body:       retriedCanonical,
				StatusCode: 200,
				ExpiresAt:  replayExpiresAt,
			},
		},
	); err != nil {
		t.Fatalf("complete recovered admission operation: %v", err)
	}
	replay, err := registry.Begin(
		ctx,
		idempotency.BeginRequest{
			Operation:   operation.Key,
			Fingerprint: operation.Fingerprint,
		},
	)
	if err != nil ||
		replay.Decision != idempotency.DecisionReplay ||
		replay.Replay == nil ||
		!jsonDocumentsEqual(replay.Replay.Body, retriedCanonical) {
		var replayBody []byte
		if replay.Replay != nil {
			replayBody = replay.Replay.Body
		}
		t.Fatalf(
			"recovered admission replay decision/error/bytes = %s/%v/%d:%d",
			replay.Decision,
			err,
			len(replayBody),
			len(retriedCanonical),
		)
	}

	operationB := admissionOperationContext(
		t,
		ctx,
		pool,
		"admission-operation-b",
		ownerID,
		cohort.ID(),
		artifacts,
	)
	if _, err := store.SealCanaryAdmission(
		operationB,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	); !errors.Is(err, ErrCanaryAdmissionConflict) {
		t.Fatalf("operation B error = %v, want ErrCanaryAdmissionConflict", err)
	}
	assertAdmissionRowCounts(t, ctx, migrationPool, ownerID, cohort.ID(), 7, 1)
}
