// serve_delegation.go wires the SWARM-03/09 background delegation claim loop into
// the daemon composition root. Split from serve.go (boot/lifecycle) to keep it off
// the 600-LOC ceiling, mirroring serve_dispatch.go's own split rationale.
package main

import (
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/swarm"
)

// runtimeDelegationWorkerIDPrefix names the delegation claim loop's per-identity
// worker id, distinct from the asset-processing worker's own prefix so a
// locked_by column value unambiguously identifies which loop holds a lease.
const runtimeDelegationWorkerIDPrefix = "aura-swarm-delegation"

// newRuntimeDelegationWorker builds the resident delegation claim loop, reusing
// runtimeTenantIngestionProcessor's identity-enumeration wrapper VERBATIM
// (asset_processing_worker.go) so the SAME "one poll, every active identity"
// shape drains the delegation queue instead of a single hardcoded identity —
// this daemon may be multi-tenant (Authula). worker is the static per-daemon
// RunConfig template (client/LLM/registry/gateway/Cfg) runChild needs; ConvID
// and Depth are overridden per claimed job from its payload
// (DelegationClaimLoop.runWithHeartbeat).
//
// steerPub is the SAME steer inbox instance the AG-UI steer route
// (internal/agui/server_run_steer.go) and the Telegram dispatch
// (internal/channels/telegram) already push operator steers into (D-04/D-05): a
// worker report and an operator steer land in one queue, delivered by the same
// Drain call. A nil pool (no Postgres configured) yields a no-op set of workers,
// matching newRuntimeAssetProcessingWorker's own degrade.
func newRuntimeDelegationWorker(chat *chatEnv, steerPub swarm.SteerPublisher) *runtimeProcessingWorkers {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return &runtimeProcessingWorkers{}
	}
	store := documents.NewPostgresIngestionJobStore(chat.pool)
	leaseDuration := time.Duration(chat.cfg.SwarmDelegationLeaseSec) * time.Second
	pollInterval := time.Duration(chat.cfg.SwarmDelegationPollSec) * time.Second
	workerTemplate := swarm.RunConfig{
		Client:         chat.client,
		LLM:            chat.cfg.LLM,
		Cfg:            *chat.cfg,
		ParentRegistry: chat.reg,
		Gateway:        chat.gateway,
	}
	delegation := &runtimeTenantIngestionProcessor{
		identities:     identity.New(chat.pool),
		width:          1,
		workerIDPrefix: runtimeDelegationWorkerIDPrefix,
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			loop := swarm.NewDelegationClaimLoop(store, steerPub, identityID, workerTemplate, leaseDuration, pollInterval)
			loop.WorkerID = workerID
			return loop
		},
	}
	return &runtimeProcessingWorkers{workers: []*runtimeIngestionWorker{
		newRuntimeIngestionWorker(delegation, pollInterval),
	}}
}
