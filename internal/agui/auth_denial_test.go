package agui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// denialCall is one recorded RecordDenial invocation, captured by fakeDenialRecorder for
// assertion.
type denialCall struct {
	identityID, capability, route, cause string
}

// fakeDenialRecorder is the injected capabilityDenialRecorder for the RequireCapability
// denial-recording tests: it captures every call in order and can be made to fail, so a
// test can assert both "the write was attempted" and "a write failure never changes the
// refusal" (T-02-20).
type fakeDenialRecorder struct {
	mu    sync.Mutex
	calls []denialCall
	err   error
}

func (f *fakeDenialRecorder) RecordDenial(_ context.Context, identityID, capability, route, cause string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, denialCall{identityID, capability, route, cause})
	return f.err
}

func (f *fakeDenialRecorder) snapshot() []denialCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]denialCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// TestRequireCapabilityRecordsDenial_MissingPrincipal: a request with no principal is
// refused 403 and the recorder receives exactly one denial whose identity field is the
// empty-principal marker.
func TestRequireCapabilityRecordsDenial_MissingPrincipal(t *testing.T) {
	rec := &fakeDenialRecorder{}
	deps := testDeps("operator-secret")
	deps.DenialRecorder = rec
	next, hit := nextRecorder()

	req := httptest.NewRequest(http.MethodPost, "/agent/run", nil) // no principal on ctx
	w := httptest.NewRecorder()
	RequireCapability(next, deps, "agent.run").ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *hit {
		t.Fatal("next reached with no principal")
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("denial calls = %d, want exactly 1: %+v", len(calls), calls)
	}
	if calls[0].identityID != NoPrincipalIdentityID {
		t.Fatalf("identity = %q, want the no-principal sentinel %q", calls[0].identityID, NoPrincipalIdentityID)
	}
	if calls[0].capability != "agent.run" {
		t.Fatalf("capability = %q, want agent.run", calls[0].capability)
	}
	if calls[0].cause != DenialCauseNoPrincipal {
		t.Fatalf("cause = %q, want %q", calls[0].cause, DenialCauseNoPrincipal)
	}
}

// TestRequireCapabilityRecordsDenial_StoreError: HasCapability errors; the response is
// still 403 and the recorded cause distinguishes a store outage from a plain refusal.
func TestRequireCapabilityRecordsDenial_StoreError(t *testing.T) {
	rec := &fakeDenialRecorder{}
	deps := testDeps("operator-secret")
	deps.DenialRecorder = rec
	deps.Identities = &fakeIdentities{hasErr: errFakeNotFound}
	next, hit := nextRecorder()

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/agent/run", nil), testLocalID)
	w := httptest.NewRecorder()
	RequireCapability(next, deps, "agent.run").ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *hit {
		t.Fatal("next reached despite a store error")
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("denial calls = %d, want exactly 1: %+v", len(calls), calls)
	}
	if calls[0].cause != DenialCauseStoreError {
		t.Fatalf("cause = %q, want %q", calls[0].cause, DenialCauseStoreError)
	}
	if calls[0].identityID != testLocalID {
		t.Fatalf("identity = %q, want %q", calls[0].identityID, testLocalID)
	}
}

// TestRequireCapabilityRecordsDenial_NotHeld: a principal that does not hold the
// capability is refused and one denial is recorded with the not-held cause — distinct
// from the store-error cause above.
func TestRequireCapabilityRecordsDenial_NotHeld(t *testing.T) {
	rec := &fakeDenialRecorder{}
	deps := testDeps("operator-secret")
	deps.DenialRecorder = rec
	next, hit := nextRecorder()

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities", nil), testLocalID)
	w := httptest.NewRecorder()
	RequireCapability(next, deps, "governance.write").ServeHTTP(w, req) // testLocalID holds only agent.run

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *hit {
		t.Fatal("next reached without the capability")
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("denial calls = %d, want exactly 1: %+v", len(calls), calls)
	}
	if calls[0].cause != DenialCauseNotHeld {
		t.Fatalf("cause = %q, want %q", calls[0].cause, DenialCauseNotHeld)
	}
}

// TestRequireCapabilityRecordsNothingOnSuccess: a permitted request records zero denials —
// the ledger stays meaningful (only refusals appear in it).
func TestRequireCapabilityRecordsNothingOnSuccess(t *testing.T) {
	rec := &fakeDenialRecorder{}
	deps := testDeps("operator-secret")
	deps.DenialRecorder = rec
	next, hit := nextRecorder()

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/agent/run", nil), testLocalID)
	w := httptest.NewRecorder()
	RequireCapability(next, deps, "agent.run").ServeHTTP(w, req)

	if w.Code != http.StatusOK || !*hit {
		t.Fatalf("status = %d hit = %v, want 200/true on a permitted request", w.Code, *hit)
	}
	if calls := rec.snapshot(); len(calls) != 0 {
		t.Fatalf("denial calls = %d, want 0 on a permitted request: %+v", len(calls), calls)
	}
}

// TestRequireCapabilityStillDeniesWhenRecorderFails: a recorder that errors leaves the
// response at 403 — never 500, never 200 — and the failure is logged at warn naming the
// capability, so "we logged it" is a fact rather than a claim.
func TestRequireCapabilityStillDeniesWhenRecorderFails(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	rec := &fakeDenialRecorder{err: errors.New("db unavailable")}
	deps := testDeps("operator-secret")
	deps.DenialRecorder = rec
	next, hit := nextRecorder()

	req := httptest.NewRequest(http.MethodPost, "/agent/run", nil) // no principal -> no_principal branch
	w := httptest.NewRecorder()
	RequireCapability(next, deps, "agent.run").ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 even when the recorder fails", w.Code)
	}
	if *hit {
		t.Fatal("next reached despite a denied request")
	}
	logged := buf.String()
	if !strings.Contains(logged, "WARN") {
		t.Fatalf("recorder failure was not logged at warn: %s", logged)
	}
	if !strings.Contains(logged, "agent.run") {
		t.Fatalf("recorder-failure log line does not name the capability: %s", logged)
	}
}

// TestRequireCapabilityWithNilRecorder: an unwired recorder (nil, matching how AuthDeps
// looks before the composition root calls SetAuditStore-style wiring) does not panic and
// does not change the refusal.
func TestRequireCapabilityWithNilRecorder(t *testing.T) {
	deps := testDeps("operator-secret") // DenialRecorder left nil
	next, hit := nextRecorder()

	req := httptest.NewRequest(http.MethodPost, "/agent/run", nil) // no principal
	w := httptest.NewRecorder()
	RequireCapability(next, deps, "agent.run").ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *hit {
		t.Fatal("next reached with no principal")
	}
}

// TestDenialRecordsRoutePatternNotRawPath: a denial on a route with a path parameter
// records the MATCHED PATTERN, not the concrete request path — so a thousand denials
// against a thousand ids read back as one route in the feed. Dispatched through a real
// http.ServeMux (like every production mount in cmd/aura), because r.Pattern is stamped
// by the mux at match time, before RequireCapability's handler runs.
func TestDenialRecordsRoutePatternNotRawPath(t *testing.T) {
	rec := &fakeDenialRecorder{}
	deps := testDeps("operator-secret")
	deps.DenialRecorder = rec

	const pattern = "POST /api/admin/identities/{id}/capabilities"
	mux := http.NewServeMux()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle(pattern, RequireCapability(next, deps, "governance.write"))

	for _, id := range []string{"aaaaaaaa-0000-0000-0000-000000000001", "bbbbbbbb-0000-0000-0000-000000000002"} {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+id+"/capabilities", nil), testLocalID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status for id=%s = %d, want 403 (testLocalID does not hold governance.write)", id, w.Code)
		}
	}

	calls := rec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("denial calls = %d, want 2 (one per id): %+v", len(calls), calls)
	}
	for _, c := range calls {
		if c.route != pattern {
			t.Fatalf("route = %q, want the matched pattern %q, not the raw path", c.route, pattern)
		}
	}
	if calls[0].route != calls[1].route {
		t.Fatalf("two denials against two different ids must read as ONE route: %q vs %q", calls[0].route, calls[1].route)
	}
}
