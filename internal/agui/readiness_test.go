package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
