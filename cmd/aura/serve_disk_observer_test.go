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
	backlog, err := retention.NewBacklogObserver(staticBacklogSource(3))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backlog.Stop)

	recorder := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `aura_retention_disk_utilization_ratio{state="draining"} 0.8`) {
		t.Fatalf("real metrics scrape missing current disk state:\n%s", body)
	}
	if !strings.Contains(body, `aura_retention_backlog_items 3`) {
		t.Fatalf("real metrics scrape missing durable retention backlog:\n%s", body)
	}
}

func TestServeObservabilityShutdownStopsObservers(t *testing.T) {
	disk := &fakeDiskObserverLifecycle{}
	backlog := &fakeObserverStopper{}
	component := &serveObservability{diskObserver: disk, backlog: backlog}
	component.shutdownRuntime()
	if !disk.stopped.Load() {
		t.Fatal("observability shutdown did not stop and join the disk observer")
	}
	if !backlog.stopped.Load() {
		t.Fatal("observability shutdown did not unregister the backlog observer")
	}
}

type staticBacklogSource int64

func (s staticBacklogSource) PendingCount(context.Context) (int64, error) { return int64(s), nil }

type fakeDiskObserverLifecycle struct {
	stopped atomic.Bool
}

func (*fakeDiskObserverLifecycle) Start(context.Context) {}

func (f *fakeDiskObserverLifecycle) Stop() {
	f.stopped.Store(true)
}

type fakeObserverStopper struct {
	stopped atomic.Bool
}

func (f *fakeObserverStopper) Stop() {
	f.stopped.Store(true)
}
