package handlers

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// KindObservabilityCheck is the system-seeded scrape-pipeline check admitted by
// migration 0104. It is never model-schedulable.
const KindObservabilityCheck TaskKind = "observability_check"

const observabilityCheckMaxDuration = 20 * time.Second

// ObservabilityChecker is the consumer-owned seam implemented by obs.SidecarChecker.
type ObservabilityChecker interface {
	Check(ctx context.Context) error
}

type observabilityCheckHandler struct {
	checker ObservabilityChecker

	mu          sync.Mutex
	initialized bool
	failed      bool
}

// NewObservabilityCheckHandler builds a transition-aware check. The first failure
// alerts, repeated failures stay failed on the run ledger without spamming, and the
// first successful check after a failure emits one recovery notification.
func NewObservabilityCheckHandler(checker ObservabilityChecker) Handler {
	return &observabilityCheckHandler{checker: checker}
}

func (h *observabilityCheckHandler) Meta() HandlerMeta {
	return HandlerMeta{
		Kind:                  KindObservabilityCheck,
		MaxDuration:           observabilityCheckMaxDuration,
		ReschedulesOnRecovery: false,
	}
}

func (h *observabilityCheckHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.checker == nil {
		return "", nil
	}
	err := h.checker.Check(ctx)

	h.mu.Lock()
	wasInitialized, wasFailed := h.initialized, h.failed
	h.initialized, h.failed = true, err != nil
	h.mu.Unlock()

	if err != nil {
		wrapped := fmt.Errorf("observability scrape check: %w", err)
		if wasInitialized && wasFailed {
			return "", repeatedObservabilityError{err: wrapped}
		}
		return "", wrapped
	}
	if wasInitialized && wasFailed {
		return "observability recovered: Tempo, Prometheus and Grafana datasources are ready; Prometheus is scraping Aura", nil
	}
	return "", nil
}

// repeatedObservabilityError keeps every unhealthy scheduler run truthfully failed
// while marking only the repeated notification as noise. Dispatch recognizes this
// tiny behavior interface without importing handlers.
type repeatedObservabilityError struct {
	err error
}

func (e repeatedObservabilityError) Error() string { return e.err.Error() }
func (e repeatedObservabilityError) Unwrap() error { return e.err }

// SuppressSchedulerNotification prevents one alert per five minutes for one outage.
func (e repeatedObservabilityError) SuppressSchedulerNotification() bool { return true }
