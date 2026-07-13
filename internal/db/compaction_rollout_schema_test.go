package db

import (
	"strings"
	"testing"
)

func TestCompactionRolloutSchemaContract(t *testing.T) {
	up := readSchemaContractFile(t, "migrations/0039_compaction_rollout.up.sql")
	for _, want := range []string{
		"CREATE TABLE aura.compaction_rollout_states",
		"CREATE TABLE aura.compaction_rollout_evidence",
		"CREATE TABLE aura.compaction_rollout_decisions",
		"last_known_good_config",
		"last_known_good_policy",
		"compaction_rollout_evidence_immutable",
		"compaction_rollout_decisions_immutable",
		"reason_code",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("compaction rollout migration missing %q", want)
		}
	}

	down := readSchemaContractFile(t, "migrations/0039_compaction_rollout.down.sql")
	for _, want := range []string{
		"DROP TABLE IF EXISTS aura.compaction_rollout_decisions",
		"DROP TABLE IF EXISTS aura.compaction_rollout_evidence",
		"DROP TABLE IF EXISTS aura.compaction_rollout_states",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("compaction rollout down migration missing %q", want)
		}
	}

	queries := readSchemaContractFile(t, "queries/compaction_rollout.sql")
	for _, want := range []string{
		"-- name: GetCompactionRolloutState :one",
		"-- name: AppendCompactionRolloutEvidence :one",
		"-- name: AppendCompactionRolloutDecision :one",
		"-- name: CASTransitionCompactionRollout :one",
		"-- name: CASRollbackCompactionRollout :one",
		"version = sqlc.arg(expected_version)",
	} {
		if !strings.Contains(queries, want) {
			t.Fatalf("compaction rollout queries missing %q", want)
		}
	}
}
