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
}
