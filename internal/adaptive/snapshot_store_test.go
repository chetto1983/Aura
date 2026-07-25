package adaptive

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestSnapshotStoreSaveRejectsUnverifiedSnapshots(t *testing.T) {
	store := NewSnapshotStore(nil)

	tests := []struct {
		name     string
		snapshot *PolicySnapshot
	}{
		{name: "nil", snapshot: nil},
		{name: "zero value", snapshot: &PolicySnapshot{}},
		{
			name: "forged verification marker",
			snapshot: &PolicySnapshot{
				id:       uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
				sha256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				scope:    snapshotTestSpec().Scope,
				verified: true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.Save(t.Context(), test.snapshot)
			if !errors.Is(err, ErrUnverifiedSnapshot) {
				t.Fatalf("Save() error = %v, want ErrUnverifiedSnapshot", err)
			}
		})
	}
}

func TestSnapshotStoreIsNilSafe(t *testing.T) {
	snapshot, err := NewPolicySnapshot(snapshotTestSpec())
	if err != nil {
		t.Fatal(err)
	}

	var nilStore *SnapshotStore
	if err := nilStore.Save(t.Context(), snapshot); err == nil {
		t.Fatal("nil SnapshotStore.Save() error = nil")
	}
	if _, err := nilStore.Load(t.Context(), snapshot.Scope().OwnerID, snapshot.ID()); err == nil {
		t.Fatal("nil SnapshotStore.Load() error = nil")
	}

	store := NewSnapshotStore(nil)
	if err := store.Save(t.Context(), snapshot); err == nil {
		t.Fatal("SnapshotStore without pool Save() error = nil")
	}
	if _, err := store.Load(t.Context(), snapshot.Scope().OwnerID, snapshot.ID()); err == nil {
		t.Fatal("SnapshotStore without pool Load() error = nil")
	}
}

func TestSnapshotStoreLoadRejectsMissingScopeBeforeDatabase(t *testing.T) {
	store := NewSnapshotStore(nil)
	ownerID := snapshotTestSpec().Scope.OwnerID
	snapshotID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")

	for name, scope := range map[string]struct {
		ownerID    uuid.UUID
		snapshotID uuid.UUID
	}{
		"owner":    {ownerID: uuid.Nil, snapshotID: snapshotID},
		"snapshot": {ownerID: ownerID, snapshotID: uuid.Nil},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := store.Load(context.Background(), scope.ownerID, scope.snapshotID)
			if !errors.Is(err, ErrSnapshotNotFound) {
				t.Fatalf("Load() error = %v, want ErrSnapshotNotFound", err)
			}
		})
	}
}

func TestPolicySnapshotCanonicalArtifactIsStableAndDefensive(t *testing.T) {
	snapshot, err := NewPolicySnapshot(snapshotTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	first, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatalf("canonicalArtifact() error = %v", err)
	}
	second, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatalf("canonicalArtifact() replay error = %v", err)
	}
	if !slices.Equal(first, second) {
		t.Fatal("canonicalArtifact() bytes changed between reads")
	}

	first[0] ^= 0xff
	third, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatalf("canonicalArtifact() after caller mutation error = %v", err)
	}
	if slices.Equal(first, third) {
		t.Fatal("canonicalArtifact() exposed mutable internal bytes")
	}
}
