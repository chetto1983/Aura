package tracing

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestSetup(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	shutdown, err := Setup("test-service", "1.0.0", logger)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer shutdown(context.Background())

	tracer := otel.GetTracerProvider().Tracer("test")
	if tracer == nil {
		t.Error("expected non-nil tracer")
	}
}

func TestSetupIfEnabledDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	shutdown, err := SetupIfEnabled("test-service", "1.0.0", false, logger)
	if err != nil {
		t.Fatalf("SetupIfEnabled failed: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestSetupIfEnabledEnabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	shutdown, err := SetupIfEnabled("test-service", "1.0.0", true, logger)
	if err != nil {
		t.Fatalf("SetupIfEnabled failed: %v", err)
	}
	defer shutdown(context.Background())
}

func TestTracer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	shutdown, err := Setup("test-service", "1.0.0", logger)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer shutdown(context.Background())

	tr := Tracer("llm")
	if tr == nil {
		t.Error("expected non-nil tracer")
	}
}

func TestStartSpan(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	shutdown, err := Setup("test-service", "1.0.0", logger)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := StartSpan(context.Background(), "llm", "send")
	defer span.End()

	if span == nil {
		t.Error("expected non-nil span")
	}
	if ctx == nil {
		t.Error("expected non-nil context")
	}
}

func TestProviderIsSDK(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	shutdown, err := Setup("test-service", "1.0.0", logger)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer shutdown(context.Background())

	provider := otel.GetTracerProvider()
	if _, ok := provider.(*sdktrace.TracerProvider); !ok {
		t.Error("expected SDK tracer provider after setup")
	}
}