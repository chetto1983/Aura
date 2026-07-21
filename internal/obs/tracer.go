package obs

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
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
	options = append(options, sdktrace.WithBatcher(exporter))
	return sdktrace.NewTracerProvider(options...), nil
}

func installTraceProvider(provider *sdktrace.TracerProvider) {
	otel.SetTracerProvider(provider)
}
