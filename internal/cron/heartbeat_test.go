//go:build db_integration

// Integration tests for the held-conn heartbeat ticker (D-02, Pitfall 4). They
// prove the liveness UPDATE lands on the held conn at the configured interval and
// that stop() joins the goroutine so goleak.VerifyTestMain (main_test.go) stays
// green. Run via:
//
//	go test -tags db_integration -race -run TestHeartbeat ./internal/cron -count=1
package cron

import (
	"context"
	"testing"
	"time"
)

func TestHeartbeatTickerUpdatesHeldConn(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := s.claim(ctx, task)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer c.release(ctx)

	before := runHeartbeat(t, ctx, s, c.RunID)

	// Tick fast so the test stays sub-second-per-beat without a real 30s wait.
	stop := startHeartbeat(ctx, c.Conn(), c.RunID, 50*time.Millisecond)
	time.Sleep(250 * time.Millisecond)
	stop() // joins the ticker goroutine — goleak gate stays green

	after := runHeartbeat(t, ctx, s, c.RunID)
	if !after.After(before) {
		t.Errorf("heartbeat did not advance last_heartbeat_at: before=%s after=%s", before, after)
	}
}

func TestHeartbeatStopsOnCtxCancel(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	parent, parentCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer parentCancel()
	c, err := s.claim(parent, task)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer c.release(parent)

	hbCtx, hbCancel := context.WithCancel(parent)
	stop := startHeartbeat(hbCtx, c.Conn(), c.RunID, 50*time.Millisecond)

	// Cancelling the heartbeat ctx must drive the goroutine to exit; stop() then
	// returns promptly (it joins on the already-exiting goroutine). No leak.
	hbCancel()
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startHeartbeat goroutine did not exit on ctx cancel (leak)")
	}
}

// runHeartbeat reads the current last_heartbeat_at for runID directly off the pool.
func runHeartbeat(t *testing.T, ctx context.Context, s *Scheduler, runID string) time.Time {
	t.Helper()
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	return run.LastHeartbeatAt
}
