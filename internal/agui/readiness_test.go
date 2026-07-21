package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runtimereadiness "github.com/chetto1983/aura/internal/readiness"
)

type readinessResponse struct {
	Status  string                  `json:"status"`
	Reasons []runtimereadiness.Code `json:"reasons"`
}

func healthyReadinessState() *runtimereadiness.Snapshot {
	s := runtimereadiness.NewSnapshot(runtimereadiness.Config{MigrationCompatible: true})
	s.MarkListenerRunning()
	return s
}

func TestReadinessProbesRunConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	check := func(name string) func(context.Context) error {
		return func(ctx context.Context) error {
			started <- name
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	s := NewServer(nil, nil, ServerConfig{
		ReadinessState: healthyReadinessState(),
		ReadinessProbes: []ReadinessProbe{
			{Name: "postgres", Code: runtimereadiness.CodePostgresUnavailable, Check: check("postgres")},
			{Name: "neo4j", Code: runtimereadiness.CodeNeo4jUnavailable, Check: check("neo4j")},
		},
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rr := httptest.NewRecorder()
		s.Mux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		done <- rr
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatal("both probes did not start concurrently")
		}
	}
	close(release)
	if rr := <-done; rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if !seen["postgres"] || !seen["neo4j"] {
		t.Fatalf("started probes = %v, want postgres and neo4j", seen)
	}
}

func TestReadinessGlobalDeadlineAndSanitizedSortedReasons(t *testing.T) {
	const secret = "postgres://operator:secret-sentinel@db.internal/aura"
	block := func(ctx context.Context) error {
		<-ctx.Done()
		return errors.New(secret + ": " + ctx.Err().Error())
	}
	s := NewServer(nil, nil, ServerConfig{
		ReadinessState: healthyReadinessState(),
		ReadinessProbes: []ReadinessProbe{
			{Name: "postgres", Code: runtimereadiness.CodePostgresUnavailable, Check: block},
			{Name: "neo4j", Code: runtimereadiness.CodeNeo4jUnavailable, Check: block},
		},
	})

	started := time.Now()
	rr := httptest.NewRecorder()
	s.Mux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	elapsed := time.Since(started)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rr.Code, rr.Body.String())
	}
	if elapsed < 1500*time.Millisecond || elapsed >= 3*time.Second {
		t.Fatalf("elapsed = %s, want one shared ~2s budget below Compose 3s", elapsed)
	}
	if strings.Contains(rr.Body.String(), secret) || strings.Contains(rr.Body.String(), "db.internal") {
		t.Fatalf("readiness response leaked dependency detail: %s", rr.Body.String())
	}
	var body readinessResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	want := []runtimereadiness.Code{
		runtimereadiness.CodeNeo4jUnavailable,
		runtimereadiness.CodePostgresUnavailable,
	}
	if body.Status != "unavailable" || !reflect.DeepEqual(body.Reasons, want) {
		t.Fatalf("body = %+v, want unavailable with sorted reasons %v", body, want)
	}
}

func TestReadinessDeadlineDoesNotWaitForProbeIgnoringCancellation(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s := NewServer(nil, nil, ServerConfig{
		ReadinessState: healthyReadinessState(),
		ReadinessProbes: []ReadinessProbe{{
			Name: "wedged",
			Check: func(context.Context) error {
				<-release
				return nil
			},
		}},
	})

	started := time.Now()
	rr := httptest.NewRecorder()
	s.Mux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if elapsed := time.Since(started); elapsed < 1500*time.Millisecond || elapsed >= 3*time.Second {
		t.Fatalf("elapsed = %s, want shared deadline without waiting for wedged probe", elapsed)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rr.Code, rr.Body.String())
	}
	var body readinessResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	want := []runtimereadiness.Code{runtimereadiness.CodeDependencyUnavailable}
	if !reflect.DeepEqual(body.Reasons, want) {
		t.Fatalf("reasons = %v, want %v", body.Reasons, want)
	}
}

func TestReadinessMergesSnapshotFailures(t *testing.T) {
	snap := runtimereadiness.NewSnapshot(runtimereadiness.Config{SchedulerEnabled: true})
	snap.MarkDraining()
	s := NewServer(nil, nil, ServerConfig{ReadinessState: snap})
	rr := httptest.NewRecorder()
	s.Mux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rr.Code, rr.Body.String())
	}
	var body readinessResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	want := []runtimereadiness.Code{
		runtimereadiness.CodeDraining,
		runtimereadiness.CodeListenerUnavailable,
		runtimereadiness.CodeMigrationIncompatible,
		runtimereadiness.CodeSchedulerStalled,
	}
	if !reflect.DeepEqual(body.Reasons, want) {
		t.Fatalf("reasons = %v, want %v", body.Reasons, want)
	}
}

func TestHealthzNeverInvokesReadinessProbes(t *testing.T) {
	var calls atomic.Int32
	s := NewServer(nil, nil, ServerConfig{
		HealthCheck: func(context.Context) error { return nil },
		ReadinessProbes: []ReadinessProbe{{
			Name: "postgres",
			Code: runtimereadiness.CodePostgresUnavailable,
			Check: func(context.Context) error {
				calls.Add(1)
				return errors.New("must not run")
			},
		}},
	})
	rr := httptest.NewRecorder()
	s.Mux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || calls.Load() != 0 {
		t.Fatalf("healthz status=%d readiness calls=%d, want 200/0", rr.Code, calls.Load())
	}
}

// TestServerReadyz pins O-05: /readyz is a READINESS probe over the required
// backends (PG + Neo4j). All probes healthy → 200; a failing probe → 503 with a
// terse body naming the failed dep. /healthz stays a cheap LIVENESS check and is
// unaffected by a readiness-dep being down.
func TestServerReadyz(t *testing.T) {
	t.Run("all probes healthy -> 200", func(t *testing.T) {
		srv := newTestServerCfg(t, nil, nil, ServerConfig{
			ReadinessProbes: []ReadinessProbe{
				{Name: "postgres", Check: func(context.Context) error { return nil }},
				{Name: "neo4j", Check: func(context.Context) error { return nil }},
			},
		})
		resp, err := http.Get(srv.URL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
		}
		if !strings.Contains(string(raw), `"ready":true`) {
			t.Fatalf("ready body missing ready flag: %s", raw)
		}
	})

	t.Run("a failing probe -> 503 naming the dep", func(t *testing.T) {
		srv := newTestServerCfg(t, nil, nil, ServerConfig{
			ReadinessProbes: []ReadinessProbe{
				{Name: "postgres", Check: func(context.Context) error { return nil }},
				{Name: "neo4j", Check: func(context.Context) error {
					return errors.New("neo4j connectivity (bolt://user:secret@host:7687): dial refused")
				}},
			},
		})
		resp, err := http.Get(srv.URL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", resp.StatusCode, raw)
		}
		body := string(raw)
		if !strings.Contains(body, "neo4j") {
			t.Fatalf("503 body must name the failed dep: %s", body)
		}
		if !strings.Contains(body, `"ready":false`) {
			t.Fatalf("503 body missing ready flag: %s", body)
		}
		// A failed probe must not leak a DSN/credential in its surfaced error.
		if strings.Contains(body, "secret") {
			t.Fatalf("readyz body leaked secret: %s", body)
		}
	})

	t.Run("no probes configured -> 200 ready", func(t *testing.T) {
		srv := newTestServerCfg(t, nil, nil, ServerConfig{})
		resp, err := http.Get(srv.URL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("healthz stays 200 when a readiness dep is down", func(t *testing.T) {
		// /healthz is liveness: its PG ping passes here, and a down Neo4j readiness
		// probe must not flip it to 503 (only /readyz reflects readiness deps).
		srv := newTestServerCfg(t, nil, nil, ServerConfig{
			HealthCheck: func(context.Context) error { return nil },
			ReadinessProbes: []ReadinessProbe{
				{Name: "neo4j", Check: func(context.Context) error { return errors.New("down") }},
			},
		})
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("healthz status = %d, want 200 (liveness, not readiness)", resp.StatusCode)
		}
	})
}
