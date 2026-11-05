//go:build db_integration

package pimwatch

import (
	"context"
	"testing"
)

func TestAWatchFillsTheDefaultDescriptionsBeforeItReadsTheLabels(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	if _, err := r.store.AddLabel(ctx, r.identity, LabelInput{Name: "Cliente"}, 0); err != nil {
		t.Fatal(err)
	}
	spy := &seedSpy{WatchStore: r.store}
	r.watcher.Store = spy
	r.tick()
	if !spy.seeded || !spy.seededBeforeBook {
		t.Fatalf("seeded = %v, before the Book = %v", spy.seeded, spy.seededBeforeBook)
	}
	labels, err := r.store.Labels(ctx, r.identity)
	if err != nil || len(labels) != 1 || labels[0].Description != DefaultLabels(LangIT)[0].Description {
		t.Fatalf("labels = %+v, %v", labels, err)
	}
}

// seedSpy records whether the seed ran before the Book was read, and that the Book already held
// the description.
type seedSpy struct {
	WatchStore
	seeded           bool
	seededBeforeBook bool
}

func (s *seedSpy) SeedLabelDescriptions(ctx context.Context, identityID string, defaults []Label) (bool, error) {
	s.seeded = true
	return s.WatchStore.SeedLabelDescriptions(ctx, identityID, defaults)
}

func (s *seedSpy) Book(ctx context.Context, identityID string) (Book, error) {
	book, err := s.WatchStore.Book(ctx, identityID)
	for _, label := range book.labels {
		if label.Name == "Cliente" && label.Description != "" {
			s.seededBeforeBook = s.seeded
		}
	}
	return book, err
}
