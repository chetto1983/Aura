package obs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type secretStringer string

func (s secretStringer) String() string { return string(s) }

type secretLogValuer string

func (s secretLogValuer) LogValue() slog.Value { return slog.StringValue(string(s)) }

func TestInitInstallsJSONLoggerWithServiceAttrsAndTracerShutdown(t *testing.T) {
	var logs bytes.Buffer
	shutdown, err := Init(context.Background(), Config{
		Service:      "aura-test",
		Version:      "test-version",
		OtelExporter: "none",
		LogWriter:    &logs,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	slog.Info("probe", "thread_id", "thread-1", "dsn", "postgresql://user:secret@localhost/db")
	line := logs.String()
	for _, want := range []string{`"service":"aura-test"`, `"version":"test-version"`, `"thread_id":"thread-1"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("JSON log missing %s: %s", want, line)
		}
	}
	if strings.Contains(line, "secret") {
		t.Fatalf("JSON log leaked a secret-bearing DSN: %s", line)
	}
}

func TestInitAppliesDefaultServiceAttrs(t *testing.T) {
	var logs bytes.Buffer
	shutdown, err := Init(context.Background(), Config{
		OtelExporter: "none",
		LogWriter:    &logs,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	slog.Info("defaults")
	line := logs.String()
	for _, want := range []string{`"service":"aura"`, `"version":"dev"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("JSON log missing %s: %s", want, line)
		}
	}
}

func TestInitRedactsErrorsStringersLogValuersAndNestedGroups(t *testing.T) {
	var logs bytes.Buffer
	shutdown, err := Init(context.Background(), Config{OtelExporter: "none", LogWriter: &logs})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	slog.Info("redaction",
		slog.Any("err", errors.New("postgres://user:pass@host/db")),
		slog.Any("stringer", secretStringer("password=hunter2")),
		slog.Any("valuer", secretLogValuer("client_secret=client-value")),
		slog.Group("nested", slog.Any("err", fmt.Errorf("request failed: bearer token-value"))),
	)
	line := logs.String()
	for _, secret := range []string{"user:pass", "hunter2", "client-value", "token-value"} {
		if strings.Contains(line, secret) {
			t.Fatalf("JSON log leaked %q: %s", secret, line)
		}
	}
	if got := strings.Count(line, "[redacted]"); got < 4 {
		t.Fatalf("JSON log redaction count = %d, want at least 4: %s", got, line)
	}
}

func TestShutdownFuncNilAndErrorPaths(t *testing.T) {
	var nilShutdown ShutdownFunc
	if err := nilShutdown.Shutdown(context.Background()); err != nil {
		t.Fatalf("nil shutdown returned error: %v", err)
	}

	wantErr := errors.New("shutdown failed")
	called := false
	err := ShutdownFunc(func(context.Context) error {
		called = true
		return wantErr
	}).Shutdown(context.Background())
	if !called {
		t.Fatal("shutdown function was not called")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("shutdown error = %v, want %v", err, wantErr)
	}
}
