package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aura/aura/internal/auth"
)

// fakeApprover records side effects so we can assert ApproveAccess /
// DenyAccess were called on the right user IDs and that errors propagate.
type fakeApprover struct {
	mu       sync.Mutex
	approved []string
	denied   []string
	failWith error
}

func (f *fakeApprover) ApproveAccess(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.approved = append(f.approved, userID)
	return nil
}

func (f *fakeApprover) DenyAccess(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.denied = append(f.denied, userID)
	return nil
}

func newPendingTestEnv(t *testing.T) (*authedTestEnv, *fakeApprover) {
	t.Helper()
	base := newTestEnv(t)
	authStore, err := auth.OpenStore(filepath.Join(base.dir, "auth.db"))
	if err != nil {
		t.Fatalf("auth store: %v", err)
	}
	t.Cleanup(func() { authStore.Close() })

	allowed := map[string]bool{}
	approver := &fakeApprover{}
	router := NewRouter(Deps{
		Wiki:            base.wiki,
		Sources:         base.sources,
		Scheduler:       base.sched,
		Auth:            authStore,
		Allowlist:       func(uid string) bool { return allowed[uid] },
		PendingApprover: approver,
	})
	return &authedTestEnv{
		testEnv:   base,
		authStore: authStore,
		allowed:   allowed,
		router:    router,
	}, approver
}

func TestPending_ListReturnsRows(t *testing.T) {
	e, _ := newPendingTestEnv(t)
	tok := e.issue(t, "alice")

	if _, err := e.authStore.RequestAccess(context.Background(), "1234567", "guest"); err != nil {
		t.Fatal(err)
	}

	rr := e.doAuthed("GET", "/pending-users", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	var got []PendingUserSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UserID != "1234567" || got[0].Username != "guest" {
		t.Errorf("got = %+v, want one row {1234567, guest}", got)
	}
}

func TestPending_ListRequiresAuth(t *testing.T) {
	e, _ := newPendingTestEnv(t)
	rr := e.doAuthed("GET", "/pending-users", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestPending_Approve(t *testing.T) {
	e, fk := newPendingTestEnv(t)
	tok := e.issue(t, "alice")

	if _, err := e.authStore.RequestAccess(context.Background(), "1234567", "guest"); err != nil {
		t.Fatal(err)
	}

	rr := e.doAuthed("POST", "/pending-users/1234567/approve", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if len(fk.approved) != 1 || fk.approved[0] != "1234567" {
		t.Errorf("approved = %v, want [1234567]", fk.approved)
	}
}

func TestPending_Deny(t *testing.T) {
	e, fk := newPendingTestEnv(t)
	tok := e.issue(t, "alice")

	if _, err := e.authStore.RequestAccess(context.Background(), "1234567", "guest"); err != nil {
		t.Fatal(err)
	}

	rr := e.doAuthed("POST", "/pending-users/1234567/deny", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if len(fk.denied) != 1 || fk.denied[0] != "1234567" {
		t.Errorf("denied = %v, want [1234567]", fk.denied)
	}
}

func TestPending_ApproveRequiresAuth(t *testing.T) {
	e, _ := newPendingTestEnv(t)
	rr := e.doAuthed("POST", "/pending-users/1234567/approve", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestPending_ApproveRejectsBadID(t *testing.T) {
	e, _ := newPendingTestEnv(t)
	tok := e.issue(t, "alice")
	rr := e.doAuthed("POST", "/pending-users/not-a-number/approve", tok, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rr.Code)
	}
}

func TestPending_ApproveSurfacesNotFound(t *testing.T) {
	e, fk := newPendingTestEnv(t)
	tok := e.issue(t, "alice")
	fk.failWith = auth.ErrInvalid

	rr := e.doAuthed("POST", "/pending-users/1234567/approve", tok, nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rr.Code)
	}
}

func TestPending_ApproveSurfacesGenericError(t *testing.T) {
	e, fk := newPendingTestEnv(t)
	tok := e.issue(t, "alice")
	fk.failWith = errors.New("send: telegram down")

	rr := e.doAuthed("POST", "/pending-users/1234567/approve", tok, nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", rr.Code)
	}
}
