package db

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestAdaptivePolicySnapshotMigrationSourceContract(t *testing.T) {
	for _, migration := range []struct {
		name string
		hash string
	}{
		{
			name: "0068_adaptive_policy_snapshots.up.sql",
			hash: "ab582ecbf145ff897ce5a12bcb33826274e1fa518e56b75607c6b668a68f255d",
		},
		{
			name: "0068_adaptive_policy_snapshots.down.sql",
			hash: "97f544852e4363b0850a25120294ebb0643b9368e15357a2d4a3224a4fb15193",
		},
		{
			name: "0069_adaptive_policy_snapshot_vector_parity.up.sql",
			hash: "9aa9409ca47e62d88a208e7a46456c2ff46a3921ed8c15cc57717422a19c1521",
		},
		{
			name: "0069_adaptive_policy_snapshot_vector_parity.down.sql",
			hash: "043abc68faad5fe057a2ccc72eee5977d7f9dd5903e51a9cd865c4a50d1079ef",
		},
	} {
		body, err := migrationsFS.ReadFile("migrations/" + migration.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != migration.hash {
			t.Fatalf("%s changed after release: sha256=%s", migration.name, got)
		}
	}

	v68Bytes, err := migrationsFS.ReadFile(
		"migrations/0068_adaptive_policy_snapshots.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	functionEnd := strings.Index(
		string(v68Bytes),
		"\n\nCREATE TABLE aura.adaptive_policy_snapshots",
	)
	if functionEnd < 0 {
		t.Fatal("cannot isolate the 0068 snapshot validator")
	}
	wantDown := strings.Replace(
		string(v68Bytes[:functionEnd]),
		"CREATE FUNCTION",
		"CREATE OR REPLACE FUNCTION",
		1,
	) + "\n"
	v69Down, err := migrationsFS.ReadFile(
		"migrations/0069_adaptive_policy_snapshot_vector_parity.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(v69Down) != wantDown {
		t.Fatal("0069 down does not restore the exact 0068 snapshot validator")
	}

	v69Up, err := migrationsFS.ReadFile(
		"migrations/0069_adaptive_policy_snapshot_vector_parity.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"normalized_values double precision[]",
		"vector_maximum := greatest(",
		"normalized_value / vector_maximum",
		"vector_maximum = 0",
	} {
		if !strings.Contains(string(v69Up), want) {
			t.Errorf("0069 up migration missing %q", want)
		}
	}

	v70Down, err := migrationsFS.ReadFile(
		"migrations/0070_adaptive_policy_snapshot_duplicate_scale.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(v70Down) != string(v69Up) {
		t.Fatal("0070 down does not restore the exact 0069 snapshot validator")
	}
	v70Up, err := migrationsFS.ReadFile(
		"migrations/0070_adaptive_policy_snapshot_duplicate_scale.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(v70Up)
	for _, want := range []string{
		"GROUP BY catalog_example.value->>'outcome_id'",
		"HAVING count(*) > 1",
		"normalized_value / vector_maximum",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0070 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"outcome_ids uuid[]",
		"ANY(outcome_ids)",
		"array_append(outcome_ids",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("0070 up migration retained quadratic path %q", forbidden)
		}
	}
}
