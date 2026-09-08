package main

import "github.com/chetto1983/aura/internal/identity"

type runtimeWorkerKey struct{ identityID, workerID string }

func (p *runtimeTenantIngestionProcessor) processorFor(identityID, workerID string) runtimeIngestionProcessor {
	if !p.retainWorkers {
		return p.worker(identityID, workerID)
	}
	p.workerMu.Lock()
	defer p.workerMu.Unlock()
	if p.retained == nil {
		p.retained = make(map[runtimeWorkerKey]runtimeIngestionProcessor)
	}
	key := runtimeWorkerKey{identityID, workerID}
	if existing := p.retained[key]; existing != nil {
		return existing
	}
	worker := p.worker(identityID, workerID)
	p.retained[key] = worker
	return worker
}

func (p *runtimeTenantIngestionProcessor) pruneRetainedWorkers(active []identity.Identity) {
	if !p.retainWorkers {
		return
	}
	live := make(map[string]bool, len(active))
	for _, item := range active {
		live[item.ID] = true
	}
	p.workerMu.Lock()
	defer p.workerMu.Unlock()
	for key, worker := range p.retained {
		if live[key.identityID] {
			continue
		}
		if state, ok := worker.(interface{ Idle() bool }); ok && !state.Idle() {
			continue
		}
		delete(p.retained, key)
	}
}
