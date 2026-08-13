package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/readiness"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestListenerRuntimeFailureEmitsActualTerminalLabels(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	boundary, err := obs.NewBoundary(
		provider.Meter("aura.listener.lifecycle.test"),
		noop.NewTracerProvider().Tracer("aura.listener.lifecycle.test"),
		obs.BoundaryConfig{Operation: "listener_serve", Transport: "http", State: "running", Count: obs.ListenerTransitionsID},
	)
	if err != nil {
		t.Fatalf("NewBoundary: %v", err)
	}
	previous := serveListenerBoundary
	serveListenerBoundary = boundary
	t.Cleanup(func() {
		serveListenerBoundary = previous
		_ = provider.Shutdown(context.Background())
	})

	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true, SchedulerEnabled: true})
	snapshot.MarkListenerBound()
	listener := newFailingServeListener()
	server := &http.Server{Handler: http.NotFoundHandler()}
	scheduler := schedulerLifecycleFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	err = runServeComponents(context.Background(), snapshot, listener, server, scheduler, func() {
		_ = server.Shutdown(context.Background())
	})
	if err == nil {
		t.Fatal("listener failure returned nil")
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect listener metrics: %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "aura.listener.transition" {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
				t.Fatalf("listener transition data = %#v, want one terminal event", instrument.Data)
			}
			attrs := sum.DataPoints[0].Attributes.ToSlice()
			if !metricHasString(attrs, "state", "running") || !metricHasString(attrs, "outcome", "error") {
				t.Fatalf("listener transition attrs = %v, want state=running,outcome=error", attrs)
			}
			return
		}
	}
	t.Fatal("aura.listener.transition metric not found")
}

func metricHasString(attrs []attribute.KeyValue, key, value string) bool {
	for _, attr := range attrs {
		if string(attr.Key) == key && attr.Value.AsString() == value {
			return true
		}
	}
	return false
}

// TestMetricsListenerAllowsNonLoopback inverts what this test used to assert, and the
// inversion is the point rather than a test bent to go green.
//
// It required the bind to be loopback. Inside a container that check is meaningless:
// the bind address does not decide who can reach the socket — publishing the port does,
// and that lives in compose where this code cannot see it. So the guard rejected the
// safe, ordinary arrangement (Prometheus in its own container on a private network) and
// forced the observability pack into aura's network namespace instead. On 2026-08-13
// that topology cost five and a half hours of `up{job="aura"} == 0` after the namespace
// owner was recreated, with nothing reporting it.
//
// Loopback remains the DEFAULT (config.go); a non-loopback bind is now allowed and
// logged at WARN.
func TestMetricsListenerAllowsNonLoopback(t *testing.T) {
	called := false
	_, err := bindMetricsListener("0.0.0.0:9464", func(network, address string) (net.Listener, error) {
		called = true
		if network != "tcp" || address != "0.0.0.0:9464" {
			t.Errorf("listener factory called with (%q,%q)", network, address)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("non-loopback bind = %v, want it to be permitted", err)
	}
	if !called {
		t.Fatal("listener factory was not called for a non-loopback address")
	}

	listener, err := bindMetricsListener("127.0.0.1:0", net.Listen)
	if err != nil {
		t.Fatalf("loopback bind: %v", err)
	}
	if listener == nil {
		t.Fatal("loopback bind returned nil listener")
	}
	_ = listener.Close()
}

// TestMetricsListenerRejectsAMalformedAddress keeps the half of the guard that was
// always right: an address that cannot be split is a configuration error, and the
// listener factory must not be called for it.
func TestMetricsListenerRejectsAMalformedAddress(t *testing.T) {
	called := false
	_, err := bindMetricsListener("not-an-address", func(string, string) (net.Listener, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("malformed address was accepted")
	}
	if called {
		t.Fatal("listener factory called for a malformed address")
	}
}

func TestPrivateMetricsServerExposesOnlyDedicatedScrapeRoute(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := newMetricsServer("127.0.0.1:9464", handler)

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
		server:   newMetricsServer("127.0.0.1:0", http.NotFoundHandler()),
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
