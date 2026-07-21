// Package obs centralizes process observability bootstrap: structured logs,
// redaction, and the global OpenTelemetry trace and metric providers.
package obs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/chetto1983/aura/internal/redact"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

const (
	defaultService  = "aura"
	defaultVersion  = "dev"
	defaultExporter = "otlp"
	defaultEndpoint = "localhost:4317"
)

// Config carries the process-level observability knobs resolved by the binary
// edge. LogWriter is injectable so tests can assert the JSON log shape.
type Config struct {
	Service           string
	Version           string
	OtelExporter      string
	OtelEndpoint      string
	EnablePrometheus  bool
	EnableOTLPMetrics bool
	LogWriter         io.Writer
}

// ShutdownFunc is the bounded process-observability cleanup contract.
type ShutdownFunc func(context.Context) error

// Shutdown flushes and releases the wrapped observability provider.
func (f ShutdownFunc) Shutdown(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

// Runtime owns the process-global OTel providers and the private Prometheus
// scrape handler. The handler is intentionally not registered on any public mux.
type Runtime struct {
	handler  http.Handler
	shutdown ShutdownFunc
}

// MetricsHandler returns the dedicated-registry scrape handler, or nil when the
// Prometheus reader is disabled.
func (r *Runtime) MetricsHandler() http.Handler {
	if r == nil {
		return nil
	}
	return r.handler
}

// Shutdown drains all constructed readers/exporters in reverse order exactly once.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.shutdown == nil {
		return nil
	}
	return r.shutdown(ctx)
}

// Init preserves the historical shutdown-function API for non-daemon callsites.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	runtime, err := InitRuntime(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return runtime.Shutdown, nil
}

// InitRuntime installs the structured logger and one process resource shared by
// the trace provider and all metric readers.
func InitRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	service := cfg.Service
	if service == "" {
		service = defaultService
	}
	version := cfg.Version
	if version == "" {
		version = defaultVersion
	}
	writer := cfg.LogWriter
	if writer == nil {
		writer = os.Stderr
	}

	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		ReplaceAttr: redactAttr,
	})).With("service", service, "version", version)
	slog.SetDefault(logger)

	// Surface OTel SDK errors (export failures, queue-full drops) through the
	// structured logger instead of swallowing them — rate-limited so a broken
	// exporter cannot flood the logs (O-03).
	installOTelErrorHandler(logger)

	exporter := cfg.OtelExporter
	if exporter == "" {
		exporter = defaultExporter
	}
	endpoint := cfg.OtelEndpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceNameKey.String(service),
		semconv.ServiceVersionKey.String(version),
	))
	if err != nil {
		return nil, fmt.Errorf("create OTel resource: %w", err)
	}
	tp, err := newTraceProvider(ctx, res, exporter, endpoint)
	if err != nil {
		return nil, err
	}
	meter, err := buildMeterRuntime(ctx, meterOptions{
		resource:         res,
		enablePrometheus: cfg.EnablePrometheus,
		enableOTLP:       cfg.EnableOTLPMetrics,
		endpoint:         endpoint,
	})
	if err != nil {
		return nil, errors.Join(err, tp.Shutdown(ctx))
	}

	installTraceProvider(tp)
	otel.SetMeterProvider(meter.provider)
	return &Runtime{
		handler:  meter.handler,
		shutdown: newShutdownStack(tp.Shutdown, meter.shutdown),
	}, nil
}

func redactAttr(groups []string, a slog.Attr) slog.Attr {
	_ = groups
	if a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue(redact.String(a.Value.String()))
	}
	return a
}
