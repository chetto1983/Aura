package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/retention"
)

func TestDiskObserverScrapesThroughRealObservabilityRuntime(t *testing.T) {
	runtime, err := obs.InitRuntime(t.Context(), obs.Config{
		Service:          "aura-disk-observer-test",
		OtelExporter:     "none",
		EnablePrometheus: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })
	observer, err := retention.NewDiskObserver(retention.DiskObserverConfig{
		RunDir:     t.TempDir(),
		Thresholds: retention.DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85},
		Interval:   time.Hour,
		SampleUsage: func(string) (float64, error) {
			return 0.80, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observer.Start(t.Context())
	t.Cleanup(observer.Stop)

	recorder := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `aura_retention_disk_utilization_ratio{state="draining"} 0.8`) {
		t.Fatalf("real metrics scrape missing current disk state:\n%s", body)
	}
}

func TestServeObservabilityShutdownStopsDiskObserver(t *testing.T) {
	observer := &fakeDiskObserverLifecycle{}
	component := &serveObservability{diskObserver: observer}
	component.shutdownRuntime()
	if !observer.stopped.Load() {
		t.Fatal("observability shutdown did not stop and join the disk observer")
	}
}

type fakeDiskObserverLifecycle struct {
	stopped atomic.Bool
}

func (*fakeDiskObserverLifecycle) Start(context.Context) {}

func (f *fakeDiskObserverLifecycle) Stop() {
	f.stopped.Store(true)
}
