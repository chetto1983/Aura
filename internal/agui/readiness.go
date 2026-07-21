package agui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/readiness"
)

// ReadinessProbeBudget is the one shared deadline for every dependency probe, so
// wedge /readyz behind a slow dial — a load balancer polling readiness must get a
// prompt answer (O-05/AP-14) below the Compose client timeout.
const ReadinessProbeBudget = 2 * time.Second

// ReadinessProbe is one named dependency-readiness check the server runs on
// /readyz. Check returns nil when the dependency is reachable and a non-nil error
// when it is not. Only Code reaches the response; redacted detail is logged.
// Probes are injected by the
// composition root (cmd/aura wires the real PG + Neo4j probes) so they stay
// testable and the set is easy to extend without touching the handler.
type ReadinessProbe struct {
	Name  string
	Code  readiness.Code
	Check func(context.Context) error
}

// handleReadyz is the READINESS endpoint (distinct from the cheap /healthz
// liveness check, O-05/AP-14). It runs every configured probe concurrently under
// one bounded context and returns 200 only when every dependency and runtime state
// is healthy; otherwise it returns 503 with sorted stable reason codes. With no
// probes
// configured it reports ready (the daemon was started without gated deps — do not
// falsely fail; AP-14).
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	reasons := make(map[readiness.Code]struct{}, len(s.cfg.ReadinessProbes)+4)
	if s.cfg.ReadinessState != nil {
		for _, code := range s.cfg.ReadinessState.Reasons() {
			reasons[code] = struct{}{}
		}
	}

	type probeResult struct {
		probe ReadinessProbe
		err   error
	}
	ctx, cancel := context.WithTimeout(r.Context(), ReadinessProbeBudget)
	defer cancel()
	results := make(chan probeResult, len(s.cfg.ReadinessProbes))
	var wg sync.WaitGroup
	for _, probe := range s.cfg.ReadinessProbes {
		if probe.Check == nil {
			continue
		}
		wg.Go(func() {
			results <- probeResult{probe: probe, err: probe.Check(ctx)}
		})
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.err == nil {
			continue
		}
		code := result.probe.Code
		if code == "" {
			code = readinessCodeForName(result.probe.Name)
		}
		reasons[code] = struct{}{}
		slog.Warn("agui: readiness probe failed",
			"probe", result.probe.Name,
			"error", SanitizeString(result.err.Error()),
		)
	}

	codes := make([]readiness.Code, 0, len(reasons))
	for code := range reasons {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	ready := len(codes) == 0

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := struct {
		Status  string           `json:"status"`
		Ready   bool             `json:"ready"`
		Reasons []readiness.Code `json:"reasons"`
	}{Status: "ready", Ready: ready, Reasons: codes}
	if !ready {
		body.Status = "unavailable"
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("agui: encode readyz response", "err", err)
	}
}

func readinessCodeForName(name string) readiness.Code {
	switch name {
	case "postgres":
		return readiness.CodePostgresUnavailable
	case "neo4j":
		return readiness.CodeNeo4jUnavailable
	default:
		return readiness.CodeDependencyUnavailable
	}
}
