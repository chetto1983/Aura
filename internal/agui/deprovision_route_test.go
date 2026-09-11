package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
)

// fakeIdentityRemover records Deactivate/PurgeOne calls in order, safe for concurrent
// use so the concurrency test can launch two goroutines against the same fake.
type fakeIdentityRemover struct {
	mu       sync.Mutex
	calls    []string
	deactErr error
	purgeErr error
	delay    time.Duration
	// afterDeactivate runs once Deactivate is done, to act out a client leaving mid-saga.
	afterDeactivate func()
	purgeCtxErr     error
}

func (f *fakeIdentityRemover) Deactivate(_ context.Context, identityID string) error {
	f.mu.Lock()
	f.calls = append(f.calls, "deactivate:"+identityID)
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.afterDeactivate != nil {
		f.afterDeactivate()
	}
	return f.deactErr
}

func (f *fakeIdentityRemover) PurgeOne(ctx context.Context, identityID string) error {
	f.mu.Lock()
	f.calls = append(f.calls, "purge:"+identityID)
	f.purgeCtxErr = ctx.Err()
	f.mu.Unlock()
	return f.purgeErr
}

// TestRemoveIdentityOutlivesTheRequest proves the saga finishes when the caller goes away
// between its legs. Measured 2026-09-11: a browser closed mid-removal left the identity
// deactivated but not purged, and its OpenRouter key live, because the purge ran on the
// request's cancelled context.
func TestRemoveIdentityOutlivesTheRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fakeIdentityRemover{afterDeactivate: cancel}
	s := &Server{}
	s.SetIdentityRemover(fake)

	s.handleRemoveIdentity(httptest.NewRecorder(), removeRequest().WithContext(ctx))
	if fake.callCount() != 2 {
		t.Fatalf("calls = %v, want deactivate then purge", fake.calls)
	}
	if fake.purgeCtxErr != nil {
		t.Fatalf("purge ran on a cancelled context (%v): the saga must not end with the request", fake.purgeCtxErr)
	}
}

func (f *fakeIdentityRemover) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func removeRequest() *http.Request {
	r := httptest.NewRequest(http.MethodDelete, "/api/admin/identities/"+testLocalID, nil)
	r.SetPathValue("id", testLocalID)
	return r
}

// TestRemoveIdentityRunsFullSaga proves a DELETE runs deactivate then purge, in that
// order, for one request -- not a row mark, and not a grace-window deferral.
func TestRemoveIdentityRunsFullSaga(t *testing.T) {
	fake := &fakeIdentityRemover{}
	s := &Server{}
	s.SetIdentityRemover(fake)

	rec := httptest.NewRecorder()
	s.handleRemoveIdentity(rec, removeRequest())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	want := []string{"deactivate:" + testLocalID, "purge:" + testLocalID}
	if len(fake.calls) != 2 || fake.calls[0] != want[0] || fake.calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v (deactivate before purge)", fake.calls, want)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["status"] != "removed" {
		t.Errorf("status field = %v, want \"removed\"", out["status"])
	}
}

// TestRemoveIdentityRequiresIdentityDelete proves the capability the composition root
// (cmd/aura/serve_webui_musr.go) mounts this route behind is identity.delete: a caller
// holding governance.write but NOT identity.delete is refused 403 by
// RequireCapability when wrapped with identity.CapIdentityDelete -- the same
// capability string the mount uses. This is the test that catches copying the
// neighbouring routes' governance.write gate, which under D-01 every identity now
// passes.
func TestRemoveIdentityRequiresIdentityDelete(t *testing.T) {
	fake := &fakeIdentityRemover{}
	s := &Server{}
	s.SetIdentityRemover(fake)

	deps := AuthDeps{
		Secret:           "test-secret",
		SecretConfigured: true,
		Identities: &fakeIdentities{
			known:        map[string]Identity{testLocalID: {ID: testLocalID, Name: "member", Kind: "user"}},
			capabilities: map[string]bool{testLocalID + "|governance.write": true},
		},
	}
	gated := RequireCapability(http.HandlerFunc(s.handleRemoveIdentity), deps, identity.CapIdentityDelete)

	req := withPrincipal(removeRequest(), testLocalID)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (governance.write must not satisfy identity.delete)", rec.Code)
	}
	if fake.callCount() != 0 {
		t.Errorf("saga must never run when the gate refuses: calls = %v", fake.calls)
	}
}

// TestRemoveIdentityRefusesLastAdminSelf proves the route refuses through the same
// policy the saga enforces -- the route cannot be a way around it.
func TestRemoveIdentityRefusesLastAdminSelf(t *testing.T) {
	fake := &fakeIdentityRemover{deactErr: identity.ErrLastAdministrator}
	s := &Server{}
	s.SetIdentityRemover(fake)

	rec := httptest.NewRecorder()
	s.handleRemoveIdentity(rec, removeRequest())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "last administrative identity") {
		t.Errorf("response must name the refusal, got %s", rec.Body.String())
	}
}

// TestRemoveIdentityIsIdempotent proves a second DELETE for an already-removed
// identity returns a success-shaped response rather than a 500, matching the saga's
// own convergence contract (Deactivate/PurgeOne are idempotent by construction).
func TestRemoveIdentityIsIdempotent(t *testing.T) {
	fake := &fakeIdentityRemover{}
	s := &Server{}
	s.SetIdentityRemover(fake)

	for i := range 2 {
		rec := httptest.NewRecorder()
		s.handleRemoveIdentity(rec, removeRequest())
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200: %s", i+1, rec.Code, rec.Body.String())
		}
	}
}

// TestRemoveIdentityRouteIsIdempotencyRegistered is a map lookup: the route pattern is
// in httpMutationRoutes.
func TestRemoveIdentityRouteIsIdempotencyRegistered(t *testing.T) {
	meta, ok := httpMutationRoutes["DELETE /api/admin/identities/{id}"]
	if !ok {
		t.Fatal("DELETE /api/admin/identities/{id} is absent from httpMutationRoutes")
	}
	if meta.Normalize == "" {
		t.Errorf("removal route metadata incomplete: %+v", meta)
	}
}

// TestRemoveIdentityUnwiredReturns503 proves an unwired port answers 503 rather than
// panicking on a nil port, matching the SetAuditStore precedent.
func TestRemoveIdentityUnwiredReturns503(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleRemoveIdentity(rec, removeRequest())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestRemoveIdentityConcurrentRequestsRunOneSaga proves two simultaneous requests for
// the same identity result in ONE saga run; the loser observes the first run's
// outcome rather than starting a second, partial teardown.
func TestRemoveIdentityConcurrentRequestsRunOneSaga(t *testing.T) {
	fake := &fakeIdentityRemover{delay: 50 * time.Millisecond}
	s := &Server{}
	s.SetIdentityRemover(fake)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			s.handleRemoveIdentity(rec, removeRequest())
			codes[idx] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("goroutine %d: status = %d, want 200", i, code)
		}
	}
	if got := fake.callCount(); got != 2 {
		t.Fatalf("total calls = %d, want exactly 2 (one Deactivate + one Purge -- ONE saga run, not two)", got)
	}
}
