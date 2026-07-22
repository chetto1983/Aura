package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/reasoningtrace"
	"github.com/chetto1983/aura/internal/retention"
)

const observabilityShutdownTimeout = 5 * time.Second

var serveListenerBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/cmd/aura", obs.BoundaryConfig{
	Operation: "listener_serve", Transport: "http", State: "running", Count: obs.ListenerTransitionsID,
})

type serveObservability struct {
	runtime      *obs.Runtime
	metrics      *privateMetricsComponent
	diskObserver diskObserverLifecycle
}

type diskObserverLifecycle interface {
	Start(context.Context)
	Stop()
}

func startServeObservability(
	ctx context.Context,
	cfg *config.Config,
	version string,
	listen listenFunc,
) (*serveObservability, error) {
	if cfg == nil {
		return nil, errors.New("start serve observability: config is nil")
	}
	runtime, err := obs.InitRuntime(ctx, obs.Config{
		Service:           "aura",
		Version:           version,
		OtelExporter:      cfg.OtelExporter,
		OtelEndpoint:      cfg.OtelEndpoint,
		EnablePrometheus:  true,
		EnableOTLPMetrics: cfg.OtelExporter == "otlp",
	})
	if err != nil {
		return nil, err
	}
	server := newPrivateMetricsServer(cfg.MetricsBind, runtime.MetricsHandler())
	listener, err := bindPrivateMetricsListener(server.Addr, listen)
	if err != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), observabilityShutdownTimeout)
		defer cancel()
		return nil, errors.Join(err, runtime.Shutdown(shutCtx))
	}
	diskObserver, err := retention.NewDiskObserver(retention.DiskObserverConfig{
		RunDir: cfg.RunDir,
		Thresholds: retention.DiskThresholds{
			Warn:         cfg.Retention.DiskWarnPercent,
			Urgent:       cfg.Retention.DiskUrgentPercent,
			StopOptional: cfg.Retention.DiskStopOptionalPercent,
		},
		SetOptionalTracesSuppressed: reasoningtrace.SetOptionalSuppressed,
	})
	if err != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), observabilityShutdownTimeout)
		defer cancel()
		return nil, errors.Join(err, listener.Close(), runtime.Shutdown(shutCtx))
	}
	diskObserver.Start(ctx)
	return &serveObservability{
		runtime:      runtime,
		metrics:      &privateMetricsComponent{listener: listener, server: server},
		diskObserver: diskObserver,
	}, nil
}

func (o *serveObservability) abort(ctx context.Context) error {
	if o == nil {
		return nil
	}
	var err error
	if o.diskObserver != nil {
		o.diskObserver.Stop()
	}
	if o.metrics != nil && o.metrics.listener != nil {
		err = errors.Join(err, o.metrics.listener.Close())
	}
	if o.runtime != nil {
		err = errors.Join(err, o.runtime.Shutdown(ctx))
	}
	return err
}

func (o *serveObservability) stopMetrics(ctx context.Context) error {
	if o == nil || o.metrics == nil || o.metrics.server == nil {
		return nil
	}
	return o.metrics.server.Shutdown(ctx)
}

func (o *serveObservability) shutdownRuntime() {
	if o == nil {
		return
	}
	if o.diskObserver != nil {
		o.diskObserver.Stop()
	}
	if o.runtime == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), observabilityShutdownTimeout)
	defer cancel()
	if err := o.runtime.Shutdown(ctx); err != nil {
		slog.Warn("aura serve: observability shutdown", "err", err)
	}
}

func (o *serveObservability) address() string {
	if o == nil || o.metrics == nil || o.metrics.server == nil {
		return ""
	}
	return o.metrics.server.Addr
}
