package agui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"go.uber.org/goleak"
	"pgregory.net/rapid"
)

// runsession_test.go pins the RunSession contract (fix-plan 1.3 Tier B RS-01,
// design §2.1/§7.2 row 1): ring wrap + firstSeq advance, gapless
// subscribe-under-append (deterministic property via rapid AND a concurrent -race
// exercise), lifecycle-never-dropped under a full channel, and terminal closing
// every subscriber channel exactly once. Pure and daemon-free; the package goleak
// TestMain (main_test.go) guards every helper goroutine.

// TestRunSessionRingWrapAdvancesFirstSeq: a cap-4 ring absorbing 6 events keeps
// only seqs 3..6, refuses a replay from before the window (the 410 seam, §3.3),
// replays the surviving window verbatim (same event instances), and treats a
// beyond-tail fromSeq as live-only.
func TestRunSessionRingWrapAdvancesFirstSeq(t *testing.T) {
	sess := newRunSession("run-w", "thread-w", "id-w", 4, 8, nil)
	ctx := context.Background()

	evs := make([]events.Event, 0, 6)
	for i := 0; i < 6; i++ {
		ev := events.NewTextMessageContentEvent("m", fmt.Sprintf("d%d", i))
		evs = append(evs, ev)
		if !sess.append(ctx, ev) {
			t.Fatalf("append %d refused", i)
		}
	}
	sess.mu.Lock()
	first, next := sess.firstSeq, sess.nextSeq
	sess.mu.Unlock()
	if first != 3 || next != 7 {
		t.Fatalf("firstSeq=%d nextSeq=%d, want 3 and 7 after wrapping 6 events into cap 4", first, next)
	}

	if _, _, ok := sess.subscribeFrom(0); ok {
		t.Fatal("subscribeFrom(0) accepted after the ring rotated past seq 1; want replay-gap refusal")
	}
	if _, _, ok := sess.subscribeFrom(1); ok {
		t.Fatal("subscribeFrom(1) accepted; seq 2 rotated out, want replay-gap refusal")
	}

	ch, cancel, ok := sess.subscribeFrom(2)
	if !ok {
		t.Fatal("subscribeFrom(2) refused; seq 3 is the oldest surviving entry")
	}
	for want := int64(3); want <= 6; want++ {
		sev := <-ch
		if sev.Seq != want {
			t.Fatalf("replayed seq %d, want %d", sev.Seq, want)
		}
		if sev.Ev != evs[want-1] {
			t.Fatalf("replayed seq %d carries a different event instance than appended", want)
		}
	}
	cancel()

	tail, _, ok := sess.subscribeFrom(99)
	if !ok {
		t.Fatal("subscribeFrom beyond the tail refused; want live-only subscription")
	}
	if len(tail) != 0 {
		t.Fatalf("beyond-tail subscription replayed %d events, want 0", len(tail))
	}
	sess.finish()
}

// TestRunSessionPropertyGaplessSuffix (rapid, §7.2): for random ring caps, run
// lengths, and subscribe points, every accepted subscriber drains a strictly
// increasing, gap-free, duplicate-free suffix from+1..total, the gap refusal
// matches the firstSeq window exactly, and firstSeq tracks max(1, n-cap+1) after
// every append.
func TestRunSessionPropertyGaplessSuffix(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ringCap := rapid.IntRange(1, 32).Draw(rt, "ringCap")
		total := rapid.IntRange(1, 96).Draw(rt, "total")
		nSubs := rapid.IntRange(1, 4).Draw(rt, "nSubs")
		attachAt := make([]int, nSubs)
		for i := range attachAt {
			attachAt[i] = rapid.IntRange(0, total).Draw(rt, fmt.Sprintf("attach%d", i))
		}

		sess := newRunSession("run-p", "thread-p", "id-p", ringCap, total+1, nil)
		ctx := context.Background()

		firstSeqAt := func(appended int) int64 {
			if appended-ringCap+1 > 1 {
				return int64(appended - ringCap + 1)
			}
			return 1
		}
		type liveSub struct {
			from int64
			ch   <-chan seqEvent
		}
		var live []liveSub
		subscribe := func(appended int) {
			for i := range attachAt {
				if attachAt[i] != appended {
					continue
				}
				from := int64(rapid.IntRange(0, appended).Draw(rt, fmt.Sprintf("from%d", i)))
				ch, _, ok := sess.subscribeFrom(from)
				wantOK := from+1 >= firstSeqAt(appended)
				if ok != wantOK {
					rt.Fatalf("subscribeFrom(%d) at %d appended: ok=%v, want %v (firstSeq %d)",
						from, appended, ok, wantOK, firstSeqAt(appended))
				}
				if ok {
					live = append(live, liveSub{from: from, ch: ch})
				}
			}
		}

		subscribe(0)
		for n := 1; n <= total; n++ {
			if !sess.append(ctx, events.NewTextMessageContentEvent("m", "x")) {
				rt.Fatalf("append %d refused", n)
			}
			sess.mu.Lock()
			gotFirst := sess.firstSeq
			sess.mu.Unlock()
			if gotFirst != firstSeqAt(n) {
				rt.Fatalf("after %d appends firstSeq=%d, want %d", n, gotFirst, firstSeqAt(n))
			}
			subscribe(n)
		}
		sess.finish()

		for _, sub := range live {
			want := sub.from + 1
			for sev := range sub.ch {
				if sev.Seq != want {
					rt.Fatalf("subscriber from=%d saw seq %d, want %d (gap or duplicate)", sub.from, sev.Seq, want)
				}
				want++
			}
			if want != int64(total)+1 {
				rt.Fatalf("subscriber from=%d suffix ended at %d, want %d", sub.from, want-1, total)
			}
		}
	})
}

// TestRunSessionGaplessSubscribeUnderAppendRace: N subscribers attach WHILE the
// producer is appending; because subscribeFrom snapshots and registers under the
// same mutex append holds, every one of them must still see the full contiguous
// 1..total with no gap and no duplicate. Run under -race this also proves the
// mutex discipline.
func TestRunSessionGaplessSubscribeUnderAppendRace(t *testing.T) {
	defer goleak.VerifyNone(t)
	const total = 400
	const nSubs = 8
	sess := newRunSession("run-r", "thread-r", "id-r", total, total, nil)
	ctx := context.Background()

	appendDone := make(chan struct{})
	go func() {
		defer close(appendDone)
		for i := 0; i < total; i++ {
			if !sess.append(ctx, events.NewTextMessageContentEvent("m", "x")) {
				t.Error("append refused mid-run")
				return
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, nSubs)
	for i := 0; i < nSubs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, _, ok := sess.subscribeFrom(0)
			if !ok {
				errs <- fmt.Errorf("subscribeFrom(0) refused although the ring never wraps here")
				return
			}
			want := int64(1)
			for sev := range ch {
				if sev.Seq != want {
					errs <- fmt.Errorf("saw seq %d, want %d (gap or duplicate)", sev.Seq, want)
					return
				}
				want++
			}
			if want != total+1 {
				errs <- fmt.Errorf("suffix ended at %d, want %d", want-1, total)
			}
		}()
	}

	<-appendDone
	sess.finish()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestRunSessionLifecycleNeverDroppedUnderFullChannel: with a full cap-2 channel a
// third delta is refused (drop-on-full, T-12-09) while a lifecycle frame blocks
// until the drain frees a slot and is then delivered (WR-01) — never dropped.
func TestRunSessionLifecycleNeverDroppedUnderFullChannel(t *testing.T) {
	defer goleak.VerifyNone(t)
	sess := newRunSession("run-l", "thread-l", "id-l", 16, 2, nil)
	ctx := context.Background()
	ch, _, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("subscribeFrom refused on a fresh session")
	}
	for i := 0; i < 2; i++ {
		if !sess.append(ctx, events.NewTextMessageContentEvent("m", "x")) {
			t.Fatalf("append %d refused", i)
		}
	}
	if !sess.append(ctx, events.NewTextMessageContentEvent("m", "overflow")) {
		t.Fatal("overflow delta append aborted; want dropped-but-continue")
	}

	delivered := make(chan bool, 1)
	go func() { delivered <- sess.append(ctx, events.NewTextMessageEndEvent("m")) }()
	if first := <-ch; first.Seq != 1 {
		t.Fatalf("drained seq %d first, want 1 (FIFO)", first.Seq)
	}
	select {
	case okAppend := <-delivered:
		if !okAppend {
			t.Fatal("lifecycle append aborted, want delivered after the drain freed a slot")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle append still blocked after a channel slot freed")
	}

	sess.finish()
	var got []int64
	for sev := range ch {
		got = append(got, sev.Seq)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("drained %v, want [2 4]: delta seq 3 dropped on full, lifecycle seq 4 delivered", got)
	}
}

// TestRunSessionLifecycleAbortsOnCtxCancel: a blocking lifecycle send unwinds with
// append=false when the producer ctx cancels — the detached producer's only
// self-escape besides the subscriber's gone signal.
func TestRunSessionLifecycleAbortsOnCtxCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	sess := newRunSession("run-c", "thread-c", "id-c", 8, 1, nil)
	ctx := context.Background()
	if _, _, ok := sess.subscribeFrom(0); !ok {
		t.Fatal("subscribeFrom refused on a fresh session")
	}
	if !sess.append(ctx, events.NewTextMessageContentEvent("m", "x")) {
		t.Fatal("fill append refused")
	}

	cctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- sess.append(cctx, events.NewTextMessageEndEvent("m")) }()
	cancel()
	select {
	case okAppend := <-result:
		if okAppend {
			t.Fatal("lifecycle append returned true on a cancelled ctx with a full channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle append did not unwind on ctx-cancel")
	}
	sess.finish()
}

// TestRunSessionUnsubscribeUnblocksLifecycleSend: the subscriber's cancel (gone
// signal) is what keeps an abandoned full channel from wedging the detached
// producer — its ctx, unlike the request ctx, never cancels on client disconnect,
// so without gone this blocking send would only unwind at the wallclock cap.
func TestRunSessionUnsubscribeUnblocksLifecycleSend(t *testing.T) {
	defer goleak.VerifyNone(t)
	sess := newRunSession("run-g", "thread-g", "id-g", 8, 1, nil)
	ctx := context.Background()
	ch, cancelSub, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("subscribeFrom refused on a fresh session")
	}
	if !sess.append(ctx, events.NewTextMessageContentEvent("m", "x")) {
		t.Fatal("fill append refused")
	}

	result := make(chan bool, 1)
	go func() { result <- sess.append(ctx, events.NewTextMessageEndEvent("m")) }()
	cancelSub()
	select {
	case okAppend := <-result:
		if !okAppend {
			t.Fatal("append aborted on unsubscribe; want continue-without-the-subscriber")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle append stayed wedged after the subscriber unsubscribed")
	}

	// The pruned subscriber receives nothing further.
	if !sess.append(ctx, events.NewTextMessageContentEvent("m", "after")) {
		t.Fatal("post-unsubscribe append refused")
	}
	if got := len(ch); got != 1 {
		t.Fatalf("unsubscribed channel buffers %d events, want only the pre-unsubscribe 1", got)
	}
	cancelSub() // idempotent
	sess.finish()
}

// TestRunSessionTerminalClosesAllChannels: finish closes every live subscriber
// channel after its buffered tail (sole sender closes), stamps finishedAt, runs the
// cleanup callback exactly once (idempotent on a double finish), refuses further
// appends, and still serves a terminal replay tail on an already-closed channel
// (§3.3 terminal-run resume).
func TestRunSessionTerminalClosesAllChannels(t *testing.T) {
	defer goleak.VerifyNone(t)
	var cleanups atomic.Int32
	sess := newRunSession("run-t", "thread-t", "id-t", 8, 4, func() { cleanups.Add(1) })
	ctx := context.Background()
	chA, _, okA := sess.subscribeFrom(0)
	chB, _, okB := sess.subscribeFrom(0)
	if !okA || !okB {
		t.Fatal("subscribeFrom refused on a live session")
	}
	for i := 0; i < 3; i++ {
		if !sess.append(ctx, events.NewTextMessageContentEvent("m", "x")) {
			t.Fatalf("append %d refused", i)
		}
	}

	sess.finish()
	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times at terminal, want 1", got)
	}
	if sess.finishedAt.IsZero() {
		t.Fatal("finishedAt not stamped at terminal")
	}
	for name, ch := range map[string]<-chan seqEvent{"A": chA, "B": chB} {
		var seqs []int64
		for sev := range ch {
			seqs = append(seqs, sev.Seq)
		}
		if len(seqs) != 3 || seqs[0] != 1 || seqs[2] != 3 {
			t.Fatalf("subscriber %s drained %v, want [1 2 3] then close", name, seqs)
		}
	}

	sess.finish()
	if got := cleanups.Load(); got != 1 {
		t.Fatalf("double finish reran cleanup: %d", got)
	}
	if sess.append(ctx, events.NewTextMessageContentEvent("m", "late")) {
		t.Fatal("append after terminal must refuse")
	}

	chC, cancelC, okC := sess.subscribeFrom(1)
	if !okC {
		t.Fatal("terminal subscribeFrom refused within the replay window")
	}
	cancelC() // no-op on a terminal subscription
	var tail []int64
	for sev := range chC {
		tail = append(tail, sev.Seq)
	}
	if len(tail) != 2 || tail[0] != 2 || tail[1] != 3 {
		t.Fatalf("terminal replay tail %v, want [2 3] then close", tail)
	}
}
