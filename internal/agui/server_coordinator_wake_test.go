package agui

import (
	"context"
	"errors"
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
	if _, live := s.runs.LiveForThread(localIdentityID, conv); live || r.locked.Load() {
		t.Fatal("a continuation with nothing to deliver stayed live or kept the lock")
	}
}

type identityLockRunner struct {
	coordinatorWakeRunner
	lockedAs string
}

func (r *identityLockRunner) TryLockThread(ctx context.Context, conv string) (func(), bool) {
	r.lockedAs = identityctx.IdentityID(ctx)
	return r.coordinatorWakeRunner.TryLockThread(ctx, conv)
}

// TestCoordinatorWakeLocksAsItsOwner: the delegation worker calls with a bare context, and the
// lock it takes must be the owner's, the one the owner's own runs contend for.
func TestCoordinatorWakeLocksAsItsOwner(t *testing.T) {
	const conv = "23232323-2323-2323-2323-232323232323"
	r := &identityLockRunner{}
	s, _ := newDetachTestServer(t, r, &fakeConvStore{known: map[string]bool{conv: true}}, ServerConfig{})
	if started, err := s.ResumePendingSteer(context.Background(), localIdentityID, conv); started || err != nil {
		t.Fatalf("started=%v err=%v, want nothing to deliver", started, err)
	}
	if r.lockedAs != localIdentityID {
		t.Fatalf("locked as %q, want the owner %q", r.lockedAs, localIdentityID)
	}
}

type pendingOnlyRunner struct{ scriptedRunner }

func (*pendingOnlyRunner) PreparePendingSteer(context.Context, string) (iter.Seq2[*agent.Event, error], bool, error) {
	return nil, true, nil
}

func TestCoordinatorWakeRefusesWhatItCannotHost(t *testing.T) {
	const conv = "22222222-2222-2222-2222-222222222222"
	ctx := context.Background()
	known := &fakeConvStore{known: map[string]bool{conv: true}}

	if started, err := NewServer(&coordinatorWakeRunner{}, known, ServerConfig{}).ResumePendingSteer(ctx, localIdentityID, conv); started || err != nil {
		t.Fatalf("without a run registry: started=%v err=%v, want a quiet no", started, err)
	}
	unlockable, _ := newDetachTestServer(t, &detachLockRunner{}, known, ServerConfig{})
	if started, err := unlockable.ResumePendingSteer(ctx, localIdentityID, conv); started || err == nil {
		t.Fatalf("a runner that cannot consume a pending steer: started=%v err=%v", started, err)
	}
	lockless, _ := newDetachTestServer(t, &pendingOnlyRunner{}, known, ServerConfig{})
	if started, err := lockless.ResumePendingSteer(ctx, localIdentityID, conv); started || err == nil {
		t.Fatalf("a runner without a conversation lock: started=%v err=%v", started, err)
	}

	r := &coordinatorWakeRunner{release: make(chan struct{})}
	r.pending.Store(true)
	full, _ := newDetachTestServer(t, r, known, ServerConfig{RunMaxLive: 1})
	busy, err := full.runs.Start(runParams{runID: "run-busy", threadID: "other", identityID: localIdentityID, cancel: func() {}})
	if err != nil {
		t.Fatal(err)
	}
	defer busy.finish()
	if started, err := full.ResumePendingSteer(ctx, localIdentityID, conv); started || !errors.Is(err, errRunRegistryFull) {
		t.Fatalf("a full registry: started=%v err=%v, want errRunRegistryFull", started, err)
	}
	if r.locked.Load() || r.preparations.Load() != 0 || !r.pending.Load() {
		t.Fatal("a refused continuation kept the lock or consumed the saved batch")
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
