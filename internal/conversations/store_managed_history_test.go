package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestManagedHistoryWorkFailsInsteadOfReturningPartialRows(t *testing.T) {
	work := managedHistoryWork{rows: maxManagedHistoryRows}
	if err := work.add(Turn{Content: "one row too many"}); !errors.Is(err, ErrManagedHistoryWorkLimit) {
		t.Fatalf("row limit error = %v", err)
	}

	work = managedHistoryWork{inlineBytes: maxManagedHistoryBytes}
	if err := work.add(Turn{Content: "one byte too many"}); !errors.Is(err, ErrManagedHistoryWorkLimit) {
		t.Fatalf("byte limit error = %v", err)
	}
}

func TestManagedHistoryMalformedStoredSummaryIsNotReadableOrWritable(t *testing.T) {
	store := &recordingCompactionCache{}
	snapshot := &compactionSnapshot{
		delegate: store,
		stored:   Compaction{Summary: strings.Repeat("invalid", 2), CoversThroughSeq: 99},
		readable: false,
		writable: false,
	}
	if _, ok, err := snapshot.LoadCompaction(t.Context(), "conv", ""); err != nil || ok {
		t.Fatalf("rejected snapshot load = ok %v err %v", ok, err)
	}
	if err := snapshot.SaveCompaction(t.Context(), "conv", "", Compaction{Summary: "new"}); err != nil {
		t.Fatalf("rejected snapshot save: %v", err)
	}
	if store.saves != 0 {
		t.Fatalf("rejected path advanced durable compaction %d times", store.saves)
	}
}

type recordingCompactionCache struct {
	saves int
}

func (*recordingCompactionCache) LoadCompaction(
	context.Context, string, string,
) (Compaction, bool, error) {
	return Compaction{}, false, nil
}

func (c *recordingCompactionCache) SaveCompaction(
	context.Context, string, string, Compaction,
) error {
	c.saves++
	return nil
}
