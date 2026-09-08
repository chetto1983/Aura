package agui

import (
	"context"
	"io"
	"iter"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
)

type coordinatorWakeRunner struct {
	detachLockRunner
	pending      atomic.Bool
	preparations atomic.Int32
	release      chan struct{}
}

func (r *coordinatorWakeRunner) PreparePendingSteer(ctx context.Context, _ string) (iter.Seq2[*agent.Event, error], bool, error) {
	r.preparations.Add(1)
	if !r.pending.CompareAndSwap(true, false) {
		return nil, false, nil
	}
	return func(yield func(*agent.Event, error) bool) {
		select {
		case <-ctx.Done():
			return
		case <-r.release:
		}
		for _, ev := range textTurn("complete coordinator synthesis") {
			if !yield(ev, nil) {
				return
			}
		}
	}, true, nil
}

func TestCoordinatorWakeIsScopedDeferredAndResumable(t *testing.T) {
	const conv = "21212121-2121-2121-2121-212121212121"
	r := &coordinatorWakeRunner{release: make(chan struct{})}
	r.pending.Store(true)
	s, srv := newDetachTestServer(t, r, &fakeConvStore{known: map[string]bool{conv: true}}, ServerConfig{})
	ctx := identityctx.WithIdentityID(context.Background(), localIdentityID)
	r.locked.Store(true)
	if started, err := s.ResumePendingSteer(ctx, localIdentityID, conv); err != nil || started || r.preparations.Load() != 0 {
		t.Fatalf("busy wake started=%v err=%v", started, err)
	}
	r.locked.Store(false)
	if started, err := s.ResumePendingSteer(ctx, localIdentityID, conv); err != nil || !started {
		t.Fatalf("wake started=%v err=%v", started, err)
	}
	sess, ok := s.runs.LiveForThread(localIdentityID, conv)
	if !ok {
		t.Fatal("continuation missing from normal run discovery")
	}
	if s.runs.coordinatorStatus("foreign", conv).RunID != "" {
		t.Fatal("foreign coordinator disclosed")
	}
	close(r.release)
	resp, err := http.Get(srv.URL + "/agent/runs/" + sess.RunID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || !strings.Contains(string(body), "complete coordinator synthesis") || !strings.Contains(string(body), "RUN_FINISHED") {
		t.Fatalf("resume stream err=%v body=%s", err, body)
	}
	deadline := time.Now().Add(time.Second)
	for r.locked.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.locked.Load() {
		t.Fatal("continuation retained thread lock")
	}
	if s.runs.coordinatorStatus(localIdentityID, conv).Status != "finished" {
		t.Fatal("missing terminal discovery")
	}
	if started, err := s.ResumePendingSteer(ctx, localIdentityID, conv); err != nil || started {
		t.Fatalf("consumed batch woke again: %v %v", started, err)
	}
}

func TestCoordinatorWakeRefusesInvalidOrMissingOwnerBeforePreparation(t *testing.T) {
	r := &coordinatorWakeRunner{release: make(chan struct{})}
	s, _ := newDetachTestServer(t, r, &fakeConvStore{known: map[string]bool{}}, ServerConfig{})
	for _, ids := range [][2]string{{"", "21212121-2121-2121-2121-212121212121"}, {localIdentityID, "invalid"}, {localIdentityID, "21212121-2121-2121-2121-212121212121"}} {
		if started, err := s.ResumePendingSteer(context.Background(), ids[0], ids[1]); err == nil || started {
			t.Fatalf("accepted invalid route: %v", ids)
		}
	}
	if r.preparations.Load() != 0 {
		t.Fatal("unauthorized preparation consumed a report")
	}
}
