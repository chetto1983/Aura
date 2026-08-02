package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeProjectionQueue struct {
	record           *OutboxRecord
	tombstoned       bool
	tombstoneResults []bool
	tombstoneChecks  int
	marked           int
	retried          int
}

func (f *fakeProjectionQueue) Claim(context.Context, ClaimOptions) (*OutboxRecord, error) {
	return f.record, nil
}
func (f *fakeProjectionQueue) MarkProjected(context.Context, uuid.UUID, string, time.Time) error {
	f.marked++
	return nil
}
func (f *fakeProjectionQueue) Retry(context.Context, uuid.UUID, string, string, time.Time, time.Time) error {
	f.retried++
	return nil
}
func (f *fakeProjectionQueue) IsOwnerTombstoned(context.Context, uuid.UUID) (bool, error) {
	if f.tombstoneChecks < len(f.tombstoneResults) {
		result := f.tombstoneResults[f.tombstoneChecks]
		f.tombstoneChecks++
		return result, nil
	}
	f.tombstoneChecks++
	return f.tombstoned, nil
}

type fakeAdaptiveGraph struct {
	projected int
	purged    int
	err       error
}

func (f *fakeAdaptiveGraph) Project(context.Context, OutboxRecord) error {
	f.projected++
	return f.err
}
func (f *fakeAdaptiveGraph) PurgeOwner(context.Context, uuid.UUID) error {
	f.purged++
	return f.err
}

func TestProjectorHonorsProjectionRetryAndDeletionRace(t *testing.T) {
	record := &OutboxRecord{
		ID: uuid.Must(uuid.NewV7()), OwnerID: uuid.Must(uuid.NewV7()),
		AggregateID: "request-1", Sequence: 1,
	}
	t.Run("success", func(t *testing.T) {
		queue := &fakeProjectionQueue{record: record}
		graph := &fakeAdaptiveGraph{}
		projector := NewProjector(queue, graph, ProjectorConfig{WorkerID: "p1"})
		didWork, err := projector.ProjectOne(context.Background())
		if err != nil || !didWork {
			t.Fatalf("ProjectOne = (%t,%v), want (true,nil)", didWork, err)
		}
		if graph.projected != 1 || queue.marked != 1 || queue.retried != 0 {
			t.Fatalf("success calls = projected:%d marked:%d retried:%d", graph.projected, queue.marked, queue.retried)
		}
	})
	t.Run("graph failure is retryable", func(t *testing.T) {
		queue := &fakeProjectionQueue{record: record}
		graph := &fakeAdaptiveGraph{err: errors.New("arcadedb down")}
		projector := NewProjector(queue, graph, ProjectorConfig{WorkerID: "p2"})
		if didWork, err := projector.ProjectOne(context.Background()); err == nil || !didWork {
			t.Fatalf("ProjectOne = (%t,%v), want work + surfaced error", didWork, err)
		}
		if queue.retried != 1 || queue.marked != 0 {
			t.Fatalf("failure calls = marked:%d retried:%d", queue.marked, queue.retried)
		}
	})
	t.Run("deleted owner purges without resurrection", func(t *testing.T) {
		queue := &fakeProjectionQueue{record: record, tombstoned: true}
		graph := &fakeAdaptiveGraph{}
		projector := NewProjector(queue, graph, ProjectorConfig{WorkerID: "p3"})
		if didWork, err := projector.ProjectOne(context.Background()); err != nil || !didWork {
			t.Fatalf("ProjectOne = (%t,%v), want purge success", didWork, err)
		}
		if graph.projected != 0 || graph.purged != 1 || queue.marked != 0 {
			t.Fatalf("delete-race calls = projected:%d purged:%d marked:%d", graph.projected, graph.purged, queue.marked)
		}
	})
	t.Run("delete fence raised during graph write purges the late projection", func(t *testing.T) {
		queue := &fakeProjectionQueue{record: record, tombstoneResults: []bool{false, true}}
		graph := &fakeAdaptiveGraph{}
		projector := NewProjector(queue, graph, ProjectorConfig{WorkerID: "p4"})
		if didWork, err := projector.ProjectOne(context.Background()); err != nil || !didWork {
			t.Fatalf("ProjectOne = (%t,%v), want fenced purge success", didWork, err)
		}
		if graph.projected != 1 || graph.purged != 1 || queue.marked != 0 || queue.tombstoneChecks != 2 {
			t.Fatalf("interleaved delete calls = projected:%d purged:%d marked:%d checks:%d, want 1/1/0/2",
				graph.projected, graph.purged, queue.marked, queue.tombstoneChecks)
		}
	})
}
