package main

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

const adaptiveBenchmarkControlTimeout = 2 * time.Minute

type adaptiveBenchmarkControlExecution struct {
	EvidenceIDs                []uuid.UUID
	ObservedFailureReasonCodes []string
	Concurrency                *auraeval.AdaptiveBenchmarkConcurrencyEvidence
}

type adaptiveBenchmarkControlExecutor func(
	context.Context,
	auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error)

type adaptiveBenchmarkControlDriverOps struct {
	Now       func() time.Time
	Timeout   time.Duration
	Executors map[string]adaptiveBenchmarkControlExecutor
}

type adaptiveBenchmarkControlDriver struct {
	runID uuid.UUID
	ops   adaptiveBenchmarkControlDriverOps
	mu    sync.Mutex
}

type adaptiveBenchmarkControlRuntime struct {
	environment *adaptiveBenchmarkChatEnvironment
	request     adaptiveBenchmarkRunRequest
	runID       uuid.UUID
	ownerB      uuid.UUID

	concurrencySnapshot *adaptiveBenchmarkConcurrencySnapshot
}

func newAdaptiveBenchmarkControlDriver(
	environment *adaptiveBenchmarkChatEnvironment,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
) (auraeval.AdaptiveBenchmarkControlDriver, error) {
	if environment == nil ||
		environment.env == nil ||
		environment.env.run == nil ||
		environment.env.cfg == nil ||
		environment.env.identity == nil ||
		environment.bundle == nil ||
		environment.recorder == nil ||
		environment.store == nil ||
		request.OwnerID == uuid.Nil ||
		runID == uuid.Nil ||
		environment.bundle.runID != runID ||
		environment.bundle.PrimaryOwnerID() != request.OwnerID {
		return nil, errors.New(
			"adaptive benchmark control runtime is unavailable",
		)
	}
	ownerB := uuid.MustParse(identityctx.CLIServiceIdentity)
	if ownerB == request.OwnerID {
		return nil, errors.New(
			"adaptive benchmark control owners are not isolated",
		)
	}
	identityCtx, cancel := context.WithTimeout(
		context.Background(),
		adaptiveBenchmarkControlTimeout,
	)
	defer cancel()
	if _, err := environment.env.identity.GetIdentityByID(
		identityCtx,
		ownerB.String(),
	); err != nil {
		return nil, errors.New(
			"adaptive benchmark CLI service identity is unavailable",
		)
	}
	capabilities, err := environment.env.identity.ListCapabilities(
		identityCtx,
		ownerB.String(),
	)
	if err != nil || len(capabilities) != 0 {
		return nil, errors.New(
			"adaptive benchmark CLI service identity has user grants",
		)
	}
	runtime := &adaptiveBenchmarkControlRuntime{
		environment: environment,
		request:     request,
		runID:       runID,
		ownerB:      ownerB,
	}
	return newAdaptiveBenchmarkControlDriverWithOps(
		runID,
		adaptiveBenchmarkControlDriverOps{
			Now:       time.Now,
			Timeout:   adaptiveBenchmarkControlTimeout,
			Executors: runtime.executors(),
		},
	)
}

func newAdaptiveBenchmarkControlDriverWithOps(
	runID uuid.UUID,
	ops adaptiveBenchmarkControlDriverOps,
) (*adaptiveBenchmarkControlDriver, error) {
	if runID == uuid.Nil ||
		ops.Now == nil ||
		ops.Timeout <= 0 ||
		len(ops.Executors) != len(auraeval.AdaptiveBenchmarkControlMatrix()) {
		return nil, errors.New(
			"adaptive benchmark control driver dependencies are invalid",
		)
	}
	for _, controlID := range auraeval.AdaptiveBenchmarkControlMatrix() {
		if ops.Executors[controlID] == nil {
			return nil, errors.New(
				"adaptive benchmark control executor is unavailable",
			)
		}
	}
	return &adaptiveBenchmarkControlDriver{runID: runID, ops: ops}, nil
}

func (driver *adaptiveBenchmarkControlDriver) RunControl(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (auraeval.AdaptiveBenchmarkControlResult, error) {
	if driver == nil {
		return auraeval.AdaptiveBenchmarkControlResult{}, errors.New(
			"adaptive benchmark control driver is unavailable",
		)
	}
	execute, ok := driver.ops.Executors[control.ControlID]
	if !ok || execute == nil {
		return auraeval.AdaptiveBenchmarkControlResult{}, errors.New(
			"adaptive benchmark control is unregistered",
		)
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	started := driver.ops.Now().Round(0).UTC()
	controlCtx, cancel := context.WithTimeout(ctx, driver.ops.Timeout)
	execution, err := execute(controlCtx, control)
	cancel()
	completed := driver.ops.Now().Round(0).UTC()
	if completed.Before(started) {
		completed = started
	}
	result := auraeval.AdaptiveBenchmarkControlResult{
		Status:      auraeval.AdaptiveBenchmarkStatusPass,
		StartedAt:   started.Format(time.RFC3339Nano),
		CompletedAt: completed.Format(time.RFC3339Nano),
		EvidenceIDs: adaptiveBenchmarkControlEvidenceStrings(
			execution.EvidenceIDs,
			driver.runID,
		),
		ReasonCodes: []string{},
		ObservedFailureReasonCodes: slices.Clone(
			execution.ObservedFailureReasonCodes,
		),
		Concurrency: execution.Concurrency,
	}
	if err != nil {
		result.Status = auraeval.AdaptiveBenchmarkStatusInvalid
		result.ReasonCodes = []string{
			auraeval.AdaptiveBenchmarkReasonControlFailed,
		}
		return result, err
	}
	return result, nil
}

func adaptiveBenchmarkControlEvidenceStrings(
	ids []uuid.UUID,
	fallback uuid.UUID,
) []string {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			unique[id] = struct{}{}
		}
	}
	if len(unique) == 0 && fallback != uuid.Nil {
		unique[fallback] = struct{}{}
	}
	values := make([]string, 0, len(unique))
	for id := range unique {
		values = append(values, id.String())
	}
	slices.Sort(values)
	return values
}

var _ auraeval.AdaptiveBenchmarkControlDriver = (*adaptiveBenchmarkControlDriver)(nil)

func adaptiveBenchmarkExpectedControlFaults(controlID string) []string {
	switch controlID {
	case "assignment_write_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonAssignmentMissing}
	case "delivery_write_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonDeliveryMissing}
	case "outcome_write_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonOutcomeMissing}
	case "model_transport_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonModelTransportFailure}
	case "memory_epoch_mismatch":
		return []string{auraeval.AdaptiveBenchmarkReasonMemoryEpochMismatch}
	case "settings_apply_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonSettingsApplyFailure}
	case "settings_restore_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonSettingsRestoreFailure}
	case "stale_override_recovery":
		return []string{auraeval.AdaptiveBenchmarkReasonStaleOverrideRecovery}
	default:
		return []string{}
	}
}
