package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// ErrOverflowMidStream rejects activation while a model stream is in flight.
var ErrOverflowMidStream = errors.New("overflow compaction requires a model boundary")

// OverflowExecutor performs exactly one compaction attempt and one reconstruction.
type OverflowExecutor struct {
	Timeout     time.Duration
	Compact     func(context.Context) error
	Reconstruct func(context.Context) ([]llm.Message, error)
}

// Run is non-destructive: every failure returns an independent copy of original.
func (e OverflowExecutor) Run(ctx context.Context, midStream bool, original []llm.Message) ([]llm.Message, error) {
	fallback := append([]llm.Message(nil), original...)
	if midStream {
		return fallback, ErrOverflowMidStream
	}
	if e.Compact == nil {
		return fallback, fmt.Errorf("overflow compaction unavailable")
	}
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}
	if err := e.Compact(ctx); err != nil {
		return fallback, err
	}
	if e.Reconstruct == nil {
		return fallback, fmt.Errorf("overflow reconstruction unavailable")
	}
	projection, err := e.Reconstruct(ctx)
	if err != nil {
		return fallback, err
	}
	return projection, nil
}

// CompactTrigger is the stable trigger vocabulary shared by every coordinator caller.
type CompactTrigger string

const (
	// CompactTriggerManual is an authenticated operator request.
	CompactTriggerManual CompactTrigger = "manual"
	// CompactTriggerProactive is a forecast-driven request.
	CompactTriggerProactive CompactTrigger = "proactive"
	// CompactTriggerBoundary runs at an explicit conversation boundary.
	CompactTriggerBoundary CompactTrigger = "boundary"
	// CompactTriggerIdle runs only after the conversation becomes idle.
	CompactTriggerIdle CompactTrigger = "idle"
	// CompactTriggerModelSafePoint is emitted by a model lifecycle safe point.
	CompactTriggerModelSafePoint CompactTrigger = "model_safe_point"
	// CompactTriggerOverflow is an emergency bounded overflow request.
	CompactTriggerOverflow CompactTrigger = "overflow"
)

// Validate rejects trigger label expansion at the coordinator boundary.
func (t CompactTrigger) Validate() error {
	switch t {
	case CompactTriggerManual, CompactTriggerProactive, CompactTriggerBoundary, CompactTriggerIdle, CompactTriggerModelSafePoint, CompactTriggerOverflow:
		return nil
	default:
		return fmt.Errorf("unsupported compaction trigger")
	}
}

// CompactStatus is the sanitized bounded operation outcome vocabulary.
type CompactStatus string

const (
	// CompactStatusDisabled means semantic activation remains shadow-only.
	CompactStatusDisabled CompactStatus = "disabled"
	// CompactStatusUnsupported means no coordinator backend is available.
	CompactStatusUnsupported CompactStatus = "unsupported"
	// CompactStatusRejected means request validation or safe-point checks failed.
	CompactStatusRejected CompactStatus = "rejected"
	// CompactStatusTimeout means the bounded operation deadline elapsed.
	CompactStatusTimeout CompactStatus = "timeout"
	// CompactStatusCompacting means a future enabled coordinator accepted work.
	CompactStatusCompacting CompactStatus = "compacting"
	// CompactStatusRecovered means restore committed its prior pointer.
	CompactStatusRecovered CompactStatus = "recovered"
	// CompactStatusUnavailable hides backend failure details.
	CompactStatusUnavailable CompactStatus = "unavailable"
)

// CompactRequest carries one durable operation identity through preview or restore.
type CompactRequest struct {
	OperationID, ConversationID, BranchID, CheckpointID, ActorID string
	Trigger                                                      CompactTrigger
	SafePoint                                                    bool
}

// CompactPreview is sanitized metadata; summary content is already validated historical data.
type CompactPreview struct {
	OperationID       string        `json:"operationId"`
	CheckpointID      string        `json:"checkpointId,omitempty"`
	PriorCheckpointID string        `json:"priorCheckpointId,omitempty"`
	Summary           string        `json:"summary,omitempty"`
	Status            CompactStatus `json:"status"`
	Activated         bool          `json:"activated"`
}

// CompactBackend owns durable preview and restore persistence.
type CompactBackend interface {
	PreviewCompaction(context.Context, CompactRequest) (CompactPreview, error)
	RestoreCompaction(context.Context, CompactRequest) error
}

// CompactCoordinator is the one bounded entry point for all compaction triggers.
type CompactCoordinator struct {
	backend           CompactBackend
	activationEnabled bool
}

// NewCompactCoordinator constructs a shadow-only coordinator unless explicitly enabled later.
func NewCompactCoordinator(backend CompactBackend, activationEnabled bool) *CompactCoordinator {
	return &CompactCoordinator{backend: backend, activationEnabled: activationEnabled}
}

// Preview returns a validated checkpoint projection without changing the active pointer.
func (c *CompactCoordinator) Preview(ctx context.Context, req CompactRequest) (CompactPreview, error) {
	if c == nil || c.backend == nil {
		return CompactPreview{OperationID: req.OperationID, Status: CompactStatusUnsupported}, nil
	}
	if req.OperationID == "" || req.Trigger.Validate() != nil {
		return CompactPreview{OperationID: req.OperationID, Status: CompactStatusRejected}, nil
	}
	preview, err := c.backend.PreviewCompaction(ctx, req)
	preview.OperationID, preview.Activated = req.OperationID, false
	if err != nil {
		preview.Status = compactErrorStatus(err)
		return preview, nil
	}
	if c.activationEnabled {
		preview.Status = CompactStatusCompacting
	} else {
		preview.Status = CompactStatusDisabled
	}
	return preview, nil
}

// Restore rolls back to a prior pointer only at an explicit model-safe point.
func (c *CompactCoordinator) Restore(ctx context.Context, req CompactRequest) (CompactPreview, error) {
	result := CompactPreview{OperationID: req.OperationID, CheckpointID: req.CheckpointID}
	if c == nil || c.backend == nil {
		result.Status = CompactStatusUnsupported
		return result, nil
	}
	if req.OperationID == "" || req.CheckpointID == "" || !req.SafePoint || req.Trigger.Validate() != nil {
		result.Status = CompactStatusRejected
		return result, nil
	}
	if err := c.backend.RestoreCompaction(ctx, req); err != nil {
		result.Status = compactErrorStatus(err)
		return result, nil
	}
	result.Status, result.Activated = CompactStatusRecovered, false
	return result, nil
}

func compactErrorStatus(err error) CompactStatus {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return CompactStatusTimeout
	}
	return CompactStatusUnavailable
}
