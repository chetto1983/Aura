package agui

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"
)

// --- fakes for the Phase-36 resource legs + saga journal ---

type fakeObjectStore struct {
	mu           sync.Mutex
	provCalls    map[string]int
	deprovCalls  map[string]int
	live         map[string]bool
	provisionErr error
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{provCalls: map[string]int{}, deprovCalls: map[string]int{}, live: map[string]bool{}}
}

func (f *fakeObjectStore) ProvisionObjectStore(_ context.Context, id string) error {
	if f.provisionErr != nil {
		return f.provisionErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provCalls[id]++
	f.live[id] = true // idempotent: a re-provision of the same id is a no-op on live state
	return nil
}

func (f *fakeObjectStore) DeprovisionObjectStore(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deprovCalls[id]++
	delete(f.live, id)
	return nil
}

func (f *fakeObjectStore) liveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.live)
}

func (f *fakeObjectStore) provisionCalls(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.provCalls[id]
}

type fakeFilesystem struct {
	mu           sync.Mutex
	provCalls    map[string]int
	deprovCalls  map[string]int
	live         map[string]bool
	provisionErr error
}

func newFakeFilesystem() *fakeFilesystem {
	return &fakeFilesystem{provCalls: map[string]int{}, deprovCalls: map[string]int{}, live: map[string]bool{}}
}

func (f *fakeFilesystem) ProvisionIdentityDirs(_ context.Context, id string) error {
	if f.provisionErr != nil {
		return f.provisionErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provCalls[id]++
	f.live[id] = true
	return nil
}

func (f *fakeFilesystem) DeprovisionIdentityDirs(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deprovCalls[id]++
	delete(f.live, id)
	return nil
}

func (f *fakeFilesystem) liveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.live)
}

// fakeJournal is a durable-across-runs in-memory saga journal so a resume can observe the
// steps a prior run marked done (forward recovery).
type fakeJournal struct {
	mu      sync.Mutex
	records int
	done    map[string]map[string]bool // sagaID -> step -> done
}

func newFakeJournal() *fakeJournal { return &fakeJournal{done: map[string]map[string]bool{}} }

func (j *fakeJournal) Record(_ context.Context, sagaID, _, _, step, status string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.records++
	if status == sagaDone {
		if j.done[sagaID] == nil {
			j.done[sagaID] = map[string]bool{}
		}
		j.done[sagaID][step] = true
	}
	return nil
}

func (j *fakeJournal) CompletedSteps(_ context.Context, sagaID string) (map[string]bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]bool{}
	maps.Copy(out, j.done[sagaID])
	return out, nil
}

func (j *fakeJournal) stepDone(sagaID, step string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done[sagaID][step]
}

// resourceService wires a saga service with the resource legs + journal attached.
func resourceService(t *testing.T, os ObjectStoreProvisioner, fs FilesystemProvisioner, j SagaJournal) (*onboardingService, string) {
	t.Helper()
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create", "agent.run"})
	svc.objectStore = os
	svc.filesystem = fs
	svc.journal = j
	return svc, tok
}

// TestProvisionResourceLegsWiredAndJournaled proves the happy path provisions the Garage
// bucket + filesystem roots exactly once each and journals every post-identity step done.
func TestProvisionResourceLegsWiredAndJournaled(t *testing.T) {
	os, fs, j := newFakeObjectStore(), newFakeFilesystem(), newFakeJournal()
	svc, tok := resourceService(t, os, fs, j)

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"agent.run"}))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if os.liveCount() != 1 || fs.liveCount() != 1 {
		t.Fatalf("resource legs not provisioned once: objectstore=%d filesystem=%d", os.liveCount(), fs.liveCount())
	}
	sid := sagaID(sagaKindProvision, resp.IdentityID)
	for _, step := range []string{sagaStepIdentity, sagaStepRecovery, sagaStepGarage, sagaStepFilesystem, sagaStepTelegram, sagaStepAudit} {
		if !j.stepDone(sid, step) {
			t.Errorf("journal step %q not marked done", step)
		}
	}
}

// TestProvisionResourceLegCompensation proves a filesystem-leg failure deprovisions the
// object store provisioned earlier in the same call and rolls back the identity + Authula
// user — no orphan across any store.
func TestProvisionResourceLegCompensation(t *testing.T) {
	os := newFakeObjectStore()
	fs := &fakeFilesystem{provCalls: map[string]int{}, deprovCalls: map[string]int{}, live: map[string]bool{}, provisionErr: errors.New("injected: filesystem")}
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.objectStore, svc.filesystem, svc.journal = os, fs, newFakeJournal()

	if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
		t.Fatal("want error on filesystem-leg failure")
	}
	if os.liveCount() != 0 {
		t.Fatalf("object store leaked after fs-leg failure: live=%d, want 0", os.liveCount())
	}
	if au.liveAuthulaUsers() != 0 || leg.liveIdentities() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
		t.Fatalf("fs-leg failure left orphans: authula=%d identities=%d tokens=%d audit=%d",
			au.liveAuthulaUsers(), leg.liveIdentities(), tg.mintedCount(), leg.auditCount())
	}
}

// TestProvisionAuditFailCompensatesResources proves a LATER leg (audit) failure reverses
// the already-provisioned Garage + filesystem resources.
func TestProvisionAuditFailCompensatesResources(t *testing.T) {
	os, fs := newFakeObjectStore(), newFakeFilesystem()
	au, tg := &fakeAuthula{}, &fakeTelegram{}
	leg := &fakeAuraLeg{auditErr: errors.New("injected: audit")}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.objectStore, svc.filesystem, svc.journal = os, fs, newFakeJournal()

	if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
		t.Fatal("want error on audit failure")
	}
	if os.liveCount() != 0 || fs.liveCount() != 0 {
		t.Fatalf("audit-fail did not reverse resources: objectstore=%d filesystem=%d, want 0/0", os.liveCount(), fs.liveCount())
	}
}

// TestResumeResourceProvisioningConverges proves the forward-recovery contract: a crash
// after the Garage leg (journaled done) but before the filesystem leg is recovered by a
// re-run that SKIPS the already-done Garage step and completes the filesystem — converging
// to ONE consistent set of resources with no duplicate bucket.
func TestResumeResourceProvisioningConverges(t *testing.T) {
	os := newFakeObjectStore()
	fs := &fakeFilesystem{provCalls: map[string]int{}, deprovCalls: map[string]int{}, live: map[string]bool{}, provisionErr: errors.New("injected: filesystem crash")}
	j := newFakeJournal()
	svc, _ := resourceService(t, os, fs, j)
	const id = "11111111-1111-4111-8111-111111111111"

	// First run "crashes" at the filesystem leg: Garage is journaled done, no compensation.
	if err := svc.resumeResourceProvisioning(context.Background(), id); err == nil {
		t.Fatal("want error on the first (crashing) resume")
	}
	if os.provisionCalls(id) != 1 || os.liveCount() != 1 {
		t.Fatalf("after crash: garage provisionCalls=%d live=%d, want 1/1", os.provisionCalls(id), os.liveCount())
	}

	// Recover: the filesystem leg now succeeds. The re-run must SKIP Garage (journal done)
	// and complete the filesystem — converging to exactly one of each.
	fs.provisionErr = nil
	if err := svc.resumeResourceProvisioning(context.Background(), id); err != nil {
		t.Fatalf("recovery resume: %v", err)
	}
	if os.provisionCalls(id) != 1 {
		t.Fatalf("Garage re-provisioned on resume (provisionCalls=%d) — forward recovery must skip a done step", os.provisionCalls(id))
	}
	if os.liveCount() != 1 || fs.liveCount() != 1 {
		t.Fatalf("resume did not converge: objectstore=%d filesystem=%d, want 1/1", os.liveCount(), fs.liveCount())
	}
}
