package sandbox

import (
	"context"
	"fmt"
	"log/slog"
)

// RecoverOnBoot is the D-06 boot reconciliation (parity with agent_job_runs). A
// prior Aura process's session containers are gone or orphaned, so every 'active'
// registry row is marked terminated (lazy-recreate on the next Acquire). It then
// runs a docker-ps stray sweep that docker-rm's any leftover aura-sandbox-sess-*
// containers a crash left behind (A7 — defense in depth). Per-row/per-container
// failures are WARN-logged and recovered next boot; only a structural ListActive
// failure returns an error.
func (m *SessionManager) RecoverOnBoot(ctx context.Context) error {
	rows, err := m.store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("session boot recovery: list active: %w", err)
	}
	for _, row := range rows {
		if mErr := m.store.MarkTerminated(ctx, row.ID); mErr != nil {
			slog.Warn("session boot recovery: mark terminated failed (retry next boot)",
				"container_id", row.ContainerID, "err", mErr)
		}
	}

	// Clear THIS process's stale in-memory session state so the NEXT Acquire misses
	// the Load and takes the create path (boot lazy-recreate). RecoverOnBoot runs at
	// boot BEFORE the reaper does meaningful work and before any Acquire, so a plain
	// capMu-guarded map clear is safe (boot-only invariant: no in-flight Acquire). No
	// docker.stop/rm (the stray sweep below rm's every aura-sandbox-sess-* container)
	// and no re-MarkTerminated (the row loop already did). count clamps at 0.
	m.capMu.Lock()
	m.sessions.Range(func(k, _ any) bool {
		m.sessions.Delete(k)
		if m.count > 0 {
			m.count--
		}
		m.endpoint().Unregister(k.(string))
		return true
	})
	m.capMu.Unlock()

	stray, err := m.docker.listStray(ctx)
	if err != nil {
		slog.Warn("session boot recovery: stray container list failed", "err", err)
		return nil
	}
	for _, id := range stray {
		if rmErr := m.docker.remove(ctx, id); rmErr != nil {
			slog.Warn("session boot recovery: remove stray container failed", "container", id, "err", rmErr)
		}
	}
	return nil
}
