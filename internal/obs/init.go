// Package obs centralizes process observability bootstrap: structured logs,
// redaction, and the global OpenTelemetry tracer provider.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agui"
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
	Service      string
	Version      string
	OtelExporter string
	OtelEndpoint string
	LogWriter    io.Writer
}

// ShutdownFunc adapts Init's shutdown function to agent.TracerProvider for older
// callsites that still hold the narrow provider interface.
type ShutdownFunc func(context.Context) error

// Shutdown flushes and releases the wrapped observability provider.
func (f ShutdownFunc) Shutdown(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

// Init installs a JSON slog logger with stable service metadata and wires the
// global OTel tracer provider. String log attributes are redacted before they hit
// the sink so DSNs and tokens do not leak through operational logs.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
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

	exporter := cfg.OtelExporter
	if exporter == "" {
		exporter = defaultExporter
	}
	endpoint := cfg.OtelEndpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	tp, err := agent.NewTracerProvider(ctx, exporter, endpoint)
	if err != nil {
		return nil, err
	}
	return tp.Shutdown, nil
}

func redactAttr(groups []string, a slog.Attr) slog.Attr {
	_ = groups
	if a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue(agui.SanitizeString(a.Value.String()))
	}
	return a
}
