package remotetunnel

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// The status field is deliberately sanitised: it reaches a browser. The operator log is the
// only place the cause survives, and losing it there cost a live appliance an hour of
// bisecting a phase=error with no stated reason (2026-09-22, the gateway-posture deadlock).
func TestReconcileFailureLogsTheCauseButKeepsStatusSanitised(t *testing.T) {
	const cause = "cloudflare refused: posture ownership conflict"
	h := newHarness(t)
	var log bytes.Buffer
	h.r = New(h.store, h.cloud, h.members, h.projection, slog.New(slog.NewTextHandler(&log, nil)), WithCredentials(h.credentials))
	h.cloud.failure = errors.New(cause)

	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("injected failure did not surface")
	}
	if !strings.Contains(log.String(), cause) {
		t.Fatalf("operator log dropped the cause: %s", log.String())
	}
	state, err := h.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseError {
		t.Fatalf("phase = %q, want %q", state.Phase, PhaseError)
	}
	if strings.Contains(state.LastError, cause) {
		t.Fatalf("sanitised status leaked the cause to the UI: %q", state.LastError)
	}
}
