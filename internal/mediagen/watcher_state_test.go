package mediagen

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCompletionAtWaitBoundaryHasOneOwner(t *testing.T) {
	for _, finishFirst := range []bool{false, true} {
		state := jobHandoff{waiting: true}
		notices := 0
		if finishFirst {
			if state.finish() {
				notices++
			}
			if state.detach() {
				notices++
			}
		} else {
			if state.detach() {
				notices++
			}
			if state.finish() {
				notices++
			}
		}
		if notices != 1 || state.claim() {
			t.Fatalf("handoff lost or duplicated: %#v notices=%d", state, notices)
		}
	}
}

func TestInlineClaimSuppressesWake(t *testing.T) {
	state := jobHandoff{waiting: true}
	if state.finish() || !state.claim() || state.detach() {
		t.Fatal("inline completion acquired twice")
	}
	if !state.settled() {
		t.Fatalf("a claimed completion has nothing left to decide: %#v", state)
	}
}

func TestCompletionBeforeRegistrationNotifiesOnce(t *testing.T) {
	detached := jobHandoff{}
	if !detached.finish() {
		t.Fatal("a job that finishes with no inline waiter must wake its conversation")
	}
	if detached.finish() || detached.detach() || detached.claim() || detached.release() {
		t.Fatalf("a notified completion was handed out again: %#v", detached)
	}

	inline := jobHandoff{waiting: true}
	if inline.finish() {
		t.Fatal("a job finished before its waiter waited must stay with that waiter")
	}
	if inline.settled() || !inline.claim() {
		t.Fatalf("the registered waiter must own a completion that preceded its wait: %#v", inline)
	}
}

func TestCancellationAfterFinishBeforeClaimHandsOver(t *testing.T) {
	state := jobHandoff{waiting: true}
	if state.finish() {
		t.Fatal("finish notified while the waiter was still registered")
	}
	if !state.detach() {
		t.Fatal("a cancelled waiter must hand the finished job to the wake path")
	}
	if state.claim() || state.detach() || state.release() || !state.settled() {
		t.Fatalf("a handed-over completion was acquired again: %#v", state)
	}
}

func TestReleaseAfterFailedDeliveryWakesOnce(t *testing.T) {
	state := jobHandoff{waiting: true}
	if state.finish() || !state.claim() {
		t.Fatalf("the waiter must own the completion first: %#v", state)
	}
	if !state.release() {
		t.Fatal("a delivery that failed before its database claim must wake the conversation")
	}
	if state.claimed || state.release() || state.detach() || state.finish() || state.claim() {
		t.Fatalf("a released completion was handed out twice: %#v", state)
	}
}

func TestDetachBeforeFinishLeavesTheWakeToFinish(t *testing.T) {
	state := jobHandoff{waiting: true}
	if state.detach() || state.release() || state.settled() {
		t.Fatalf("an unfinished job has nothing to notify: %#v", state)
	}
	if !state.finish() || state.finish() || state.release() {
		t.Fatalf("a detached job must notify exactly once when it finishes: %#v", state)
	}
}

// TestHandoffGuardsEachBlockAlone pins every guard of claim and notifyIfDetached with a state
// in which that guard is the only reason to refuse.
func TestHandoffGuardsEachBlockAlone(t *testing.T) {
	claims := map[string]jobHandoff{
		"not terminal": {waiting: true},
		"not waiting":  {terminal: true},
		"claimed":      {terminal: true, waiting: true, claimed: true},
		"notified":     {terminal: true, waiting: true, notified: true},
	}
	for name, state := range claims {
		if state.claim() {
			t.Errorf("claim with %s: acquired %#v", name, state)
		}
	}
	if open := (jobHandoff{terminal: true, waiting: true}); !open.claim() {
		t.Error("claim refused the one state it must accept")
	}

	notices := map[string]jobHandoff{
		"not terminal": {},
		"waiting":      {terminal: true, waiting: true},
		"claimed":      {terminal: true, claimed: true},
		"notified":     {terminal: true, notified: true},
	}
	for name, state := range notices {
		if state.notifyIfDetached() {
			t.Errorf("notify with %s: notified %#v", name, state)
		}
	}
	if open := (jobHandoff{terminal: true}); !open.notifyIfDetached() || !open.notified {
		t.Error("notifyIfDetached refused the one state it must accept, or did not record it")
	}
}

func TestWatcherHandsAnInlineCompletionToItsWaiterWithoutWake(t *testing.T) {
	provider := newFakeProvider(t, statuses(
		`{"id":"vid_inline","status":"in_progress"}`,
		`{"id":"vid_inline","status":"completed","usage":{"cost":0.4}}`,
	), serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_inline")

	finished, owns := h.watcher.Track(job, true).Wait(context.Background(), time.Minute)
	if !owns || finished.Status != StatusCompleted || finished.AssetID == "" || finished.CostUSD == nil || *finished.CostUSD != 0.4 {
		t.Fatalf("Wait = %+v owns=%v; want the completed row with its asset and cost, owned inline", finished, owns)
	}
	h.stop(t)
	if notices := h.drainNotices(); len(notices) != 0 || trackedJobs(h.watcher) != 0 {
		t.Fatalf("notices = %+v, entries = %d; an inline-owned completion wakes nothing and leaves no entry",
			notices, trackedJobs(h.watcher))
	}
	if h.store.count("Progress") != 1 || h.store.count("Complete") != 1 || provider.posts.Load() != 0 {
		t.Fatalf("Progress=%d Complete=%d POSTs=%d; want one progress write, one completion, no submission",
			h.store.count("Progress"), h.store.count("Complete"), provider.posts.Load())
	}
	for _, request := range provider.recorded() {
		if !strings.HasSuffix(request, " Bearer key-"+job.IdentityID) {
			t.Fatalf("request %q did not carry the owner's credential", request)
		}
	}
}

func TestWatcherOutlivesTheToolContext(t *testing.T) {
	release := make(chan struct{})
	provider := newFakeProvider(t, func(n int32, w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		respondJSON(w, http.StatusOK, `{"id":"vid_turn","status":"completed","usage":{"cost":0.2}}`)
	}, serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_turn")
	waiter := h.watcher.Track(job, true)

	turn, endTurn := context.WithCancel(context.Background())
	endTurn()
	if _, owns := waiter.Wait(turn, time.Minute); owns {
		t.Fatal("a cancelled turn claimed a job that had not finished")
	}
	close(release)
	job.Status = StatusCompleted
	if notice := h.awaitNotice(t); notice != completionOf(job) {
		t.Fatalf("notice = %+v, want the completion of %s", notice, job.ID)
	}
	waiter.Release()
	h.stop(t)
	if extra := h.drainNotices(); len(extra) != 0 {
		t.Fatalf("the completion was notified again: %+v", extra)
	}
}

func TestWatcherWakesOnceWhenTheInlineWaitRunsOut(t *testing.T) {
	release := make(chan struct{})
	provider := newFakeProvider(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		respondJSON(w, http.StatusOK, `{"id":"vid_late","status":"completed","usage":{"cost":0.3}}`)
	}, serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_late")
	waiter := h.watcher.Track(job, true)

	if _, owns := waiter.Wait(context.Background(), 0); owns {
		t.Fatal("an unfinished job was claimed when the inline wait ran out")
	}
	waiter.Release()
	waiter.Release()
	close(release)
	if notice := h.awaitNotice(t); notice.JobID != job.ID || notice.Status != StatusCompleted {
		t.Fatalf("notice = %+v, want the completion of %s", notice, job.ID)
	}
	h.stop(t)
	if extra := h.drainNotices(); len(extra) != 0 {
		t.Fatalf("one timeout produced %d extra notices: %+v", len(extra), extra)
	}
}

func TestWatcherReleaseAfterAClaimedCompletion(t *testing.T) {
	cases := map[string]struct {
		final   string
		deliver func(t *testing.T, h *watcherHarness, job Job)
		wakes   int
	}{
		"delivery claim committed": {
			final: `{"id":"vid_claim","status":"completed"}`,
			deliver: func(t *testing.T, h *watcherHarness, job Job) {
				if _, won, err := h.store.ClaimDelivery(context.Background(), job.IdentityID, job.ID, job.ConversationID, "call-deliver"); err != nil || !won {
					t.Fatalf("ClaimDelivery = %v, %v", won, err)
				}
			},
		},
		"delivery failed before its claim": {
			final:   `{"id":"vid_claim","status":"completed"}`,
			deliver: func(*testing.T, *watcherHarness, Job) {},
			wakes:   1,
		},
		"claim unreadable": {
			final: `{"id":"vid_claim","status":"completed"}`,
			deliver: func(_ *testing.T, h *watcherHarness, _ Job) {
				h.store.failNext("Get", errors.New("connection reset by peer"))
			},
			wakes: 1,
		},
		"failure reported inline": {
			final:   `{"id":"vid_claim","status":"failed","error":"Content policy violation"}`,
			deliver: func(*testing.T, *watcherHarness, Job) {},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := newFakeProvider(t, statuses(tc.final), serveClip)
			h := newWatcherHarness(t, context.Background(), provider, nil)
			job := h.insertJob(t, "vid_claim")
			waiter := h.watcher.Track(job, true)
			finished, owns := waiter.Wait(context.Background(), time.Minute)
			if !owns {
				t.Fatalf("the waiter did not own %+v", finished)
			}
			tc.deliver(t, h, finished)
			waiter.Release()
			waiter.Release()
			if d := h.store.deadline("Get"); finished.Status == StatusCompleted && (d.IsZero() || time.Until(d) > deliveryCheckTimeout) {
				t.Fatalf("Release read the delivery claim with deadline %v, want one within %v", d, deliveryCheckTimeout)
			}
			h.stop(t)
			notices := h.drainNotices()
			if len(notices) != tc.wakes {
				t.Fatalf("wakes = %+v, want %d", notices, tc.wakes)
			}
			if tc.wakes == 1 && notices[0] != completionOf(finished) {
				t.Fatalf("notice = %+v, want %+v", notices[0], completionOf(finished))
			}
		})
	}
}

func TestWatcherTracksAJobOnce(t *testing.T) {
	release := make(chan struct{})
	provider := newFakeProvider(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		respondJSON(w, http.StatusOK, `{"id":"vid_once","status":"completed"}`)
	}, serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_once")

	owner := h.watcher.Track(job, true)
	duplicate := h.watcher.Track(job, true)
	resumed := h.watcher.Track(job, false)
	if duplicate.entry != owner.entry || resumed.entry != owner.entry || duplicate.owner || resumed.owner {
		t.Fatal("a second Track replaced the job's entry or its owner")
	}
	duplicate.Release()
	turn, endTurn := context.WithCancel(context.Background())
	endTurn()
	if _, owns := duplicate.Wait(turn, 0); owns {
		t.Fatal("a waiter that owns nothing claimed the job")
	}
	close(release)
	if finished, owns := owner.Wait(context.Background(), time.Minute); !owns || finished.Status != StatusCompleted {
		t.Fatalf("owner Wait = %+v owns=%v; the duplicates must not have detached the owner", finished, owns)
	}
	if _, owns := duplicate.Wait(context.Background(), 0); owns {
		t.Fatal("a waiter that owns nothing reported the owner's claim as its own")
	}
	h.stop(t)
	if notices := h.drainNotices(); len(notices) != 0 {
		t.Fatalf("notices = %+v, want none", notices)
	}
	if provider.polls.Load() != 1 || provider.downloads.Load() != 1 || h.store.count("Complete") != 1 {
		t.Fatalf("polls=%d downloads=%d completes=%d; want one supervisor's single pass",
			provider.polls.Load(), provider.downloads.Load(), h.store.count("Complete"))
	}
}

func TestWatcherTracksATerminalJobWithoutPolling(t *testing.T) {
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	completed := func() Job {
		job := h.insertJob(t, "vid_"+uuid.NewString())
		job.Status, job.AssetID = StatusCompleted, uuid.NewString()
		return h.store.seed(job)
	}

	undelivered := completed()
	waiter := h.watcher.Track(undelivered, false)
	if notice := h.awaitNotice(t); notice != completionOf(undelivered) {
		t.Fatalf("notice = %+v, want %+v", notice, completionOf(undelivered))
	}
	select {
	case <-waiter.entry.done:
	default:
		t.Fatal("a terminal job's done channel stayed open")
	}

	inline := completed()
	if finished, owns := h.watcher.Track(inline, true).Wait(context.Background(), 0); !owns || finished.ID != inline.ID {
		t.Fatalf("Wait = %+v owns=%v; the registered waiter owns a job that finished before it waited", finished, owns)
	}

	abandoned := completed()
	turn, endTurn := context.WithCancel(context.Background())
	endTurn()
	if _, owns := h.watcher.Track(abandoned, true).Wait(turn, time.Minute); owns {
		t.Fatal("a cancelled turn claimed a finished job it can no longer deliver")
	}
	if notice := h.awaitNotice(t); notice != completionOf(abandoned) {
		t.Fatalf("notice = %+v, want the hand-over of %+v", notice, completionOf(abandoned))
	}
	h.stop(t)
	if extra := h.drainNotices(); len(extra) != 0 || trackedJobs(h.watcher) != 0 {
		t.Fatalf("notices = %+v, entries = %d; want no other notice and no entry left once settled", extra, trackedJobs(h.watcher))
	}
}

func TestWatcherReleaseAfterStopLeavesTheCompletionForResume(t *testing.T) {
	provider := newFakeProvider(t, statuses(`{"id":"vid_shutdown","status":"completed"}`), serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_shutdown")
	waiter := h.watcher.Track(job, true)
	if _, owns := waiter.Wait(context.Background(), time.Minute); !owns {
		t.Fatal("the waiter did not own the completion")
	}

	h.stop(t)
	waiter.Release()
	if notices := h.drainNotices(); len(notices) != 0 {
		t.Fatalf("a release after Stop woke a shutting-down dispatcher: %+v", notices)
	}
	recoverable, err := h.store.Recoverable(context.Background(), job.IdentityID)
	if err != nil || len(recoverable) != 1 || recoverable[0].Status != StatusCompleted {
		t.Fatalf("Recoverable = %+v, %v; want the undelivered completion for the next boot", recoverable, err)
	}
}

// TestWatcherKeepsARetrackedEntryWhenAnOldWaiterSettles releases a waiter whose entry was
// already replaced by a new Track of the same job: only the stale entry may be dropped.
func TestWatcherKeepsARetrackedEntryWhenAnOldWaiterSettles(t *testing.T) {
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_retracked")
	job.Status, job.AssetID = StatusCompleted, uuid.NewString()
	job = h.store.seed(job)

	first := h.watcher.Track(job, true)
	if _, owns := first.Wait(context.Background(), 0); !owns {
		t.Fatal("the first waiter did not own the completion")
	}
	second := h.watcher.Track(job, true)
	if second.entry == first.entry || !second.owner {
		t.Fatal("tracking a settled job again did not install a new owner")
	}
	if _, won, err := h.store.ClaimDelivery(context.Background(), job.IdentityID, job.ID, job.ConversationID, "call-deliver"); err != nil || !won {
		t.Fatalf("ClaimDelivery = %v, %v", won, err)
	}
	first.Release()
	if tracked := trackedJobs(h.watcher); tracked != 1 {
		t.Fatalf("entries = %d after the old waiter released, want the new entry kept", tracked)
	}
	if _, owns := second.Wait(context.Background(), 0); !owns || trackedJobs(h.watcher) != 0 {
		t.Fatal("the new owner lost its claim or its settled entry stayed")
	}
	h.stop(t)
	if notices := h.drainNotices(); len(notices) != 0 {
		t.Fatalf("notices = %+v, want none", notices)
	}
}

func TestNewWatcherRefusesAMiswiredWatcher(t *testing.T) {
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	store := newFakeJobStore(time.Now)
	client := NewClient(provider.Client(), 1<<20)
	credentials := &fakeCredentials{answers: []credentialAnswer{{baseURL: provider.URL}}}
	assets := &fakeVideoAssets{store: store, bySource: map[string]string{}}
	notify := func(Completion) {}
	valid := WatcherOptions{PollInterval: time.Millisecond, MaxAge: time.Minute, MaxVideoBytes: 1}
	with := func(change func(*WatcherOptions)) WatcherOptions {
		opts := valid
		change(&opts)
		return opts
	}
	ctx := context.Background()
	for name, build := range map[string]func(){
		"no store":        func() { NewWatcher(ctx, nil, client, credentials, assets, notify, valid) },
		"no client":       func() { NewWatcher(ctx, store, nil, credentials, assets, notify, valid) },
		"no credentials":  func() { NewWatcher(ctx, store, client, nil, assets, notify, valid) },
		"no video assets": func() { NewWatcher(ctx, store, client, credentials, nil, notify, valid) },
		"no notify":       func() { NewWatcher(ctx, store, client, credentials, assets, nil, valid) },
		"zero poll interval": func() {
			NewWatcher(ctx, store, client, credentials, assets, notify, with(func(o *WatcherOptions) { o.PollInterval = 0 }))
		},
		"negative ceiling": func() {
			NewWatcher(ctx, store, client, credentials, assets, notify, with(func(o *WatcherOptions) { o.MaxAge = -time.Minute }))
		},
		"zero byte limit": func() {
			NewWatcher(ctx, store, client, credentials, assets, notify, with(func(o *WatcherOptions) { o.MaxVideoBytes = 0 }))
		},
		"unbounded byte limit": func() {
			NewWatcher(ctx, store, client, credentials, assets, notify, with(func(o *WatcherOptions) { o.MaxVideoBytes = math.MaxInt64 }))
		},
	} {
		if !panics(build) {
			t.Errorf("NewWatcher with %s built a watcher", name)
		}
	}
	w := NewWatcher(ctx, store, client, credentials, assets, notify, valid)
	if err := w.Stop(ctx); err != nil {
		t.Fatalf("the valid watcher's Stop = %v", err)
	}
}

func panics(build func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	build()
	return false
}
