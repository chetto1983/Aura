package agui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// readinessProbeTimeout bounds each dependency probe so a hung backend cannot
// wedge /readyz behind a slow dial — a load balancer polling readiness must get a
// prompt answer (O-05/AP-14). Each probe runs under its own derived ctx.
const readinessProbeTimeout = 3 * time.Second

// ReadinessProbe is one named dependency-readiness check the server runs on
// /readyz. Check returns nil when the dependency is reachable, a non-nil error
// (surfaced sanitized in the 503 body) when it is not. Probes are injected by the
// composition root (cmd/aura wires the real PG + Neo4j probes) so they stay
// testable and the set is easy to extend without touching the handler.
type ReadinessProbe struct {
	Name  string
	Check func(context.Context) error
}

// handleReadyz is the READINESS endpoint (distinct from the cheap /healthz
// liveness check, O-05/AP-14). It runs every configured probe under a short
// bounded ctx and returns 200 only when ALL required dependencies are reachable;
// otherwise 503 with a terse body naming each failed dep so an orchestrator stops
// routing traffic to an instance whose required backend is down. With no probes
// configured it reports ready (the daemon was started without gated deps — do not
// falsely fail; AP-14).
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ready := true
	deps := make(map[string]string, len(s.cfg.ReadinessProbes))
	for _, p := range s.cfg.ReadinessProbes {
		if p.Check == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), readinessProbeTimeout)
		err := p.Check(ctx)
		cancel()
		if err != nil {
			ready = false
			deps[p.Name] = SanitizeString(err.Error())
			continue
		}
		deps[p.Name] = "ok"
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"ready": ready, "deps": deps}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("agui: encode readyz response", "err", err)
	}
}
