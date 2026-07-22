package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	backlogObserverMeterName = "github.com/chetto1983/aura/internal/retention/backlog-observer"
	backlogQueryTimeout      = time.Second
)

// BacklogSource returns the current durable count of non-terminal retention items.
type BacklogSource interface {
	PendingCount(context.Context) (int64, error)
}

// BacklogObserver exports current durable retention state directly from PostgreSQL.
// It deliberately does not derive state from process-local transition counters.
type BacklogObserver struct {
	source       BacklogSource
	logger       *slog.Logger
	gauge        metric.Int64ObservableGauge
	registration metric.Registration
	stopOnce     sync.Once
}

// NewBacklogObserver registers the durable backlog gauge on the global provider.
func NewBacklogObserver(source BacklogSource) (*BacklogObserver, error) {
	return newBacklogObserver(otel.Meter(backlogObserverMeterName), source)
}

func newBacklogObserver(meter metric.Meter, source BacklogSource) (*BacklogObserver, error) {
	if source == nil {
		return nil, errors.New("retention backlog source is nil")
	}
	descriptor := obs.MustDescriptor(obs.RetentionBacklogID)
	gauge, err := meter.Int64ObservableGauge(
		descriptor.Name,
		metric.WithDescription(descriptor.Description),
		metric.WithUnit(descriptor.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("create retention backlog gauge: %w", err)
	}
	observer := &BacklogObserver{source: source, logger: slog.Default(), gauge: gauge}
	registration, err := meter.RegisterCallback(observer.observe, gauge)
	if err != nil {
		return nil, fmt.Errorf("register retention backlog callback: %w", err)
	}
	observer.registration = registration
	return observer, nil
}

func (o *BacklogObserver) observe(ctx context.Context, observer metric.Observer) error {
	queryCtx, cancel := context.WithTimeout(ctx, backlogQueryTimeout)
	defer cancel()
	count, err := o.source.PendingCount(queryCtx)
	if err != nil || count < 0 {
		// Keep the scrape healthy but omit the point: zero would falsely assert an
		// empty backlog. Do not log the raw DB error, which may contain connection data.
		o.logger.Warn("retention backlog observation failed")
		return nil
	}
	observer.ObserveInt64(o.gauge, count)
	return nil
}

// Stop unregisters the scrape callback. It is safe to call repeatedly.
func (o *BacklogObserver) Stop() {
	if o == nil {
		return
	}
	o.stopOnce.Do(func() {
		if o.registration != nil {
			if err := o.registration.Unregister(); err != nil {
				o.logger.Warn("retention backlog metric unregister failed")
			}
		}
	})
}
