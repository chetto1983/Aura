package agui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
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
// Probes are injected by the composition root (cmd/aura wires the Postgres pool
// ping and a functional search through the mounted memory MCP) so they stay
// testable and the set is easy to extend without touching the handler.
type ReadinessProbe struct {
	Name  string
	Code  readiness.Code
	Check func(context.Context) error
}

type readinessProbeRun struct {
	done chan struct{}
	err  error
}

// readinessProbeCoordinator keeps at most one concrete Check execution in flight
// per configured probe. Request-local waiters may time out, but a non-cooperative
// dependency adapter cannot accumulate one permanent goroutine per readiness poll.
type readinessProbeCoordinator struct {
	mu      sync.Mutex
	running map[int]*readinessProbeRun
}

func (s *Server) awaitReadinessProbe(ctx context.Context, index int, probe ReadinessProbe) error {
	coordinator := &s.readinessRuns
	coordinator.mu.Lock()
	if coordinator.running == nil {
		coordinator.running = make(map[int]*readinessProbeRun)
	}
	run := coordinator.running[index]
	if run == nil {
		run = &readinessProbeRun{done: make(chan struct{})}
		coordinator.running[index] = run
		go func(probeCtx context.Context) {
			run.err = probe.Check(probeCtx)
			close(run.done)
			coordinator.mu.Lock()
			if coordinator.running[index] == run {
				delete(coordinator.running, index)
			}
			coordinator.mu.Unlock()
		}(ctx)
	}
	coordinator.mu.Unlock()

	select {
	case <-run.done:
		return run.err
	case <-ctx.Done():
		return ctx.Err()
	}
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
		index int
		probe ReadinessProbe
		err   error
	}
	ctx, cancel := context.WithTimeout(r.Context(), ReadinessProbeBudget)
	defer cancel()
	results := make(chan probeResult, len(s.cfg.ReadinessProbes))
	pending := make(map[int]ReadinessProbe, len(s.cfg.ReadinessProbes))
	for i, probe := range s.cfg.ReadinessProbes {
		if probe.Check == nil {
			continue
		}
		pending[i] = probe
		go func(index int, current ReadinessProbe) {
			results <- probeResult{index: index, probe: current, err: s.awaitReadinessProbe(ctx, index, current)}
		}(i, probe)
	}
	for len(pending) > 0 {
		select {
		case result := <-results:
			delete(pending, result.index)
			if result.err != nil {
				addReadinessProbeFailure(reasons, result.probe, result.err)
			}
		case <-ctx.Done():
			for _, probe := range pending {
				addReadinessProbeFailure(reasons, probe, ctx.Err())
			}
			clear(pending)
		}
	}

	codes := make([]readiness.Code, 0, len(reasons))
	for code := range reasons {
		codes = append(codes, code)
	}
	slices.Sort(codes)
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

func addReadinessProbeFailure(reasons map[readiness.Code]struct{}, probe ReadinessProbe, err error) {
	code := probe.Code
	if code == "" {
		code = readinessCodeForName(probe.Name)
	}
	reasons[code] = struct{}{}
	slog.Warn("agui: readiness probe failed",
		"probe", probe.Name,
		"error", SanitizeString(err.Error()),
	)
}

func readinessCodeForName(name string) readiness.Code {
	switch name {
	case "postgres":
		return readiness.CodePostgresUnavailable
	case "memory":
		return readiness.CodeMemoryUnavailable
	default:
		return readiness.CodeDependencyUnavailable
	}
}
