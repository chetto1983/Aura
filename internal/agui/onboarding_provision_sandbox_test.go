package agui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// onboarding_provision_sandbox_test.go covers the sandbox leg's compensation symmetry
// (Task 2, D-09): a later-leg failure destroys the box FIRST and the rest in reverse
// order, compensation survives a cancelled parent context and a double invocation, an
// unwired sandbox port still provisions the other three resources, and the sandbox leg's
// OWN failure destroys nothing else (mirroring memory/objectStore, not filesystem's full
// compResources() call) — the caller compensates the earlier legs via the returned
// closure.

// TestProvisionSandboxOwnFailureDestroysNothingElse proves a sandbox-leg failure
// self-destroys only (an inline DestroySandbox on its own failure), returns a
// provisionFail-shaped error, and does NOT itself reverse the earlier legs — the CALLER
// invokes the returned compResources to do that (Task 2 behavior 1).
func TestProvisionSandboxOwnFailureDestroysNothingElse(t *testing.T) {
	memory, os, fs := newFakeMemoryProvisioner(), newFakeObjectStore(), newFakeFilesystem()
	sandbox := newFakeSandboxProvisioner()
	sandbox.provisionErr = errors.New("injected: sandbox create")
	svc, _ := sagaService(t, &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}, []string{"identity.create"})
	svc.memory, svc.objectStore, svc.filesystem, svc.sandbox, svc.journal = memory, os, fs, sandbox, newFakeJournal()

	const id = "sandbox-own-fail-1"
	run := newSagaRun(context.Background(), svc.journal, sagaKindProvision, id)
	compResources, err := svc.provisionResourceLegs(context.Background(), run, id)
	if err == nil {
		t.Fatal("provisionResourceLegs err = nil, want a sandbox-provision error")
	}
	if !strings.Contains(err.Error(), "sandbox provision") {
		t.Fatalf("err = %v, want it shaped by provisionFail(\"sandbox provision\", ...)", err)
	}
	// Self-destroy only: the sandbox leg's own compensation ran once...
	if got := sandbox.destroyCalls(id); got != 1 {
		t.Fatalf("sandbox destroy calls = %d, want 1 (self-destroy on own failure)", got)
	}
	// ...but memory/objectStore/filesystem (provisioned successfully before sandbox) are
	// STILL LIVE — the leg did not reverse them itself.
	if memory.liveCount() != 1 || os.liveCount() != 1 || fs.liveCount() != 1 {
		t.Fatalf("earlier legs were touched by the sandbox leg's own failure: memory=%d objectstore=%d filesystem=%d, want 1/1/1",
			memory.liveCount(), os.liveCount(), fs.liveCount())
	}
	// The caller compensates the earlier legs via the returned closure.
	compResources()
	if memory.liveCount() != 0 || os.liveCount() != 0 || fs.liveCount() != 0 {
		t.Fatalf("compResources() did not reverse the earlier legs: memory=%d objectstore=%d filesystem=%d, want 0/0/0",
			memory.liveCount(), os.liveCount(), fs.liveCount())
	}
}

// TestProvisionSandboxLaterLegFailureReversesInStrictOrder proves a LATER leg (Telegram/
// audit) failure invokes compResources, and the recorded order is sandbox, filesystem,
// objectStore, memory — the exact reverse of provisioning order (Task 2 behavior 2).
func TestProvisionSandboxLaterLegFailureReversesInStrictOrder(t *testing.T) {
	log := &orderLog{}
	au, tg := &fakeAuthula{}, &fakeTelegram{}
	leg := &fakeAuraLeg{auditErr: errors.New("injected: audit")}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.memory = orderedMemory{log: log}
	svc.objectStore = orderedObjectStore{log: log}
	svc.filesystem = orderedFS{log: log}
	svc.sandbox = orderedSandboxProvisioner{log: log}
	svc.journal = newFakeJournal()

	if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
		t.Fatal("want error on audit failure")
	}
	if got := strings.Join(log.order(), ","); got != "sandbox,dirs,objectstore,memory" {
		t.Fatalf("compensation order = %q, want %q (sandbox, filesystem, objectStore, memory)",
			got, "sandbox,dirs,objectstore,memory")
	}
}

// TestProvisionSandboxCompensationIsIdempotent proves invoking compResources twice
// destroys the box twice without error — a re-run after a crash must converge (Task 2
// behavior 3).
func TestProvisionSandboxCompensationIsIdempotent(t *testing.T) {
	sandbox := newFakeSandboxProvisioner()
	svc, _ := sagaService(t, &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}, []string{"identity.create"})
	svc.sandbox, svc.journal = sandbox, newFakeJournal()

	const id = "sandbox-idempotent-1"
	run := newSagaRun(context.Background(), svc.journal, sagaKindProvision, id)
	compResources, err := svc.provisionResourceLegs(context.Background(), run, id)
	if err != nil {
		t.Fatalf("provisionResourceLegs: %v", err)
	}
	compResources()
	compResources()
	if got := sandbox.destroyCalls(id); got != 2 {
		t.Fatalf("destroy calls = %d, want 2 (idempotent double invocation)", got)
	}
}

// TestProvisionSandboxCompensationSurvivesCancelledContext proves DestroySandbox still
// receives a LIVE context even when the caller's ctx is already cancelled before
// compResources runs (context.WithoutCancel, Task 2 behavior 4).
func TestProvisionSandboxCompensationSurvivesCancelledContext(t *testing.T) {
	sandbox := newFakeSandboxProvisioner()
	sandbox.provisionErr = errors.New("injected: sandbox create")
	svc, _ := sagaService(t, &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}, []string{"identity.create"})
	svc.sandbox, svc.journal = sandbox, newFakeJournal()

	ctx, cancel := context.WithCancel(context.Background())
	const id = "sandbox-cancel-ctx-1"
	run := newSagaRun(ctx, svc.journal, sagaKindProvision, id)
	cancel() // cancel BEFORE the leg (and its self-compensation) runs
	if _, err := svc.provisionResourceLegs(ctx, run, id); err == nil {
		t.Fatal("want error on sandbox-provision failure")
	}
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.destroyCtxLive) == 0 || !sandbox.destroyCtxLive[0] {
		t.Fatalf("DestroySandbox received a cancelled context; want context.WithoutCancel, got liveness=%v", sandbox.destroyCtxLive)
	}
}

// TestProvisionSandboxNilPortSkipsLegAndCompensation proves a nil Sandbox port skips the
// leg and its compensation entirely — an unwired deployment still provisions the other
// three resources (Task 2 behavior 5, backward compatibility with pre-cutover
// compositions).
func TestProvisionSandboxNilPortSkipsLegAndCompensation(t *testing.T) {
	memory, os, fs := newFakeMemoryProvisioner(), newFakeObjectStore(), newFakeFilesystem()
	svc, _ := sagaService(t, &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}, []string{"identity.create"})
	svc.memory, svc.objectStore, svc.filesystem, svc.sandbox, svc.journal = memory, os, fs, nil, newFakeJournal()

	const id = "sandbox-nil-port-1"
	run := newSagaRun(context.Background(), svc.journal, sagaKindProvision, id)
	compResources, err := svc.provisionResourceLegs(context.Background(), run, id)
	if err != nil {
		t.Fatalf("provisionResourceLegs: %v", err)
	}
	if memory.liveCount() != 1 || os.liveCount() != 1 || fs.liveCount() != 1 {
		t.Fatalf("nil sandbox port blocked the other legs: memory=%d objectstore=%d filesystem=%d, want 1/1/1",
			memory.liveCount(), os.liveCount(), fs.liveCount())
	}
	// Compensation must not panic on a nil sandbox port.
	compResources()
	if memory.liveCount() != 0 || os.liveCount() != 0 || fs.liveCount() != 0 {
		t.Fatalf("compResources() with a nil sandbox port did not reverse the rest: memory=%d objectstore=%d filesystem=%d, want 0/0/0",
			memory.liveCount(), os.liveCount(), fs.liveCount())
	}
}

// TestProvisionSandboxHappyPathIsLastAndJournaled proves the sandbox leg runs LAST (after
// filesystem) on the happy path and is journaled via the existing sagaStepSandbox
// constant.
func TestProvisionSandboxHappyPathIsLastAndJournaled(t *testing.T) {
	memory, os, fs := newFakeMemoryProvisioner(), newFakeObjectStore(), newFakeFilesystem()
	sandbox := newFakeSandboxProvisioner()
	j := newFakeJournal()
	svc, tok := sagaService(t, &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}, []string{"identity.create", "agent.run"})
	svc.memory, svc.objectStore, svc.filesystem, svc.sandbox, svc.journal = memory, os, fs, sandbox, j

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"agent.run"}))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if sandbox.liveCount() != 1 {
		t.Fatalf("sandbox live count = %d, want 1", sandbox.liveCount())
	}
	sid := sagaID(sagaKindProvision, resp.IdentityID)
	if !j.stepDone(sid, sagaStepSandbox) {
		t.Fatal("sagaStepSandbox not marked done in the journal")
	}
}

// --- orderLog-compatible fakes extending deprovision_sandbox_test.go's shapes ---

type orderedObjectStore struct{ log *orderLog }

func (o orderedObjectStore) ProvisionObjectStore(context.Context, string) error { return nil }
func (o orderedObjectStore) DeprovisionObjectStore(context.Context, string) error {
	return o.log.add("objectstore")
}

// orderedMemory (memory-only PurgeMemory) already exists in deprovision_sandbox_test.go
// but does not satisfy MemoryProvisioner's ProvisionMemory; wrap it here so this file's
// tests can use the SAME orderLog-backed type across all four legs.
func (m orderedMemory) ProvisionMemory(context.Context, string) error { return nil }

// orderedSandboxProvisioner adds ProvisionSandbox to deprovision_sandbox_test.go's
// orderedSandbox so it satisfies SandboxProvisioner (embeds SandboxPurger) for the
// provisioning-order test above, reusing the SAME orderLog-backed DestroySandbox rather
// than declaring a parallel sandbox double.
type orderedSandboxProvisioner struct{ orderedSandbox }

func (s orderedSandboxProvisioner) ProvisionSandbox(context.Context, string) error { return nil }
