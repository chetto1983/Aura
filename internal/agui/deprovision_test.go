package agui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// --- reverse-leg fakes (the objectstore/filesystem/journal fakes are shared from
// onboarding_provision_resources_test.go) ---

type fakeDeactivator struct {
	mu        sync.Mutex
	marked    map[string]time.Time
	targets   map[string]DeprovisionTarget
	purgeable []DeprovisionTarget
	markErr   error
	listErr   error
}

func (f *fakeDeactivator) MarkDeactivated(_ context.Context, id string, purgeAfter time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.marked == nil {
		f.marked = map[string]time.Time{}
	}
	f.marked[id] = purgeAfter
	return nil
}

func (f *fakeDeactivator) ListPurgeable(_ context.Context, _ time.Time) ([]DeprovisionTarget, error) {
	return f.purgeable, f.listErr
}

func (f *fakeDeactivator) ResolveTarget(_ context.Context, id string) (DeprovisionTarget, error) {
	if t, ok := f.targets[id]; ok {
		return t, nil
	}
	return DeprovisionTarget{IdentityID: id}, nil
}

type recordingPort struct {
	mu   sync.Mutex
	seen []string
	err  error
}

func (r *recordingPort) record(v string) error {
	if r.err != nil {
		return r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, v)
	return nil
}

func (r *recordingPort) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

type fakeSessions struct{ recordingPort }

func (f *fakeSessions) KillSessions(_ context.Context, u string) error { return f.record(u) }

type fakeJobs struct{ recordingPort }

func (f *fakeJobs) TerminateJobs(_ context.Context, id string) error { return f.record(id) }

type fakeConvPurger struct{ recordingPort }

func (f *fakeConvPurger) PurgeConversations(_ context.Context, id string) error { return f.record(id) }

type fakeGraphPurger struct{ recordingPort }

func (f *fakeGraphPurger) PurgeGraph(_ context.Context, id string) error { return f.record(id) }

type fakeAdaptiveGraphPurger struct{ recordingPort }

func (f *fakeAdaptiveGraphPurger) PurgeAdaptiveGraph(_ context.Context, id string) error {
	return f.record(id)
}

type fakeAdaptiveIdentityFencer struct{ recordingPort }

func (f *fakeAdaptiveIdentityFencer) FenceAdaptiveIdentity(_ context.Context, id string) error {
	return f.record(id)
}

type fakeIdentityDeleter struct{ recordingPort }

func (f *fakeIdentityDeleter) DeleteIdentity(_ context.Context, name string) error {
	return f.record(name)
}

type fakeAuthulaDeleter struct{ recordingPort }

func (f *fakeAuthulaDeleter) DeleteUser(_ context.Context, u string) error { return f.record(u) }

const testIdentityID = "22222222-2222-4222-8222-222222222222"

func fullDeprovisionDeps() (DeprovisionDeps, *deprovFakes) {
	f := &deprovFakes{
		deact:    &fakeDeactivator{targets: map[string]DeprovisionTarget{}},
		sess:     &fakeSessions{},
		jobs:     &fakeJobs{},
		conv:     &fakeConvPurger{},
		graph:    &fakeGraphPurger{},
		adaptive: &fakeAdaptiveGraphPurger{},
		fence:    &fakeAdaptiveIdentityFencer{},
		os:       newFakeObjectStore(),
		fs:       newFakeFilesystem(),
		iddel:    &fakeIdentityDeleter{},
		authdel:  &fakeAuthulaDeleter{},
		journal:  newFakeJournal(),
	}
	deps := DeprovisionDeps{
		Journal: f.journal, Deactivator: f.deact, Sessions: f.sess, Jobs: f.jobs,
		Conversations: f.conv, Graph: f.graph, AdaptiveFence: f.fence, AdaptiveGraph: f.adaptive, ObjectStore: f.os, Filesystem: f.fs,
		IdentityDelete: f.iddel, AuthulaDelete: f.authdel,
	}
	return deps, f
}

type deprovFakes struct {
	deact    *fakeDeactivator
	sess     *fakeSessions
	jobs     *fakeJobs
	conv     *fakeConvPurger
	graph    *fakeGraphPurger
	adaptive *fakeAdaptiveGraphPurger
	fence    *fakeAdaptiveIdentityFencer
	os       *fakeObjectStore
	fs       *fakeFilesystem
	iddel    *fakeIdentityDeleter
	authdel  *fakeAuthulaDeleter
	journal  *fakeJournal
}

func targetFor(id string) DeprovisionTarget {
	return DeprovisionTarget{IdentityID: id, IdentityName: "user@aura.local", AuthulaUserID: "authula-" + id}
}

// TestDeprovisionDeactivateImmediate proves Deactivate stamps the grace window, kills the
// Authula sessions (blocks login), and terminates the identity's background jobs.
func TestDeprovisionDeactivateImmediate(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.targets[testIdentityID] = targetFor(testIdentityID)
	d := NewDeprovisioner(deps)

	before := time.Now().UTC()
	if err := d.Deactivate(context.Background(), testIdentityID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	pa, ok := f.deact.marked[testIdentityID]
	if !ok {
		t.Fatal("Deactivate did not stamp deactivated_at/purge_after")
	}
	if !pa.After(before.Add(defaultDeprovisionGrace - time.Minute)) {
		t.Fatalf("purge_after %v is not ~now+grace", pa)
	}
	if f.sess.count() != 1 {
		t.Fatalf("KillSessions calls = %d, want 1 (login must be blocked immediately)", f.sess.count())
	}
	if f.jobs.count() != 1 {
		t.Fatalf("TerminateJobs calls = %d, want 1 (jobs must be terminated immediately)", f.jobs.count())
	}
}

// TestDeprovisionPurgeReversesEveryLeg proves Purge tears down every plane the provisioning
// saga built, leaving no orphan.
func TestDeprovisionPurgeReversesEveryLeg(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	d := NewDeprovisioner(deps)

	if err := d.Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if f.conv.count() != 1 || f.graph.count() != 1 || f.fence.count() != 1 || f.adaptive.count() != 1 {
		t.Fatalf("data-plane purge: conversations=%d graph=%d fence=%d adaptive=%d, want 1/1/1/1",
			f.conv.count(), f.graph.count(), f.fence.count(), f.adaptive.count())
	}
	if f.os.deprovCalls[testIdentityID] != 1 || f.fs.deprovCalls[testIdentityID] != 1 {
		t.Fatalf("resource-plane purge: objectstore=%d filesystem=%d, want 1/1",
			f.os.deprovCalls[testIdentityID], f.fs.deprovCalls[testIdentityID])
	}
	if f.iddel.count() != 1 || f.authdel.count() != 1 {
		t.Fatalf("identity/authula delete: id=%d authula=%d, want 1/1", f.iddel.count(), f.authdel.count())
	}
	sid := sagaID(sagaKindDeprovision, testIdentityID)
	for _, step := range []string{sagaStepConversations, sagaStepAdaptiveFence, sagaStepAdaptiveGraph, sagaStepGraph, sagaStepObjectStore, sagaStepDirs, sagaStepIdentityRow, sagaStepAuthula} {
		if !f.journal.stepDone(sid, step) {
			t.Errorf("deprovision journal step %q not marked done", step)
		}
	}
}

// TestDeprovisionPurgeResumable proves a mid-purge failure leaves a journaled partial state
// and a re-run SKIPS the already-done steps (no double teardown) and completes the rest —
// converging to a fully-purged identity.
func TestDeprovisionPurgeResumable(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.graph.err = errors.New("injected: graph purge crash")
	d := NewDeprovisioner(deps)
	target := targetFor(testIdentityID)

	if err := d.Purge(context.Background(), target); err == nil {
		t.Fatal("want error on the graph-leg failure")
	}
	if f.conv.count() != 1 {
		t.Fatalf("conversations purge before the failure = %d, want 1", f.conv.count())
	}
	// Recover: the graph leg now succeeds. The re-run must skip conversations (journal done)
	// and complete graph + every later leg exactly once.
	f.graph.err = nil
	if err := d.Purge(context.Background(), target); err != nil {
		t.Fatalf("resume purge: %v", err)
	}
	if f.conv.count() != 1 {
		t.Fatalf("conversations re-purged on resume (count=%d) — a done step must be skipped", f.conv.count())
	}
	if f.graph.count() != 1 || f.iddel.count() != 1 || f.authdel.count() != 1 {
		t.Fatalf("resume did not converge: graph=%d idDelete=%d authula=%d, want 1/1/1",
			f.graph.count(), f.iddel.count(), f.authdel.count())
	}
}

func TestDeprovisionPurgeRequiresOwnedDataPlanesBeforeAnyStep(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DeprovisionDeps)
	}{
		{
			name: "conversation purger",
			mutate: func(deps *DeprovisionDeps) {
				deps.Conversations = nil
			},
		},
		{
			name: "memory graph purger",
			mutate: func(deps *DeprovisionDeps) {
				deps.Graph = nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, f := fullDeprovisionDeps()
			tt.mutate(&deps)

			err := NewDeprovisioner(deps).Purge(context.Background(), targetFor(testIdentityID))
			if err == nil {
				t.Fatalf("Purge without %s succeeded", tt.name)
			}
			if f.conv.count()+f.graph.count()+f.fence.count()+f.adaptive.count() != 0 {
				t.Fatal("purge executed a data-plane step before required-port preflight")
			}
			if f.iddel.count() != 0 || f.authdel.count() != 0 {
				t.Fatal("identity deletion ran without verified conversation and graph purgers")
			}
		})
	}
}

// TestPurgeExpiredScansAndPurges proves the scheduled entry lists the grace-elapsed
// identities and purges each.
func TestPurgeExpiredScansAndPurges(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.purgeable = []DeprovisionTarget{targetFor("a1111111-1111-4111-8111-111111111111"), targetFor("b2222222-2222-4222-8222-222222222222")}
	d := NewDeprovisioner(deps)

	purged, err := d.PurgeExpired(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if purged != 2 {
		t.Fatalf("purged = %d, want 2", purged)
	}
	if f.iddel.count() != 2 || f.authdel.count() != 2 {
		t.Fatalf("batch purge deletes: id=%d authula=%d, want 2/2", f.iddel.count(), f.authdel.count())
	}
}
