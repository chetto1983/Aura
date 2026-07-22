package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/obs"
)

const (
	// ArtifactPending means external removal has not been durably observed.
	ArtifactPending = "pending"
	// ArtifactRemoved means the adapter removed the external bytes.
	ArtifactRemoved = "removed"
	// ArtifactAbsent means the external bytes were already absent.
	ArtifactAbsent = "absent"
	// ArtifactFailed means the adapter returned a classified retryable failure.
	ArtifactFailed = "failed"
	// defaultPlanValidity bounds the approval window for a dry-run token. Crash
	// resume is not subject to this window after durable authorization.
	defaultPlanValidity = 15 * time.Minute
)

// ErrVersionConflict means the backing artifact changed after revalidation and
// must survive until a fresh plan authorizes its current version.
var ErrVersionConflict = errors.New("retention candidate version changed")

var (
	retentionPlanBoundary = obs.NewGlobalBoundary("aura/retention", obs.BoundaryConfig{
		Operation: "retention_plan", Count: obs.RetentionOperationsID,
	})
	retentionApplyBoundary = obs.NewGlobalBoundary("aura/retention", obs.BoundaryConfig{
		Operation: "retention_apply", Count: obs.RetentionOperationsID, Duration: obs.RetentionDurationID,
	})
)

// CandidateSource discovers policy-eligible candidates without mutating storage.
type CandidateSource interface {
	Candidates(context.Context, Policy, time.Time) ([]Candidate, error)
}

// Revalidation is the trusted state observed immediately before removal.
type Revalidation struct {
	Exists   bool
	Owned    bool
	Version  int64
	Activity ActivityEvidence
}

// CandidateRevalidator refreshes ownership, version, and active-work evidence.
type CandidateRevalidator interface {
	Revalidate(context.Context, Candidate) (Revalidation, error)
}

// RemovalResult records an idempotent adapter-owned external operation.
type RemovalResult struct {
	Bytes  int64
	Absent bool
}

// ArtifactRemover deletes only adapter-reconstructed trusted resources.
type ArtifactRemover interface {
	Remove(context.Context, Candidate) (RemovalResult, error)
}

// MetadataFinalizer removes or expires metadata after the external result is durable.
type MetadataFinalizer interface {
	Finalize(context.Context, Candidate) error
}

// ApplyReport is a bounded content-free cleanup audit summary.
type ApplyReport struct {
	PolicyVersion  string         `json:"policy_version"`
	Completed      int            `json:"completed"`
	Bytes          int64          `json:"bytes"`
	Retryable      int            `json:"retryable"`
	Failed         int            `json:"failed"`
	FailureClasses map[string]int `json:"failure_classes"`
}

// Engine runs one shared plan/apply implementation for CLI and scheduler owners.
type Engine struct {
	Policy            Policy
	Source            CandidateSource
	Store             OperationStore
	Revalidator       CandidateRevalidator
	Remover           ArtifactRemover
	Finalizer         MetadataFinalizer
	Audit             AuditSink
	WorkerID          string
	ActivityFreshness time.Duration
	PlanValidity      time.Duration
	Now               func() time.Time
	metrics           retentionMetrics
}

// Plan builds and durably records a dry-run snapshot without invoking effects.
func (e *Engine) Plan(ctx context.Context) (plan Plan, err error) {
	ctx, end := retentionPlanBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	if err = e.validate(); err != nil {
		return Plan{}, err
	}
	if err = ctx.Err(); err != nil {
		return Plan{}, err
	}
	now := e.clock()
	candidates, err := e.Source.Candidates(ctx, e.Policy, now)
	if err != nil {
		return Plan{}, fmt.Errorf("retention candidates: %w", err)
	}
	plan, err = BuildPlan(e.Policy.Version, candidates, e.Policy.BatchSize)
	if err != nil {
		return Plan{}, err
	}
	_, created, err := e.Store.SavePlan(ctx, plan, now)
	if err != nil {
		return Plan{}, err
	}
	if created {
		e.metricSink().recordItems(ctx, "pending", "accepted", int64(len(plan.Candidates)))
	}
	e.audit(ctx, AuditSummary{Mode: "plan", PolicyVersion: e.Policy.Version, Planned: len(plan.Candidates), PlannedBytes: plan.TotalBytes})
	return plan, nil
}

// Apply resolves the immutable persisted snapshot and executes its bounded items.
func (e *Engine) Apply(ctx context.Context, token string) (report ApplyReport, err error) {
	ctx, end := retentionApplyBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	if err = e.validate(); err != nil {
		return report, err
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if token == "" {
		return report, ErrTokenMismatch
	}
	now := e.clock()
	operation, err := e.Store.GetByToken(ctx, token)
	if err != nil {
		return report, err
	}
	operation, err = e.authorizeFirstApply(ctx, token, operation, now)
	if err != nil {
		return report, err
	}
	report = ApplyReport{PolicyVersion: operation.PolicyVersion, FailureClasses: map[string]int{}}
	deadline := now.Add(e.Policy.MaxRunDuration)
	for {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if !e.clock().Before(deadline) {
			return report, context.DeadlineExceeded
		}
		items, claimErr := e.Store.Claim(ctx, operation.ID, e.WorkerID, e.Policy.BatchSize, e.clock(), e.Policy.LeaseDuration)
		if claimErr != nil {
			return report, claimErr
		}
		if len(items) == 0 {
			break
		}
		retrySeen := false
		for _, item := range items {
			retry, processErr := e.processItem(ctx, item, &report)
			if processErr != nil {
				return report, processErr
			}
			retrySeen = retrySeen || retry
		}
		// A retryable item is released for the next invocation, never hot-looped by
		// the same worker. This bounds persistent external failures.
		if retrySeen {
			break
		}
	}
	if _, err = e.Store.FinalizeOperation(ctx, operation.ID, e.clock()); err != nil {
		return report, err
	}
	recordRetentionBytes(ctx, report.Bytes)
	e.audit(ctx, AuditSummary{
		Mode: "apply", PolicyVersion: report.PolicyVersion, Completed: report.Completed,
		CompletedBytes: report.Bytes, Retryable: report.Retryable, Failed: report.Failed,
		FailureClasses: report.FailureClasses,
	})
	return report, nil
}

// authorizeFirstApply re-evaluates the live policy/candidate set only while the
// durable operation is still planned. The successful status transition is the
// crash-resume boundary: subsequent invocations use persisted items unchanged.
func (e *Engine) authorizeFirstApply(ctx context.Context, token string, operation Operation, now time.Time) (Operation, error) {
	if operation.Status != StatusPlanned {
		return operation, nil
	}
	validity := e.PlanValidity
	if validity <= 0 {
		validity = defaultPlanValidity
	}
	if operation.PolicyVersion != e.Policy.Version || operation.CreatedAt.IsZero() || !now.Before(operation.CreatedAt.Add(validity)) {
		return Operation{}, ErrTokenMismatch
	}
	candidates, err := e.Source.Candidates(ctx, e.Policy, now)
	if err != nil {
		return Operation{}, fmt.Errorf("retention candidates: %w", err)
	}
	current, err := BuildPlan(e.Policy.Version, candidates, e.Policy.BatchSize)
	if err != nil {
		return Operation{}, err
	}
	if err := current.RequireToken(token); err != nil {
		return Operation{}, err
	}
	authorized, err := e.Store.Authorize(ctx, operation.ID, token, e.Policy.Version, now)
	if err != nil {
		return Operation{}, err
	}
	if authorized {
		operation.Status = StatusDeleting
		return operation, nil
	}
	// A concurrent worker may have authorized the same immutable snapshot. Reload
	// and resume only after observing that durable transition.
	operation, err = e.Store.GetByToken(ctx, token)
	if err != nil {
		return Operation{}, err
	}
	if operation.Status == StatusPlanned {
		return Operation{}, ErrTokenMismatch
	}
	return operation, nil
}

func (e *Engine) processItem(ctx context.Context, item Item, report *ApplyReport) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	revalidation, err := e.Revalidator.Revalidate(ctx, item.Candidate)
	if err != nil {
		return true, e.retry(ctx, item, report, "revalidate_unavailable")
	}
	if !revalidation.Owned {
		return false, e.fail(ctx, item, report, "ownership_mismatch")
	}
	if !revalidation.Exists {
		if err := e.Store.RecordArtifact(ctx, item.ID, e.WorkerID, ArtifactAbsent, 0, "", e.clock()); err != nil {
			return false, err
		}
		if err := e.Store.FinalizeItem(ctx, item.ID, e.WorkerID, e.clock()); err != nil {
			return false, err
		}
		report.Completed++
		e.metricSink().recordItems(ctx, "completed", "success", 1)
		return false, nil
	}
	if revalidation.Version != item.Candidate.Version {
		return true, e.retry(ctx, item, report, "version_changed")
	}
	if protected, reason := revalidation.Activity.Protected(e.clock(), e.ActivityFreshness); protected {
		return true, e.retry(ctx, item, report, reason)
	}

	removedBytes := item.RemovedBytes
	if item.ArtifactResult != ArtifactRemoved && item.ArtifactResult != ArtifactAbsent {
		removal, removeErr := e.Remover.Remove(ctx, item.Candidate)
		if removeErr != nil {
			if errors.Is(removeErr, ErrVersionConflict) {
				return true, e.retry(ctx, item, report, "version_changed")
			}
			if recordErr := e.Store.RecordArtifact(ctx, item.ID, e.WorkerID, ArtifactFailed, 0, "external_unavailable", e.clock()); recordErr != nil {
				return false, recordErr
			}
			return true, e.retry(ctx, item, report, "external_unavailable")
		}
		removedBytes = removal.Bytes
		result := ArtifactRemoved
		if removal.Absent {
			result = ArtifactAbsent
		}
		if err := e.Store.RecordArtifact(ctx, item.ID, e.WorkerID, result, removedBytes, "", e.clock()); err != nil {
			return false, err
		}
	}
	if err := e.Finalizer.Finalize(ctx, item.Candidate); err != nil {
		return true, e.retry(ctx, item, report, "metadata_unavailable")
	}
	if err := e.Store.FinalizeItem(ctx, item.ID, e.WorkerID, e.clock()); err != nil {
		return false, err
	}
	report.Completed++
	report.Bytes += removedBytes
	e.metricSink().recordItems(ctx, "completed", "success", 1)
	return false, nil
}

func (e *Engine) retry(ctx context.Context, item Item, report *ApplyReport, class string) error {
	if err := e.Store.RetryItem(ctx, item.ID, e.WorkerID, class, e.clock()); err != nil {
		return err
	}
	report.Retryable++
	report.FailureClasses[class]++
	return nil
}

func (e *Engine) fail(ctx context.Context, item Item, report *ApplyReport, class string) error {
	if err := e.Store.FailItem(ctx, item.ID, e.WorkerID, class, e.clock()); err != nil {
		return err
	}
	report.Failed++
	report.FailureClasses[class]++
	e.metricSink().recordItems(ctx, "completed", "error", 1)
	return nil
}

func (e *Engine) metricSink() retentionMetrics {
	if e != nil && e.metrics != nil {
		return e.metrics
	}
	return globalRetentionTelemetry
}

func (e *Engine) validate() error {
	if e == nil || e.Source == nil || e.Store == nil || e.Revalidator == nil || e.Remover == nil || e.Finalizer == nil {
		return errors.New("retention engine is not configured")
	}
	if err := e.Policy.Validate(); err != nil {
		return err
	}
	if e.WorkerID == "" {
		return errors.New("retention worker id is required")
	}
	return nil
}

func (e *Engine) clock() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) audit(ctx context.Context, summary AuditSummary) {
	if e.Audit != nil {
		e.Audit.Record(ctx, summary)
	}
}
