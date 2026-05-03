package tracing

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Setup initializes OpenTelemetry tracing with a stdout exporter.
// Returns a shutdown function that must be called on exit.
func Setup(serviceName, version string, logger *slog.Logger) (func(context.Context) error, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
	}

	res, err := resource.New(
		context.Background(),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)

	// Set global text map propagator for distributed tracing
	otel.SetTextMapPropagator(otel.GetTextMapPropagator())

	logger.Info("tracing initialized", "service", serviceName, "version", version)

	return provider.Shutdown, nil
}

// SetupIfEnabled initializes tracing only if enabled is true.
// Returns a no-op shutdown function if tracing is disabled.
func SetupIfEnabled(serviceName, version string, enabled bool, logger *slog.Logger) (func(context.Context) error, error) {
	if !enabled {
		logger.Info("tracing disabled (set OTEL_ENABLED=true to enable)")
		return func(context.Context) error { return nil }, nil
	}
	return Setup(serviceName, version, logger)
}

// Tracer returns a named tracer for the given component.
func Tracer(name string) trace.Tracer {
	return otel.GetTracerProvider().Tracer("github.com/aura/aura/" + name)
}

// StartSpan starts a new span for the given component and operation.
func StartSpan(ctx context.Context, component, operation string) (context.Context, trace.Span) {
	return Tracer(component).Start(ctx, operation)
}
