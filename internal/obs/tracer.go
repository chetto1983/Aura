package obs

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	exporterNone   = "none"
	exporterStdout = "stdout"
	exporterOTLP   = "otlp"
)

func newTraceProvider(
	ctx context.Context,
	res *resource.Resource,
	mode string,
	endpoint string,
) (*sdktrace.TracerProvider, error) {
	options := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if mode == exporterNone {
		return sdktrace.NewTracerProvider(options...), nil
	}

	var (
		exporter sdktrace.SpanExporter
		err      error
	)
	switch mode {
	case exporterStdout:
		exporter, err = stdouttrace.New()
	case exporterOTLP:
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
	default:
		return nil, fmt.Errorf("unsupported OTel trace exporter %q", mode)
	}
	if err != nil {
		return nil, fmt.Errorf("create %s trace exporter: %w", mode, err)
	}
	// The global meter delegates to the provider InitRuntime installs after this returns.
	exporter, err = countSpanExportFailures(exporter, otel.Meter("github.com/chetto1983/aura/internal/obs"))
	if err != nil {
		return nil, err
	}
	options = append(options, sdktrace.WithBatcher(exporter))
	return sdktrace.NewTracerProvider(options...), nil
}

func installTraceProvider(provider *sdktrace.TracerProvider) {
	otel.SetTracerProvider(provider)
}

// spanExportCounter counts every failed ExportSpans call. The process OTel error handler
// sees these failures too, but it also sees metric-export errors and queue drops and cannot
// tell them apart, so the exporter call is the one site where the count is exact.
type spanExportCounter struct {
	sdktrace.SpanExporter
	failures metric.Int64Counter
}

func countSpanExportFailures(exporter sdktrace.SpanExporter, meter metric.Meter) (sdktrace.SpanExporter, error) {
	descriptor := MustDescriptor(AgentSpanExportFailuresID)
	failures, err := meter.Int64Counter(descriptor.Name,
		metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		return nil, fmt.Errorf("span export failure counter: %w", err)
	}
	return spanExportCounter{SpanExporter: exporter, failures: failures}, nil
}

func (e spanExportCounter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	if err != nil {
		e.failures.Add(ctx, 1)
	}
	return err
}
