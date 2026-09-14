package main

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
)

var errRecoveryStoreUnused = errors.New("not reached by boot recovery")

// recoveryJobStore answers Recoverable per owner from rows and records which owners asked;
// boot recovery reaches no other store method.
type recoveryJobStore struct {
	mu     sync.Mutex
	rows   map[string][]mediagen.Job
	owners []string
}

var _ mediagen.JobStore = (*recoveryJobStore)(nil)

func (s *recoveryJobStore) Recoverable(_ context.Context, ownerID string) ([]mediagen.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owners = append(s.owners, ownerID)
	return slices.Clone(s.rows[ownerID]), nil
}

func (s *recoveryJobStore) askedOwners() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.owners)
}

func (*recoveryJobStore) Insert(context.Context, mediagen.Job) (mediagen.Job, error) {
	return mediagen.Job{}, errRecoveryStoreUnused
}

func (*recoveryJobStore) Get(context.Context, string, string) (mediagen.Job, error) {
	return mediagen.Job{}, errRecoveryStoreUnused
}

func (*recoveryJobStore) Progress(context.Context, string, string, mediagen.Status, *float64, *mediagen.Error) (mediagen.Job, error) {
	return mediagen.Job{}, errRecoveryStoreUnused
}

func (*recoveryJobStore) Complete(context.Context, string, string, string, *float64) (mediagen.Job, error) {
	return mediagen.Job{}, errRecoveryStoreUnused
}

func (*recoveryJobStore) ClaimDelivery(context.Context, string, string, string, string) (mediagen.Job, bool, error) {
	return mediagen.Job{}, false, errRecoveryStoreUnused
}

// scriptedIdentityLister answers ListIdentities with the next queued error, then with rows.
type scriptedIdentityLister struct {
	rows  []identity.Identity
	errs  []error
	calls int
}

func (l *scriptedIdentityLister) ListIdentities(context.Context) ([]identity.Identity, error) {
	l.calls++
	if len(l.errs) > 0 {
		err := l.errs[0]
		l.errs = l.errs[1:]
		return nil, err
	}
	return slices.Clone(l.rows), nil
}

// scriptedResumer fails an owner's Resume with its queued errors, then succeeds.
type scriptedResumer struct {
	fail  map[string][]error
	calls []string
}

func (r *scriptedResumer) Resume(_ context.Context, ownerID string) error {
	r.calls = append(r.calls, ownerID)
	if queue := r.fail[ownerID]; len(queue) > 0 {
		r.fail[ownerID] = queue[1:]
		return queue[0]
	}
	return nil
}

var bootRoster = []identity.Identity{
	{ID: "owner-a", Kind: "user"},
	{ID: "owner-b", Kind: "user", Deactivated: true},
	{ID: "", Kind: "user"},
	{ID: "owner-c", Kind: "user"},
}

func testMediaDeps(t *testing.T, store mediagen.JobStore, resolver snapshotResolver) *mediaDeps {
	t.Helper()
	return &mediaDeps{
		credentials:   mediaCredentials{resolver: resolver},
		client:        mediagen.NewClient(nil, 1<<20),
		jobs:          store,
		maxVideoBytes: mediagen.DefaultAssetMaxVideoBytes,
	}
}

func stopWatcher(t *testing.T, watcher *mediagen.Watcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := watcher.Stop(ctx); err != nil {
		t.Fatalf("watcher Stop: %v", err)
	}
}

func recoverableVideoJob(t *testing.T, id, owner string, status mediagen.Status) mediagen.Job {
	t.Helper()
	request, err := mediagen.JobRequest(
		mediagen.VideoRequest{Model: mediagen.DefaultVideoModel, Prompt: "a lighthouse at dusk"},
		mediagen.JobAudit{Origin: "https://openrouter.ai/api/v1"},
	)
	if err != nil {
		t.Fatalf("JobRequest: %v", err)
	}
	return mediagen.Job{
		ID: id, IdentityID: owner, ConversationID: "conv-" + owner, ToolCallID: "call-1",
		ProviderJobID: "provider-" + id, Model: mediagen.DefaultVideoModel, Request: request,
		Status: status, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func TestNewMediaWatcherStaysOffWithoutItsDependencies(t *testing.T) {
	notify := func(mediagen.Completion) {}
	withCeiling := func(maxVideoBytes int64) *mediaDeps {
		media := testMediaDeps(t, &recoveryJobStore{}, nil)
		media.maxVideoBytes = maxVideoBytes
		return media
	}
	for name, media := range map[string]*mediaDeps{
		"no media dependencies":        nil,
		"no job store without pool":    testMediaDeps(t, nil, nil),
		"video ceiling unreadable":     withCeiling(0),
		"video ceiling bounds nothing": withCeiling(math.MaxInt64),
	} {
		t.Run(name, func(t *testing.T) {
			if watcher := newMediaWatcher(context.Background(), media, notify); watcher != nil {
				stopWatcher(t, watcher)
				t.Fatal("a watcher was built without the dependencies it needs; NewWatcher would panic or supervise nothing")
			}
		})
	}
	if watcher := newMediaWatcher(context.Background(), testMediaDeps(t, &recoveryJobStore{}, nil), notify); watcher == nil {
		t.Fatal("no watcher with a job store and the boot video ceiling")
	} else {
		stopWatcher(t, watcher)
	}
}

// TestMediaJobRecoveryResumesEachActiveOwnerOnceThroughTheWatcher runs the boot pass over a
// real watcher: every active identity's jobs are read through the owner-scoped store, the
// synchronous pass has finished when SweepNow returns (runServe calls it before any request is
// served), and a later pass never resumes an owner again, since a second Resume would wake a
// completed, undelivered job a second time.
func TestMediaJobRecoveryResumesEachActiveOwnerOnceThroughTheWatcher(t *testing.T) {
	store := &recoveryJobStore{rows: map[string][]mediagen.Job{
		"owner-a": {recoverableVideoJob(t, "job-a", "owner-a", mediagen.StatusCompleted)},
	}}
	var mu sync.Mutex
	var completions []mediagen.Completion
	watcher := newMediaWatcher(context.Background(), testMediaDeps(t, store, nil), func(c mediagen.Completion) {
		mu.Lock()
		defer mu.Unlock()
		completions = append(completions, c)
	})
	defer stopWatcher(t, watcher)
	lister := &scriptedIdentityLister{rows: bootRoster}
	recovery := newMediaJobRecovery(watcher, lister)
	defer recovery.Stop()

	recovery.SweepNow(context.Background())
	recovery.SweepNow(context.Background())

	if got, want := store.askedOwners(), []string{"owner-a", "owner-c"}; !slices.Equal(got, want) {
		t.Fatalf("Recoverable owners = %q, want each active owner exactly once: %q", got, want)
	}
	if lister.calls != 1 {
		t.Fatalf("ListIdentities calls = %d, want the boot roster listed once", lister.calls)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []mediagen.Completion{{IdentityID: "owner-a", ConversationID: "conv-owner-a", JobID: "job-a", Status: mediagen.StatusCompleted}}
	if !slices.Equal(completions, want) {
		t.Fatalf("completions = %+v, want the undelivered job woken once: %+v", completions, want)
	}
}

func TestMediaJobRecoveryRetriesUntilEveryOwnerIsResumed(t *testing.T) {
	lister := &scriptedIdentityLister{rows: bootRoster, errs: []error{errors.New("connection refused")}}
	resumer := &scriptedResumer{fail: map[string][]error{"owner-c": {errors.New("statement timeout")}}}
	recovery := &mediaJobRecovery{resumer: resumer, identities: lister}
	ctx := context.Background()

	recovery.sweep(ctx)
	if len(resumer.calls) != 0 || lister.calls != 1 {
		t.Fatalf("after a failed listing: resumes %q, listings %d; want no resume and a retry pending", resumer.calls, lister.calls)
	}
	recovery.sweep(ctx)
	if want := []string{"owner-a", "owner-c"}; !slices.Equal(resumer.calls, want) {
		t.Fatalf("resumes = %q, want %q", resumer.calls, want)
	}
	recovery.sweep(ctx)
	recovery.sweep(ctx)
	if want := []string{"owner-a", "owner-c", "owner-c"}; !slices.Equal(resumer.calls, want) {
		t.Fatalf("resumes = %q, want only the failed owner retried, once it succeeds never again: %q", resumer.calls, want)
	}
	if lister.calls != 2 {
		t.Fatalf("ListIdentities calls = %d, want the roster kept once it was listed", lister.calls)
	}
}

func TestMediaBackgroundWorkIsNilSafeWhenDisabled(t *testing.T) {
	recovery := newMediaJobRecovery(nil, &scriptedIdentityLister{})
	if recovery != nil {
		t.Fatal("recovery without a watcher must be the disabled nil sweeper")
	}
	recovery.SweepNow(context.Background())
	recovery.Start(context.Background())
	recovery.Stop()

	rail := steer.NewPostgresStore(nil, steer.Config{})
	for name, chat := range map[string]*chatEnv{
		"nothing wired": {},
		"no steer rail": {run: &runner.Runner{}},
		"no runner":     {steer: rail},
	} {
		dispatcher := newServeCompletionDispatcher(context.Background(), chat)
		if dispatcher != nil {
			t.Fatalf("%s: a dispatcher was built without the runner and steer rail its wakes need", name)
		}
		dispatcher.NotifyMedia(mediaDone("job-1"))
		dispatcher.NotifyShell(shellDone("sh-1", "exited:0"))
	}
	shutdownBackgroundWork(&serveEnv{chatEnv: &chatEnv{}})
}

type recordingHookSetter struct {
	hook tools.BackgroundShellCompletionHook
}

func (r *recordingHookSetter) SetCompletionHook(hook tools.BackgroundShellCompletionHook) {
	r.hook = hook
}

// TestServeCompletionDispatcherIsTheShellCompletionHook proves the installed hook is the
// dispatcher's shell entry: a completion handed to the hook wakes its conversation under the
// shell source.
func TestServeCompletionDispatcherIsTheShellCompletionHook(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{}
	shells := &recordingHookSetter{}
	dispatcher := installBackgroundCompletions(context.Background(), run, acceptingSteerPusher{}, shells)
	if shells.hook == nil {
		t.Fatal("no completion hook was installed on the background shells")
	}
	shells.hook(shellDone("sh-hooked", "exited:0"))
	wakes := run.waitForWakes(t, 1)
	stopDispatcher(t, dispatcher)
	if wakes[0].source != steer.SourceShell || wakes[0].owner != "owner-1" || !strings.Contains(wakes[0].text, "Background shell sh-hooked ") {
		t.Fatalf("wake = %+v, want the hooked shell completion delivered under the shell source", wakes[0])
	}

	chat := &chatEnv{
		run:         &runner.Runner{},
		steer:       steer.NewPostgresStore(nil, steer.Config{}),
		toolHandles: runtimeToolHandles{BackgroundShells: tools.NewBackgroundShells(nil)},
	}
	served := newServeCompletionDispatcher(context.Background(), chat)
	if served == nil {
		t.Fatal("no dispatcher with a runner and the steer rail")
	}
	stopDispatcher(t, served)
}

// cancelObservingResolver blocks the supervisor's credential lookup until the watcher is
// stopped, then reports whether the dispatcher was still open at that moment.
type cancelObservingResolver struct {
	dispatcher *backgroundCompletionDispatcher
	entered    chan struct{}
	openAtStop chan bool
}

func (r *cancelObservingResolver) SnapshotFor(ctx context.Context, _ string) (llm.RuntimeSnapshot, error) {
	r.entered <- struct{}{}
	<-ctx.Done()
	r.dispatcher.mu.Lock()
	r.openAtStop <- !r.dispatcher.closed
	r.dispatcher.mu.Unlock()
	return llm.RuntimeSnapshot{}, ctx.Err()
}

// TestShutdownBackgroundWorkStopsTheWatcherBeforeTheDispatcher: the media producer is
// stopped and joined while the dispatcher still accepts completions, and the dispatcher is
// closed when shutdown returns.
func TestShutdownBackgroundWorkStopsTheWatcherBeforeTheDispatcher(t *testing.T) {
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), &fakeBackgroundCompletionRunner{}, acceptingSteerPusher{})
	resolver := &cancelObservingResolver{dispatcher: dispatcher, entered: make(chan struct{}, 1), openAtStop: make(chan bool, 1)}
	watcher := newMediaWatcher(context.Background(), testMediaDeps(t, &recoveryJobStore{}, resolver), dispatcher.NotifyMedia)
	env := &serveEnv{
		chatEnv:               &chatEnv{},
		backgroundCompletions: dispatcher,
		mediaWatcher:          watcher,
		mediaRecovery:         newMediaJobRecovery(watcher, &scriptedIdentityLister{}),
	}
	watcher.Track(recoverableVideoJob(t, "job-running", "owner-a", mediagen.StatusInProgress), false)
	waitStarted(t, resolver.entered, "the job's supervisor")

	shutdownBackgroundWork(env)

	select {
	case open := <-resolver.openAtStop:
		if !open {
			t.Fatal("the dispatcher closed before the watcher's supervisors were stopped")
		}
	default:
		t.Fatal("shutdown returned before the watcher's supervisor was cancelled and joined")
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if !dispatcher.closed {
		t.Fatal("shutdown left the background completion dispatcher open")
	}
}

func TestVideoGenerateHandleRetainedWithoutDependencies(t *testing.T) {
	reg, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
	video := handles.VideoGenerate
	if video == nil || *video != (tools.VideoGenerate{}) {
		t.Fatalf("the registry must retain a dependency-free video_generate handle for serve-boot wiring: %+v", video)
	}
	registered, ok := reg.Get("video_generate")
	if !ok || registered != video || !registered.Spec().Deferred {
		t.Fatal("the registry must list the retained, deferred video_generate tool")
	}
}

func TestWireVideoToolSharesTheImageToolsDependenciesAndTheWatcher(t *testing.T) {
	_, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
	svc := &assets.Service{Limits: assets.Limits{MaxImageBytes: 12 << 20, MaxVideoBytes: 30 << 20}}
	chat := &chatEnv{cfg: &config.Config{}, assets: svc, toolHandles: handles}
	media := newMediaDeps(chat)
	media.jobs = &recoveryJobStore{}
	watcher := newMediaWatcher(context.Background(), media, func(mediagen.Completion) {})
	defer stopWatcher(t, watcher)
	wireMediaTools(chat, media)

	wireVideoTool(chat, media, watcher)

	video, image := handles.VideoGenerate, handles.ImageGenerate
	if video.Credentials != image.Credentials || video.Settings != image.Settings || video.Catalog != image.Catalog ||
		video.Client != image.Client || video.References != image.References {
		t.Fatal("video_generate must reuse the one credential port, settings, catalog, client and reference adapter")
	}
	if video.Jobs != media.jobs || video.Watcher != watcher || video.MaxImageBytes != 12<<20 || video.MaxVideoBytes != 30<<20 {
		t.Fatalf("video_generate = %+v, want the job store, the daemon's watcher and both boot ceilings", video)
	}
	if clips, ok := video.VideoAssets.(mediaAssetAdapter); !ok || clips.svc != svc {
		t.Fatalf("VideoAssets = %#v, want the asset adapter the watcher ingests through", video.VideoAssets)
	}
}

func TestWireVideoToolLeavesTheToolRefusingWhenItCannotBeServed(t *testing.T) {
	svc := &assets.Service{Limits: assets.Limits{MaxImageBytes: 12 << 20, MaxVideoBytes: 30 << 20}}
	for name, tc := range map[string]struct {
		secret  string
		assets  *assets.Service
		watcher bool
	}{
		"watcher disabled":        {assets: svc},
		"no asset service":        {watcher: true},
		"unreadable settings key": {secret: "not-hex", assets: svc, watcher: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
			chat := &chatEnv{cfg: &config.Config{AuthulaSecret: tc.secret}, assets: tc.assets, toolHandles: handles}
			var watcher *mediagen.Watcher
			if tc.watcher {
				watcher = newMediaWatcher(context.Background(), testMediaDeps(t, &recoveryJobStore{}, nil), func(mediagen.Completion) {})
				defer stopWatcher(t, watcher)
			}
			wireVideoTool(chat, newMediaDeps(chat), watcher)
			if video := handles.VideoGenerate; *video != (tools.VideoGenerate{}) {
				t.Fatalf("video_generate partially wired: %+v", video)
			}
		})
	}
}
