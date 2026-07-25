package db

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAdaptiveOwnerLockMigrationSourceContract(t *testing.T) {
	for _, migration := range []struct {
		name string
		hash string
	}{
		{
			name: "0064_adaptive_locked_identity_audit.up.sql",
			hash: "77702080d97f6a4306276d113e79f15ab1fb79c4e396a3d6353787ef24493bda",
		},
		{
			name: "0064_adaptive_locked_identity_audit.down.sql",
			hash: "039f0597d2a744421e3a3c14e02b98f542cf6f78ebdc56b57ac1f109d4196ed5",
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

	upBytes, err := migrationsFS.ReadFile(
		"migrations/0065_adaptive_owner_lock_order.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION aura.fence_adaptive_identity",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog",
		"SELECT id",
		"FROM aura.identities",
		"WHERE id = p_owner_id",
		"FOR UPDATE",
		"IF NOT FOUND THEN",
		"RETURN false",
		"INSERT INTO aura.adaptive_identity_tombstones",
		"RETURN true",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0065 up migration missing %q", want)
		}
	}
	lockAt := strings.Index(up, "FOR UPDATE")
	tombstoneAt := strings.Index(up, "INSERT INTO aura.adaptive_identity_tombstones")
	if lockAt < 0 || tombstoneAt <= lockAt {
		t.Error("0065 must lock the owner row before writing its tombstone")
	}

	priorBytes, err := migrationsFS.ReadFile(
		"migrations/0056_adaptive_tombstone_privileges.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	prior := string(priorBytes)
	priorStart := strings.Index(
		prior,
		"CREATE FUNCTION aura.fence_adaptive_identity",
	)
	if priorStart < 0 {
		t.Fatal("cannot find the pre-0065 fence function")
	}
	priorEnd := strings.Index(prior[priorStart:], "\n\nREVOKE ")
	if priorEnd < 0 {
		t.Fatal("cannot isolate the pre-0065 fence function")
	}
	wantDown := strings.Replace(
		prior[priorStart:priorStart+priorEnd],
		"CREATE FUNCTION",
		"CREATE OR REPLACE FUNCTION",
		1,
	) + "\n"
	downBytes, err := migrationsFS.ReadFile(
		"migrations/0065_adaptive_owner_lock_order.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(downBytes) != wantDown {
		t.Error("0065 down migration does not restore the pre-0065 fence function exactly")
	}

	queryBytes, err := os.ReadFile("queries/adaptive_outbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := string(queryBytes)
	for _, want := range []string{
		"-- name: LockAdaptiveOwner :one",
		"FROM aura.identities",
		"FOR KEY SHARE",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("adaptive query source missing %q", want)
		}
	}
}
