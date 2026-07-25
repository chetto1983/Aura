//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFocalCohortMigration0071LiveRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, _ := schema2MigrationDatabase(t, ctx, "aura_focal_cohort_rows", 71)
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, owner)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save focal cohort snapshot: %v", err)
	}

	t.Run("accepts canonical future cohort backed by its snapshot", func(t *testing.T) {
		cohort := focalCohortForSnapshot(
			t,
			snapshot,
			time.Now().Add(time.Hour),
			[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
		)
		if err := insertFocalCohort(ctx, pool, cohort); err != nil {
			t.Fatalf("insert valid focal cohort: %v", err)
		}
	})

	t.Run("accepts the canonical extended identifier alphabet", func(t *testing.T) {
		spec := focalCohortSpecForSnapshot(
			snapshot,
			time.Now().Add(time.Hour),
			[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
		)
		spec.ExperimentID = "knowledge/depth:2026@canary+1"
		if err := insertFocalCohort(ctx, pool, mustFocalCohort(t, spec)); err != nil {
			t.Fatalf("insert cohort with canonical extended identifier: %v", err)
		}
	})

	t.Run("rejects a past cutoff", func(t *testing.T) {
		cohort := focalCohortForSnapshot(
			t,
			snapshot,
			time.Now().Add(-time.Minute),
			[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
		)
		assertCohortConstraintError(t, insertFocalCohort(ctx, pool, cohort), "cutoff must be after server creation time")
	})

	t.Run("rejects snapshot scope mismatches", func(t *testing.T) {
		for _, testCase := range []struct {
			name   string
			mutate func(*CohortSpec)
		}{
			{
				name: "sha",
				mutate: func(spec *CohortSpec) {
					spec.Scope.SnapshotSHA256 = strings.Repeat("00", 32)
				},
			},
			{
				name: "provider",
				mutate: func(spec *CohortSpec) {
					spec.Scope.ProviderID = "other-provider"
				},
			},
			{
				name: "model",
				mutate: func(spec *CohortSpec) {
					spec.Scope.ModelID = "other-model"
				},
			},
			{
				name: "policy",
				mutate: func(spec *CohortSpec) {
					spec.Scope.PolicyVersion = "other-policy"
				},
			},
			{
				name: "domain",
				mutate: func(spec *CohortSpec) {
					spec.Predicate.Domain = DomainReasoning
					spec.Predicate.Point = PointReasoning
				},
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				spec := focalCohortSpecForSnapshot(
					snapshot,
					time.Now().Add(time.Hour),
					[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
				)
				testCase.mutate(&spec)
				assertForeignKeyError(t, insertFocalCohort(ctx, pool, mustFocalCohort(t, spec)))
			})
		}
	})

	t.Run("rejects claims after cutoff", func(t *testing.T) {
		cutoff := time.Now().Add(2 * time.Second)
		cohort := focalCohortForSnapshot(
			t,
			snapshot,
			cutoff,
			[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
		)
		if err := insertFocalCohort(ctx, pool, cohort); err != nil {
			t.Fatalf("insert future focal cohort: %v", err)
		}
		time.Sleep(time.Until(cohort.Cutoff()) + 100*time.Millisecond)
		_, _, err := insertFocalCohortClaim(ctx, pool, cohort, newFocalCohortClaim(t, ctx, pool, cohort, true, true))
		assertCohortConstraintError(t, err, "claim is after cohort cutoff")
	})

	t.Run("overrides supplied claim times and enforces frozen block keys", func(t *testing.T) {
		cohort := focalCohortForSnapshot(
			t,
			snapshot,
			time.Now().Add(time.Hour),
			[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
		)
		if err := insertFocalCohort(ctx, pool, cohort); err != nil {
			t.Fatalf("insert focal cohort: %v", err)
		}
		before := time.Now()
		claimedAt, blockStart, err := insertFocalCohortClaim(
			ctx,
			pool,
			cohort,
			newFocalCohortClaim(t, ctx, pool, cohort, true, true),
		)
		if err != nil {
			t.Fatalf("insert matching claim: %v", err)
		}
		after := time.Now()
		if claimedAt.Before(before) || claimedAt.After(after) {
			t.Fatalf("server claimed_at = %s, want time in [%s, %s]", claimedAt, before, after)
		}
		wantBlockStart := time.Unix(claimedAt.Unix()/3600*3600, 0)
		if !blockStart.Equal(wantBlockStart) {
			t.Fatalf("server time_block_start = %s, want %s", blockStart, wantBlockStart)
		}
		_, _, err = insertFocalCohortClaim(ctx, pool, cohort, newFocalCohortClaim(t, ctx, pool, cohort, false, false))
		assertCohortConstraintError(t, err, "block keys do not match cohort admission")

		withoutSessionOrEpisode := focalCohortForSnapshot(
			t,
			snapshot,
			time.Now().Add(time.Hour),
			[]BlockKey{BlockOwner, BlockTimeBlock},
		)
		if err := insertFocalCohort(ctx, pool, withoutSessionOrEpisode); err != nil {
			t.Fatalf("insert no-session focal cohort: %v", err)
		}
		if _, _, err := insertFocalCohortClaim(
			ctx,
			pool,
			withoutSessionOrEpisode,
			newFocalCohortClaim(t, ctx, pool, withoutSessionOrEpisode, false, false),
		); err != nil {
			t.Fatalf("insert claim without frozen optional blocks: %v", err)
		}
		_, _, err = insertFocalCohortClaim(
			ctx,
			pool,
			withoutSessionOrEpisode,
			newFocalCohortClaim(t, ctx, pool, withoutSessionOrEpisode, true, false),
		)
		assertCohortConstraintError(t, err, "block keys do not match cohort admission")
	})

	t.Run("enforces every cohort claim uniqueness key", func(t *testing.T) {
		cohort := focalCohortForSnapshot(
			t,
			snapshot,
			time.Now().Add(time.Hour),
			[]BlockKey{BlockOwner, BlockTimeBlock},
		)
		if err := insertFocalCohort(ctx, pool, cohort); err != nil {
			t.Fatalf("insert focal cohort: %v", err)
		}
		claim := newFocalCohortClaim(t, ctx, pool, cohort, false, false)
		if _, _, err := insertFocalCohortClaim(ctx, pool, cohort, claim); err != nil {
			t.Fatalf("insert unique claim: %v", err)
		}
		for _, target := range []string{
			"(cohort_id, request_id)",
			"(cohort_id, evaluation_conversation_id)",
			"(cohort_id, assignment_id)",
		} {
			inserted, err := insertFocalCohortClaimOnConflict(ctx, pool, cohort, claim, target)
			if err != nil {
				t.Fatalf("insert duplicate on conflict %s: %v", target, err)
			}
			if inserted {
				t.Fatalf("duplicate claim inserted despite unique target %s", target)
			}
		}
	})
}

func TestFocalCohortMigration0071RejectsMalformedRawArtifacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, _ := schema2MigrationDatabase(t, ctx, "aura_focal_cohort_raw", 71)
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, owner)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save focal cohort snapshot: %v", err)
	}
	cohort := focalCohortForSnapshot(t, snapshot, time.Now().Add(time.Hour), []BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock})

	for _, testCase := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"arms unordered", func(d map[string]any) { a := rawArray(d, "arms"); a[0], a[1] = a[1], a[0] }},
		{"arms duplicate", func(d map[string]any) { rawArray(d, "arms")[1].(map[string]any)["arm_id"] = "baseline" }},
		{"arms unsafe lowercase", func(d map[string]any) { rawArray(d, "arms")[0].(map[string]any)["arm_id"] = "token-arm" }},
		{"arms private extra field", func(d map[string]any) { rawArray(d, "arms")[0].(map[string]any)["raw_prompt"] = "private" }},
		{"arms unsupported", func(d map[string]any) {
			a := rawArray(d, "arms")
			a[0].(map[string]any)["probability"], a[1].(map[string]any)["probability"] = 0.0, 1.0
		}},
		{"arms sum", func(d map[string]any) { rawArray(d, "arms")[0].(map[string]any)["probability"] = 0.4 }},
		{"actions unordered", func(d map[string]any) { a := rawArray(d, "actions"); a[0], a[1] = a[1], a[0] }},
		{"actions duplicate", func(d map[string]any) { rawArray(d, "actions")[1].(map[string]any)["action_id"] = "graph_expansion" }},
		{"actions unsafe lowercase", func(d map[string]any) { rawArray(d, "actions")[0].(map[string]any)["action_id"] = "content-dump" }},
		{"actions static required", func(d map[string]any) { rawArray(d, "actions")[1].(map[string]any)["action_id"] = "sparse" }},
		{"actions unsupported", func(d map[string]any) {
			a := rawArray(d, "actions")
			a[0].(map[string]any)["probability"], a[1].(map[string]any)["probability"] = 0.0, 0.75
		}},
		{"actions sum", func(d map[string]any) { rawArray(d, "actions")[0].(map[string]any)["probability"] = 0.1 }},
		{"evaluators unordered", func(d map[string]any) { a := rawArray(d, "evaluators"); a[0], a[1] = a[1], a[0] }},
		{"evaluators duplicate", func(d map[string]any) { rawArray(d, "evaluators")[1] = rawArray(d, "evaluators")[0] }},
		{"evaluators ineligible", func(d map[string]any) { rawArray(d, "evaluators")[0].(map[string]any)["kind"] = "self_report" }},
		{"evaluators unsafe identifier", func(d map[string]any) {
			rawArray(d, "evaluators")[0].(map[string]any)["id"] = "private-summary-evaluator"
		}},
		{"evaluators invalid provenance SHA", func(d map[string]any) { rawArray(d, "evaluators")[0].(map[string]any)["provenance_sha256"] = "nope" }},
		{"quality endpoint kind", func(d map[string]any) { rawObject(d, "primary_quality")["kind"] = "binary_harm" }},
		{"quality endpoint ID", func(d map[string]any) { rawObject(d, "primary_quality")["id"] = "credential-quality" }},
		{"quality endpoint evaluator", func(d map[string]any) { rawObject(rawObject(d, "primary_quality"), "evaluator")["id"] = "unregistered" }},
		{"harm endpoint kind", func(d map[string]any) { rawObject(d, "primary_harm")["kind"] = "binary_quality" }},
		{"harm endpoint evaluator", func(d map[string]any) { rawObject(rawObject(d, "primary_harm"), "evaluator")["id"] = "unregistered" }},
		{"power baseline feasibility", func(d map[string]any) { rawObject(d, "power")["baseline_quality_rate"] = 1.0 }},
		{"power lift", func(d map[string]any) { rawObject(d, "power")["minimum_detectable_lift"] = 0.0 }},
		{"power members", func(d map[string]any) { rawObject(d, "power")["minimum_members"] = 6000.0 }},
		{"power fractional members", func(d map[string]any) { rawObject(d, "power")["minimum_members"] = 2500.5 }},
		{"power support", func(d map[string]any) { rawObject(d, "power")["minimum_arm_support"] = 0.6 }},
		{"power SHA", func(d map[string]any) { rawObject(d, "power")["simulation_sha256"] = "nope" }},
		{"margins feasibility", func(d map[string]any) { rawObject(d, "margins")["minimum_quality_uplift"] = 0.5 }},
		{"censoring bounds", func(d map[string]any) { rawObject(d, "censoring")["max_censor_rate"] = 1.0 }},
		{"admission block key", func(d map[string]any) { rawArray(rawObject(d, "admission"), "block_keys")[0] = "tenant" }},
		{"admission sorted", func(d map[string]any) {
			a := rawArray(rawObject(d, "admission"), "block_keys")
			a[0], a[1] = a[1], a[0]
		}},
		{"admission duplicate", func(d map[string]any) { rawArray(rawObject(d, "admission"), "block_keys")[1] = "episode" }},
		{"admission time block", func(d map[string]any) { rawArray(rawObject(d, "admission"), "block_keys")[3] = "owner" }},
		{"admission seconds", func(d map[string]any) { rawObject(d, "admission")["time_block_seconds"] = 0.0 }},
		{"admission interference", func(d map[string]any) { rawObject(d, "admission")["interference"] = "unmodelled" }},
		{"looks ordered", func(d map[string]any) { a := rawArray(d, "looks"); a[0], a[1] = a[1], a[0] }},
		{"looks positive", func(d map[string]any) { rawArray(d, "looks")[0].(map[string]any)["alpha"] = 0.0 }},
		{"looks alpha budget", func(d map[string]any) { rawArray(d, "looks")[4].(map[string]any)["alpha"] = 0.03 }},
		{"scope provider type", func(d map[string]any) { rawObject(d, "scope")["provider_id"] = 1.0 }},
		{"predicate ordinal type", func(d map[string]any) { rawObject(d, "predicate")["ordinal"] = "1" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			artifact, artifactJSON := malformedRawCohortArtifact(t, cohort, testCase.mutate)
			assertCohortConstraintError(t, insertRawFocalCohort(ctx, pool, cohort, artifact, artifactJSON), "adaptive_focal_cohorts_artifact_scope_check")
		})
	}

	t.Run("duplicate raw JSON object key", func(t *testing.T) {
		artifact, err := cohort.canonicalArtifact()
		if err != nil {
			t.Fatal(err)
		}
		artifact = bytes.Replace(artifact, []byte(`"experiment_id":`), []byte(`"experiment_id":"`+cohort.ExperimentID()+`","experiment_id":`), 1)
		var artifactJSON json.RawMessage
		if err := json.Unmarshal(artifact, &artifactJSON); err != nil {
			t.Fatal(err)
		}
		assertCohortConstraintError(t, insertRawFocalCohort(ctx, pool, cohort, artifact, artifactJSON), "adaptive_focal_cohorts_artifact_scope_check")
	})
}

func rawObject(document map[string]any, key string) map[string]any {
	return document[key].(map[string]any)
}
func rawArray(document map[string]any, key string) []any { return document[key].([]any) }

func malformedRawCohortArtifact(t *testing.T, cohort *FocalCohort, mutate func(map[string]any)) ([]byte, json.RawMessage) {
	t.Helper()
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(artifact, &document); err != nil {
		t.Fatal(err)
	}
	mutate(document)
	artifact, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, json.RawMessage(artifact)
}

func insertRawFocalCohort(ctx context.Context, pool *pgxpool.Pool, cohort *FocalCohort, artifact []byte, artifactJSON json.RawMessage) error {
	scope, predicate := cohort.Scope(), cohort.Predicate()
	snapshotSHA256, err := hex.DecodeString(scope.SnapshotSHA256)
	if err != nil {
		return err
	}
	predicateSHA256, err := hex.DecodeString(cohort.PredicateSHA256())
	if err != nil {
		return err
	}
	digest := sha256.Sum256(artifact)
	id := uuid.NewHash(sha256.New(), cohortIDNamespace, digest[:], 5)
	return db.WithIdentityTxRaw(ctx, pool, scope.OwnerID.String(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO aura.adaptive_focal_cohorts (
			id, owner_id, provider_id, model_id, policy_epoch, policy_version, snapshot_id,
			snapshot_sha256, environment, domain, decision_point, point_ordinal,
			predicate_sha256, experiment_id, cutoff, sha256, artifact, artifact_json
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			id, scope.OwnerID, scope.ProviderID, scope.ModelID, scope.PolicyEpoch, scope.PolicyVersion,
			scope.SnapshotID, snapshotSHA256, scope.Environment, predicate.Domain, predicate.Point,
			predicate.Ordinal, predicateSHA256, cohort.ExperimentID(), cohort.Cutoff(), digest[:], artifact, artifactJSON)
		return err
	})
}

func focalCohortSnapshotForOwner(t *testing.T, owner uuid.UUID) *PolicySnapshot {
	t.Helper()
	spec := snapshotTestSpec()
	spec.Scope.OwnerID = owner
	spec.Scope.Domain = DomainKnowledge
	spec.Scope.Point = PointKnowledge
	for actionIndex := range spec.Actions {
		for exampleIndex := range spec.Actions[actionIndex].Examples {
			example := &spec.Actions[actionIndex].Examples[exampleIndex]
			example.OwnerID = owner
			example.Domain = DomainKnowledge
			example.Point = PointKnowledge
		}
	}
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func focalCohortSpecForSnapshot(
	snapshot *PolicySnapshot,
	cutoff time.Time,
	blockKeys []BlockKey,
) CohortSpec {
	snapshotScope := snapshot.Scope()
	spec := focalCohortTestSpec()
	spec.Scope = CohortScope{
		OwnerID:        snapshotScope.OwnerID,
		ProviderID:     snapshotScope.ProviderID,
		ModelID:        snapshotScope.ModelID,
		PolicyVersion:  snapshotScope.PolicyVersion,
		PolicyEpoch:    7,
		SnapshotID:     snapshot.ID(),
		SnapshotSHA256: snapshot.SHA256(),
		Environment:    EvaluationProductionCanary,
	}
	spec.Predicate = FocalPredicate{Domain: snapshotScope.Domain, Point: snapshotScope.Point, Ordinal: 1}
	spec.Admission.BlockKeys = blockKeys
	spec.Cutoff = cutoff
	return spec
}

func focalCohortForSnapshot(
	t *testing.T,
	snapshot *PolicySnapshot,
	cutoff time.Time,
	blockKeys []BlockKey,
) *FocalCohort {
	t.Helper()
	return mustFocalCohort(t, focalCohortSpecForSnapshot(snapshot, cutoff, blockKeys))
}

func insertFocalCohort(ctx context.Context, pool *pgxpool.Pool, cohort *FocalCohort) error {
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		return err
	}
	var artifactJSON json.RawMessage
	if err := json.Unmarshal(artifact, &artifactJSON); err != nil {
		return err
	}
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	snapshotSHA256, err := hex.DecodeString(scope.SnapshotSHA256)
	if err != nil {
		return err
	}
	sha256, err := hex.DecodeString(cohort.SHA256())
	if err != nil {
		return err
	}
	predicateSHA256, err := hex.DecodeString(cohort.PredicateSHA256())
	if err != nil {
		return err
	}
	return db.WithIdentityTxRaw(ctx, pool, scope.OwnerID.String(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO aura.adaptive_focal_cohorts (
			id, owner_id, provider_id, model_id, policy_epoch, policy_version,
			snapshot_id, snapshot_sha256, environment, domain, decision_point,
			point_ordinal, predicate_sha256, experiment_id, cutoff, sha256, artifact, artifact_json
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18
		)`, cohort.ID(), scope.OwnerID, scope.ProviderID, scope.ModelID, scope.PolicyEpoch,
			scope.PolicyVersion, scope.SnapshotID, snapshotSHA256, scope.Environment,
			predicate.Domain, predicate.Point, predicate.Ordinal, predicateSHA256,
			cohort.ExperimentID(), cohort.Cutoff(), sha256, artifact, artifactJSON)
		return err
	})
}

type focalCohortClaim struct {
	requestID      uuid.UUID
	conversationID uuid.UUID
	assignmentID   uuid.UUID
	sessionID      *uuid.UUID
	episodeID      *uuid.UUID
}

func newFocalCohortClaim(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	cohort *FocalCohort,
	withSession bool,
	withEpisode bool,
) focalCohortClaim {
	t.Helper()
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	claim := focalCohortClaim{requestID: uuid.New(), conversationID: uuid.New()}
	if withSession {
		sessionID := uuid.New()
		claim.sessionID = &sessionID
	}
	if withEpisode {
		episodeID := uuid.New()
		claim.episodeID = &episodeID
	}
	err := db.WithIdentityTxRaw(ctx, pool, scope.OwnerID.String(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT aura.adaptive_schema2_assignment_id($1,$2,$3,$4)`,
			scope.OwnerID, claim.requestID, predicate.Point, predicate.Ordinal,
		).Scan(&claim.assignmentID)
	})
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func insertFocalCohortClaim(
	ctx context.Context,
	pool *pgxpool.Pool,
	cohort *FocalCohort,
	claim focalCohortClaim,
) (time.Time, time.Time, error) {
	returning := `RETURNING claimed_at, time_block_start`
	return insertFocalCohortClaimRow(ctx, pool, cohort, claim, returning)
}

func insertFocalCohortClaimOnConflict(
	ctx context.Context,
	pool *pgxpool.Pool,
	cohort *FocalCohort,
	claim focalCohortClaim,
	target string,
) (bool, error) {
	returning := `ON CONFLICT ` + target + ` DO NOTHING RETURNING claimed_at, time_block_start`
	_, _, err := insertFocalCohortClaimRow(ctx, pool, cohort, claim, returning)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func insertFocalCohortClaimRow(
	ctx context.Context,
	pool *pgxpool.Pool,
	cohort *FocalCohort,
	claim focalCohortClaim,
	returning string,
) (time.Time, time.Time, error) {
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	var claimedAt, blockStart time.Time
	err := db.WithIdentityTxRaw(ctx, pool, scope.OwnerID.String(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO aura.adaptive_focal_cohort_claims (
			owner_id, cohort_id, request_id, evaluation_conversation_id, assignment_id,
			domain, decision_point, point_ordinal, session_id, episode_id, time_block_start, claimed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12
		) `+returning, scope.OwnerID, cohort.ID(), claim.requestID, claim.conversationID,
			claim.assignmentID, predicate.Domain, predicate.Point, predicate.Ordinal,
			claim.sessionID, claim.episodeID, time.Unix(1, 0), time.Unix(1, 0),
		).Scan(&claimedAt, &blockStart)
	})
	return claimedAt, blockStart, err
}

func assertCohortConstraintError(t *testing.T, err error, message string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || !strings.Contains(pgErr.Message, message) {
		t.Fatalf("cohort constraint error = %v, want 23514 containing %q", err, message)
	}
}

func assertForeignKeyError(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("snapshot scope mismatch error = %v, want foreign-key 23503", err)
	}
}
