package memoryindex

import (
	"context"
	"errors"
	"testing"
	"time"
)

// compactRebuildStubIndex implements VectorIndex for compact_rebuild tests.
type compactRebuildStubIndex struct {
	recreateReport VectorReport
	recreateErr    error
	recreateCalled bool
}

func (s *compactRebuildStubIndex) Recreate(_ context.Context, _ []Document) (VectorReport, error) {
	s.recreateCalled = true
	return s.recreateReport, s.recreateErr
}
func (s *compactRebuildStubIndex) Upsert(_ context.Context, _ []Document) error { return nil }
func (s *compactRebuildStubIndex) Search(_ context.Context, _ string, _ Filter) ([]Document, error) {
	return nil, nil
}
func (s *compactRebuildStubIndex) Delete(_ context.Context, _ []string) error { return nil }

// compactRebuildForceStub additionally satisfies the forceRebuilder duck interface.
type compactRebuildForceStub struct {
	compactRebuildStubIndex
	forceReport CompactRebuildReport
	forceErr    error
	forceCalled bool
}

func (s *compactRebuildForceStub) ForceRebuild(_ context.Context, _ []Document) (CompactRebuildReport, error) {
	s.forceCalled = true
	return s.forceReport, s.forceErr
}

func TestRebuildCompactNilStoreErrors(t *testing.T) {
	var s *Store
	_, err := s.RebuildCompact(context.Background())
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
}

func TestRebuildCompactNoVector(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Upsert(ctx, Document{
		ID:        "source:rb-x1",
		Kind:      KindSource,
		Body:      "test body for no-vector path",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	report, err := store.RebuildCompact(ctx)
	if err != nil {
		t.Fatalf("RebuildCompact: %v", err)
	}
	if report.DocsInSQL != 1 {
		t.Errorf("DocsInSQL = %d, want 1", report.DocsInSQL)
	}
	if report.Collection != "" || report.PointsIndexed != 0 || report.VectorSize != 0 {
		t.Errorf("expected zero vector fields for no-vector path, got %+v", report)
	}
}

func TestRebuildCompactUsesForceRebuilder(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	stub := &compactRebuildForceStub{
		forceReport: CompactRebuildReport{
			Collection:    "aura_memory_v1_compact",
			PointsIndexed: 3,
			VectorSize:    256,
		},
	}
	store, err := NewStoreWithVector(db, stub, nil)
	if err != nil {
		t.Fatalf("NewStoreWithVector: %v", err)
	}
	if err := store.Upsert(ctx, Document{
		ID:        "source:rb-x2",
		Kind:      KindSource,
		Body:      "test body for force-rebuild path",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	report, err := store.RebuildCompact(ctx)
	if err != nil {
		t.Fatalf("RebuildCompact: %v", err)
	}
	if !stub.forceCalled {
		t.Fatal("ForceRebuild was not called")
	}
	if stub.recreateCalled {
		t.Fatal("Recreate was called unexpectedly on force-rebuild path")
	}
	if report.DocsInSQL != 1 {
		t.Errorf("DocsInSQL = %d, want 1 (stamp by RebuildCompact)", report.DocsInSQL)
	}
	if report.PointsIndexed != 3 || report.Collection != "aura_memory_v1_compact" || report.VectorSize != 256 {
		t.Errorf("unexpected report fields: %+v", report)
	}
}

func TestRebuildCompactFallsBackToRecreate(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	stub := &compactRebuildStubIndex{
		recreateReport: VectorReport{
			Collection:  "aura_memory_v1_compact",
			DocsIndexed: 2,
			VectorSize:  128,
		},
	}
	store, err := NewStoreWithVector(db, stub, nil)
	if err != nil {
		t.Fatalf("NewStoreWithVector: %v", err)
	}
	if err := store.Upsert(ctx, Document{
		ID:        "source:rb-x3",
		Kind:      KindSource,
		Body:      "test body for recreate-fallback path",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	report, err := store.RebuildCompact(ctx)
	if err != nil {
		t.Fatalf("RebuildCompact: %v", err)
	}
	if !stub.recreateCalled {
		t.Fatal("Recreate was not called on fallback path")
	}
	if report.DocsInSQL != 1 {
		t.Errorf("DocsInSQL = %d, want 1", report.DocsInSQL)
	}
	if report.PointsIndexed != 2 || report.Collection != "aura_memory_v1_compact" || report.VectorSize != 128 {
		t.Errorf("unexpected report fields: %+v", report)
	}
}

func TestRebuildCompactForceRebuilderError(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	stub := &compactRebuildForceStub{
		forceErr: errors.New("qdrant unavailable"),
	}
	store, err := NewStoreWithVector(db, stub, nil)
	if err != nil {
		t.Fatalf("NewStoreWithVector: %v", err)
	}
	if err := store.Upsert(ctx, Document{
		ID:        "source:rb-x4",
		Kind:      KindSource,
		Body:      "test body for error path",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	_, err = store.RebuildCompact(ctx)
	if err == nil {
		t.Fatal("expected error from ForceRebuild, got nil")
	}
}
