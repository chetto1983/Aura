package obs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

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
