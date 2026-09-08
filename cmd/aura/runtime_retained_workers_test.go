package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

type occupiedRuntimeProcessor struct{ occupied bool }

func (p *occupiedRuntimeProcessor) ProcessOnce(context.Context) (int, error) {
	if p.occupied {
		return 0, nil
	}
	p.occupied = true
	return 1, nil
}
func (p *occupiedRuntimeProcessor) Idle() bool { return !p.occupied }

func TestRetainedTenantWorkerPreservesAdmissionAcrossPolls(t *testing.T) {
	created := 0
	p := &runtimeTenantIngestionProcessor{identities: fakeRuntimeIdentityLister{identities: []identity.Identity{{ID: "owner"}}}, retainWorkers: true, worker: func(string, string) runtimeIngestionProcessor { created++; return &occupiedRuntimeProcessor{} }}
	if n, err := p.ProcessOnce(t.Context()); err != nil || n != 1 {
		t.Fatal("first work not admitted")
	}
	if n, err := p.ProcessOnce(t.Context()); err != nil || n != 0 {
		t.Fatal("new poll discarded occupied worker state")
	}
	if created != 1 {
		t.Fatalf("factory called %d times", created)
	}
}

func TestRetainedWorkersPruneOnlyRetiredIdleIdentities(t *testing.T) {
	p := &runtimeTenantIngestionProcessor{retainWorkers: true, worker: func(string, string) runtimeIngestionProcessor { return &occupiedRuntimeProcessor{} }}
	idle := p.processorFor("idle", "slot-1")
	busy := p.processorFor("busy", "slot-1").(*occupiedRuntimeProcessor)
	busy.occupied = true
	active := p.processorFor("active", "slot-1")
	p.pruneRetainedWorkers([]identity.Identity{{ID: "active"}})
	if p.processorFor("idle", "slot-1") == idle {
		t.Fatal("retired idle worker retained")
	}
	if p.processorFor("busy", "slot-1") != busy {
		t.Fatal("live work discarded")
	}
	if p.processorFor("active", "slot-1") != active {
		t.Fatal("active identity discarded")
	}
	busy.occupied = false
	p.pruneRetainedWorkers(nil)
	if len(p.retained) != 0 {
		t.Fatal("idle retired processors leaked")
	}
}
