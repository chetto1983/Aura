package cron

// oneshot_cleanup_test.go covers the settled one-shot delete without a database: the store
// wrapper's rows-affected projection, the int4 clamp on the retry bound, the error wrap, and
// the tick treating a failed delete as housekeeping rather than a failed tick. The cascade
// itself is proven against Postgres in store_oneshot_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestStoreFakeDeleteSettledOneShotsReturnsDeletedCount(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execTag: pgconn.NewCommandTag("DELETE 2")}
	n, err := storeWithFake(f).DeleteSettledOneShots(context.Background(), 3)
	if err != nil {
		t.Fatalf("DeleteSettledOneShots: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d, want 2", n)
	}
	if bound, ok := f.gotArgs[0].(int32); !ok || bound != 3 {
		t.Fatalf("the attempt bound must bind verbatim, got %v", f.gotArgs[0])
	}
}

func TestStoreFakeDeleteSettledOneShotsClampsTheBound(t *testing.T) {
	t.Parallel()
	for _, bound := range []int{0, -4, int(^uint(0) >> 1)} {
		f := &cronFakeDBTX{}
		if _, err := storeWithFake(f).DeleteSettledOneShots(context.Background(), bound); err != nil {
			t.Fatalf("DeleteSettledOneShots(%d): %v", bound, err)
		}
		if got := f.gotArgs[0].(int32); got != 1 {
			t.Errorf("bound %d must clamp to 1, got %d", bound, got)
		}
	}
}

func TestStoreFakeDeleteSettledOneShotsWrapsTheDBError(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	if _, err := storeWithFake(f).DeleteSettledOneShots(context.Background(), 3); !errors.Is(err, errDB) {
		t.Fatalf("DeleteSettledOneShots must wrap the DB error, got %v", err)
	}
}

func TestTickSurvivesAFailedOneShotDelete(t *testing.T) {
	f := &cronFakeDBTX{queryRows: &cronFakeRows{}, execErr: errDB}
	s := NewScheduler(nil, storeWithFake(f), SchedulerConfig{MaxConcurrent: 1, TickInterval: time.Second})
	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("a failed one-shot delete must not fail the tick, got %v", err)
	}
	if !strings.Contains(f.gotSQL, "DeleteSettledOneShots") {
		t.Fatalf("the tick never ran the one-shot delete; last statement:\n%s", f.gotSQL)
	}
}
