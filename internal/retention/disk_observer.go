package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	diskObserverMeterName   = "github.com/chetto1983/aura/internal/retention/disk-observer"
	diskObservationInterval = time.Minute
	diskMetricStateReady    = "ready"
	diskMetricStateDegraded = "degraded"
	diskMetricStateDraining = "draining"
	diskMetricStateStopped  = "stopped"
)

// DiskUsageSampler returns the used ratio of the filesystem containing path.
type DiskUsageSampler func(path string) (float64, error)

// DiskObserverConfig supplies the Aura-owned filesystem and bounded pressure actions.
type DiskObserverConfig struct {
	RunDir                      string
	Thresholds                  DiskThresholds
	Interval                    time.Duration
	SampleUsage                 DiskUsageSampler
	SetOptionalTracesSuppressed func(bool)
	Logger                      *slog.Logger
}

type diskObservation struct {
	ratio float64
	state string
}

// DiskObserver owns current-state telemetry and the optional-trace pressure signal.
type DiskObserver struct {
	runDir        string
	thresholds    DiskThresholds
	interval      time.Duration
	sampleUsage   DiskUsageSampler
	setSuppressed func(bool)
	logger        *slog.Logger
	gauge         metric.Float64ObservableGauge
	registration  metric.Registration
	current       atomic.Pointer[diskObservation]

	sampleMu               sync.Mutex
	suppressionInitialized bool
	lastSuppressed         bool
	lifecycleMu            sync.Mutex
	cancel                 context.CancelFunc
	done                   chan struct{}
	started                bool
	stopped                bool
}

// NewDiskObserver registers the catalog gauge against the process-global meter provider.
func NewDiskObserver(cfg DiskObserverConfig) (*DiskObserver, error) {
	return newDiskObserver(otel.Meter(diskObserverMeterName), cfg)
}

func newDiskObserver(meter metric.Meter, cfg DiskObserverConfig) (*DiskObserver, error) {
	if cfg.RunDir == "" || !filepath.IsAbs(cfg.RunDir) {
		return nil, fmt.Errorf("disk observer run dir must be absolute")
	}
	if err := cfg.Thresholds.Validate(); err != nil {
		return nil, fmt.Errorf("disk observer thresholds: %w", err)
	}
	if cfg.Interval < 0 {
		return nil, fmt.Errorf("disk observer interval must not be negative")
	}
	if cfg.Interval == 0 {
		cfg.Interval = diskObservationInterval
	}
	if cfg.SampleUsage == nil {
		cfg.SampleUsage = sampleDiskUsage
	}
	if cfg.SetOptionalTracesSuppressed == nil {
		cfg.SetOptionalTracesSuppressed = func(bool) {}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	descriptor := obs.MustDescriptor(obs.RetentionDiskUtilizationID)
	gauge, err := meter.Float64ObservableGauge(
		descriptor.Name,
		metric.WithDescription(descriptor.Description),
		metric.WithUnit(descriptor.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("create retention disk utilization gauge: %w", err)
	}
	observer := &DiskObserver{
		runDir:        cfg.RunDir,
		thresholds:    cfg.Thresholds,
		interval:      cfg.Interval,
		sampleUsage:   cfg.SampleUsage,
		setSuppressed: cfg.SetOptionalTracesSuppressed,
		logger:        cfg.Logger,
		gauge:         gauge,
	}
	registration, err := meter.RegisterCallback(observer.observe, gauge)
	if err != nil {
		return nil, fmt.Errorf("register retention disk utilization callback: %w", err)
	}
	observer.registration = registration
	return observer, nil
}

// Start samples immediately, then refreshes the current observation periodically.
func (o *DiskObserver) Start(parent context.Context) {
	if o == nil {
		return
	}
	o.lifecycleMu.Lock()
	if o.started || o.stopped {
		o.lifecycleMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	o.cancel = cancel
	o.done = done
	o.started = true
	o.lifecycleMu.Unlock()

	o.sample()
	go o.run(ctx, done)
}

// Stop cancels and joins the observer before unregistering its metric callback.
func (o *DiskObserver) Stop() {
	if o == nil {
		return
	}
	o.lifecycleMu.Lock()
	if o.stopped {
		o.lifecycleMu.Unlock()
		return
	}
	o.stopped = true
	cancel := o.cancel
	done := o.done
	registration := o.registration
	o.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if registration != nil {
		if err := registration.Unregister(); err != nil {
			o.logger.Warn("retention disk metric unregister failed", "err", err)
		}
	}
	o.sampleMu.Lock()
	if o.suppressionInitialized && o.lastSuppressed {
		o.setSuppressed(false)
		o.lastSuppressed = false
	}
	o.sampleMu.Unlock()
}

func (o *DiskObserver) run(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.sample()
		}
	}
}

func (o *DiskObserver) sample() {
	o.sampleMu.Lock()
	defer o.sampleMu.Unlock()
	ratio, err := o.sampleUsage(o.runDir)
	if err != nil {
		o.logger.Warn("retention disk observation failed", "err", err)
		return
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		o.logger.Warn("retention disk observation failed", "err", fmt.Errorf("invalid used ratio %v", ratio))
		return
	}
	state := diskMetricState(ratio, o.thresholds)
	previous := o.current.Load()
	o.current.Store(&diskObservation{ratio: ratio, state: state})
	suppressed := state == diskMetricStateStopped
	if !o.suppressionInitialized || suppressed != o.lastSuppressed {
		o.setSuppressed(suppressed)
		o.suppressionInitialized = true
		o.lastSuppressed = suppressed
	}
	if previous == nil || previous.state != state {
		o.logTransition(state, ratio)
	}
}

func (o *DiskObserver) observe(_ context.Context, observer metric.Observer) error {
	current := o.current.Load()
	if current == nil {
		return nil
	}
	observer.ObserveFloat64(o.gauge, current.ratio, metric.WithAttributes(
		attribute.String(string(obs.AttributeState), current.state),
	))
	return nil
}

func (o *DiskObserver) logTransition(state string, ratio float64) {
	attrs := []any{"state", state, "used_ratio", ratio}
	switch state {
	case diskMetricStateStopped:
		o.logger.Error("retention disk pressure transition", attrs...)
	case diskMetricStateDegraded, diskMetricStateDraining:
		o.logger.Warn("retention disk pressure transition", attrs...)
	default:
		o.logger.Info("retention disk pressure transition", attrs...)
	}
}

func diskMetricState(ratio float64, thresholds DiskThresholds) string {
	pressure := thresholds.EvaluateRatio(ratio)
	switch {
	case pressure.StopOptionalTraces:
		return diskMetricStateStopped
	case pressure.Urgent:
		return diskMetricStateDraining
	case pressure.Warn:
		return diskMetricStateDegraded
	default:
		return diskMetricStateReady
	}
}

func ratioFromCapacity(total, available uint64) (float64, error) {
	if total == 0 {
		return 0, errors.New("filesystem capacity is zero")
	}
	if available > total {
		available = total
	}
	return 1 - float64(available)/float64(total), nil
}
