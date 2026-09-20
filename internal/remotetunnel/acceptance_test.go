package remotetunnel

import (
	"errors"
	"testing"
	"time"
)

func TestExternalAcceptancePersistsAndSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.store.state.LastError = "prior diagnostic"
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatal(err)
	}
	if !h.store.state.ObservedHealthy || h.store.state.Phase != PhaseHealthy || h.store.state.LastError != "" {
		t.Fatal("acceptance not persisted")
	}
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatalf("repeat acceptance: %v", err)
	}
	h.r = h.newReconciler()
	h.r.acceptance = PersistedAcceptance{Store: h.store}
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Phase != PhaseHealthy {
		t.Fatal("restart lost accepted generation")
	}
	h.cloud.tunnel.Status = "down"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.ObservedHealthy {
		t.Fatal("disconnect retained acceptance")
	}
	h.cloud.tunnel.Status = "healthy"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.ObservedHealthy {
		t.Fatal("reconnect bypassed new external acceptance")
	}
}

func TestExternalAcceptanceRejectsStaleDisabledUnownedAndUnhealthy(t *testing.T) {
	for _, mode := range []string{"stale", "disabled", "unowned", "unhealthy", "error", "terminal", "missing", "deleted", "load", "persist"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			generation := h.store.state.Generation
			want := ErrConfiguration
			switch mode {
			case "stale":
				generation--
				want = ErrStaleGeneration
			case "disabled":
				h.store.state.Desired.Enabled = false
			case "unowned":
				h.cloud.tunnel.Name = "foreign"
				want = ErrOwnershipConflict
			case "unhealthy":
				h.store.state.ObservedHealthy = true
				h.store.state.Phase = PhaseHealthy
				h.cloud.tunnel.Status = "down"
			case "error":
				h.cloud.failure = errors.New("unavailable")
				want = h.cloud.failure
			case "terminal":
				h.store.state.Phase = PhaseError
			case "missing":
				h.store.state.Resources.TunnelID = ""
				h.cloud.allowUnpersisted = true
			case "deleted":
				now := time.Now()
				h.cloud.tunnel.DeletedAt = &now
				want = ErrOwnershipConflict
			case "load":
				h.store.loadError = errors.New("load failed")
				want = h.store.loadError
			case "persist":
				h.cloud.tunnel.Status = "down"
				h.store.failAdvance = errors.New("commit failed")
				want = h.store.failAdvance
			}
			if err := h.r.AcceptExternal(t.Context(), generation); !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
			if h.store.state.ObservedHealthy {
				t.Fatal("invalid acceptance persisted")
			}
			if mode == "unhealthy" && h.store.state.Phase != PhaseConnecting {
				t.Fatal("disconnected connector still reported healthy")
			}
		})
	}
}

func TestPersistedAcceptanceRejectsOtherGenerationAndStoreFailure(t *testing.T) {
	s := &memoryState{state: State{Generation: 2, ObservedHealthy: true}}
	probe := PersistedAcceptance{Store: s}
	if ok, err := probe.Ready(t.Context(), State{Generation: 1}); ok || err != nil {
		t.Fatalf("stale evidence=%v %v", ok, err)
	}
	s.loadError = errors.New("database unavailable")
	if ok, err := probe.Ready(t.Context(), State{Generation: 2}); ok || !errors.Is(err, s.loadError) {
		t.Fatalf("failed read=%v %v", ok, err)
	}
}
