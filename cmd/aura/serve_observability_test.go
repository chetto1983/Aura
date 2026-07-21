package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/readiness"
)

func TestPrivateMetricsListenerRequiresLoopback(t *testing.T) {
	called := false
	_, err := bindPrivateMetricsListener("0.0.0.0:9464", func(string, string) (net.Listener, error) {
		called = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback bind error = %v, want loopback rejection", err)
	}
	if called {
		t.Fatal("listener factory called for rejected non-loopback address")
	}

	listener, err := bindPrivateMetricsListener("127.0.0.1:0", net.Listen)
	if err != nil {
		t.Fatalf("loopback bind: %v", err)
	}
	if listener == nil {
		t.Fatal("loopback bind returned nil listener")
	}
	_ = listener.Close()
}

func TestPrivateMetricsServerExposesOnlyDedicatedScrapeRoute(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := newPrivateMetricsServer("127.0.0.1:9464", handler)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusNoContent {
		t.Fatalf("GET /metrics status = %d, want 204", metricsRec.Code)
	}
	if got := metricsRec.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("private metrics route emitted auth cookie %q", got)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusNotFound {
		t.Fatalf("GET /readyz status = %d, want 404", readyRec.Code)
	}
}

func TestPrivateMetricsRuntimeFailureCancelsAndJoinsSiblings(t *testing.T) {
	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true, SchedulerEnabled: true})
	snapshot.MarkListenerBound()
	aguiListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("AG-UI listen: %v", err)
	}
	aguiServer := &http.Server{Handler: http.NotFoundHandler()}
	metricsErr := errors.New("metrics listener sentinel")
	metrics := &privateMetricsComponent{
		listener: newFailingMetricsListener(metricsErr),
		server:   newPrivateMetricsServer("127.0.0.1:0", http.NotFoundHandler()),
	}
	schedulerCancelled := make(chan struct{})
	scheduler := schedulerLifecycleFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(schedulerCancelled)
		return nil
	})

	err = runServeComponentsWithMetrics(context.Background(), snapshot, aguiListener, aguiServer, scheduler, metrics, func() {
		_ = aguiServer.Shutdown(context.Background())
		_ = metrics.server.Shutdown(context.Background())
	})
	if !errors.Is(err, metricsErr) {
		t.Fatalf("lifecycle error = %v, want metrics sentinel", err)
	}
	select {
	case <-schedulerCancelled:
	default:
		t.Fatal("metrics failure did not cancel and join scheduler")
	}
}

type failingMetricsListener struct {
	err error
}

func newFailingMetricsListener(err error) net.Listener {
	return &failingMetricsListener{err: err}
}

func (l *failingMetricsListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *failingMetricsListener) Close() error              { return nil }
func (l *failingMetricsListener) Addr() net.Addr            { return testMetricsAddr("127.0.0.1:0") }

type testMetricsAddr string

func (a testMetricsAddr) Network() string { return "tcp" }
func (a testMetricsAddr) String() string  { return string(a) }
