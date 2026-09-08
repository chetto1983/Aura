package agui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
	"go.uber.org/goleak"
)

const controlOwner = "11111111-1111-1111-1111-111111111111"
const controlConversation = "22222222-2222-2222-2222-222222222222"

func controlContext() context.Context {
	return identityctx.WithIdentityID(context.Background(), controlOwner)
}

func registryWorkerParams(child string) agent.WorkerControlParams {
	return agent.WorkerControlParams{ConversationID: controlConversation, ChildID: child, SteerEnabled: true, Cancel: func() {}, Stop: func(context.Context) error { return nil }}
}

func TestWorkerRegistryPreservesParentDiscoveryAndAdmission(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := NewRunRegistry(ServerConfig{RunMaxLive: 1})
	defer r.Close()
	parent, err := r.Start(runParams{runID: "parent", threadID: controlConversation, identityID: controlOwner})
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.StartWorker(controlContext(), registryWorkerParams("w1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.StartWorker(controlContext(), registryWorkerParams("w2"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.LiveForThread(controlOwner, controlConversation); !ok || got != parent {
		t.Fatal("child replaced parent discovery")
	}
	if _, ok := r.LiveForWorker(controlOwner, controlConversation, ""); ok {
		t.Fatal("empty child selected its parent")
	}
	if _, ok := r.LiveForWorker("foreign", controlConversation, "w1"); ok {
		t.Fatal("foreign owner discovered worker")
	}
	if _, err := r.StartWorker(controlContext(), registryWorkerParams("w1")); !errors.Is(err, errWorkerAlreadyRunning) {
		t.Fatalf("duplicate live worker: %v", err)
	}
	parent.finish()
	if r.atCapacity() {
		t.Fatal("worker observation consumed the operator's parent slot")
	}
	newParent, err := r.Start(runParams{runID: "new-parent", threadID: controlConversation, identityID: controlOwner})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.LiveForWorker(controlOwner, controlConversation, "w1"); ok {
		t.Fatal("closed worker remains live")
	}
	if got, ok := r.LiveForWorker(controlOwner, controlConversation, "w2"); !ok || got.RunID != second.RunID {
		t.Fatal("closing one worker removed its sibling")
	}
	if got, ok := r.LiveForThread(controlOwner, controlConversation); !ok || got != newParent {
		t.Fatal("worker cleanup removed parent")
	}
	if err := second.Finish(); err != nil {
		t.Fatal(err)
	}
	newParent.finish()
}

func TestWorkerControlSettlementWaitsForAcceptedMutation(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := NewRunRegistry(ServerConfig{})
	defer r.Close()
	var applied atomic.Bool
	p := registryWorkerParams("w1")
	p.OnClose = func() error {
		if !applied.Load() {
			t.Error("settled before accepted mutation completed")
		}
		return nil
	}
	control, err := r.StartWorker(controlContext(), p)
	if err != nil {
		t.Fatal(err)
	}
	session, _ := r.Get(control.RunID)
	entered, release, mutationDone, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan error, 1)
	go func() {
		mutationDone <- session.withWorkerControl(func() error { close(entered); <-release; applied.Store(true); return nil })
	}()
	<-entered
	go func() { finished <- control.Finish() }()
	select {
	case err := <-finished:
		t.Errorf("finished during accepted mutation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-mutationDone; err != nil {
		t.Fatal(err)
	}
	if err := control.Finish(); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := session.withWorkerControl(func() error { called = true; return nil }); !errors.Is(err, steer.ErrClosed) || called {
		t.Fatal("terminal worker accepted another mutation")
	}
}

func TestWorkerCloseRunsOnceAndPreservesCleanupError(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := NewRunRegistry(ServerConfig{})
	defer r.Close()
	var closed atomic.Int32
	want := errors.New("receipt store unavailable")
	p := registryWorkerParams("w1")
	p.OnClose = func() error { closed.Add(1); return want }
	control, err := r.StartWorker(controlContext(), p)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := control.Finish(); !errors.Is(err, want) {
				t.Errorf("cleanup error: %v", err)
			}
		})
	}
	wg.Wait()
	if closed.Load() != 1 {
		t.Fatalf("cleanup called %d times", closed.Load())
	}
}
