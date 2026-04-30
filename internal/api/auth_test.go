package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/auth"
)

// authedTestEnv is testEnv plus an auth.Store and an Allowlist predicate.
// Used only for tests that exercise RequireBearer; the bare testEnv keeps
// Auth=nil so the read-side tests (router_test.go, writes_test.go) don't
// have to mint a token to drive every assertion.
type authedTestEnv struct {
	*testEnv
	authStore *auth.Store
	allowed   map[string]bool
	router    http.Handler
}

func newAuthedTestEnv(t *testing.T) *authedTestEnv {
	t.Helper()
	base := newTestEnv(t)
	authStore, err := auth.OpenStore(filepath.Join(base.dir, "auth.db"))
	if err != nil {
		t.Fatalf("auth store: %v", err)
	}
	t.Cleanup(func() { authStore.Close() })

	allowed := map[string]bool{}
	router := NewRouter(Deps{
		Wiki:      base.wiki,
		Sources:   base.sources,
		Scheduler: base.sched,
		Auth:      authStore,
		Allowlist: func(uid string) bool { return allowed[uid] },
	})
	return &authedTestEnv{
		testEnv:   base,
		authStore: authStore,
		allowed:   allowed,
		router:    router,
	}
}

func (e *authedTestEnv) issue(t *testing.T, userID string) string {
	t.Helper()
	e.allowed[userID] = true
	tok, err := e.authStore.Issue(context.Background(), userID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return tok
}

func (e *authedTestEnv) doAuthed(method, path, token string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytesReaderOrNil(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// bytesReaderOrNil mirrors what httptest.NewRequest does internally — keep
// the call shape simple for the auth tests that mostly send GETs.
func bytesReaderOrNil(b []byte) *bytesReaderWrapper {
	if b == nil {
		return nil
	}
	return &bytesReaderWrapper{b: b}
}

type bytesReaderWrapper struct {
	b []byte
	i int
}

func (r *bytesReaderWrapper) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, errEOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

var errEOF = &eofErr{}

type eofErr struct{}

func (*eofErr) Error() string { return "EOF" }

// ---- Router-level RequireBearer wiring ----------------------------------

func TestAuth_RejectsUnauthedRead(t *testing.T) {
	e := newAuthedTestEnv(t)
	rr := e.doAuthed("GET", "/health", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestAuth_AcceptsValidToken(t *testing.T) {
	e := newAuthedTestEnv(t)
	tok := e.issue(t, "alice")
	rr := e.doAuthed("GET", "/health", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
}

func TestAuth_RejectsRevokedToken(t *testing.T) {
	e := newAuthedTestEnv(t)
	tok := e.issue(t, "alice")
	if err := e.authStore.Revoke(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	rr := e.doAuthed("GET", "/health", tok, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestAuth_RejectsDeAllowlistedUser(t *testing.T) {
	e := newAuthedTestEnv(t)
	tok := e.issue(t, "alice")
	delete(e.allowed, "alice") // user removed from allowlist after issuance
	rr := e.doAuthed("GET", "/health", tok, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestAuth_WriteEndpointsGated(t *testing.T) {
	e := newAuthedTestEnv(t)
	cases := []struct {
		method, path string
	}{
		{"POST", "/wiki/index/rebuild"},
		{"POST", "/sources/src_0123456789abcdef/ingest"},
		{"POST", "/tasks/x/cancel"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rr := e.doAuthed(c.method, c.path, "", nil)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("status %d, want 401", rr.Code)
			}
		})
	}
}

// ---- /auth/whoami -------------------------------------------------------

func TestAuth_Whoami(t *testing.T) {
	e := newAuthedTestEnv(t)
	tok := e.issue(t, "alice")
	rr := e.doAuthed("GET", "/auth/whoami", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["user_id"] != "alice" {
		t.Errorf("user_id = %q, want alice", got["user_id"])
	}
}

// ---- /auth/logout -------------------------------------------------------

func TestAuth_LogoutRevokesToken(t *testing.T) {
	e := newAuthedTestEnv(t)
	tok := e.issue(t, "alice")

	// Pre-flight: token works.
	if rr := e.doAuthed("GET", "/health", tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("pre-logout health status %d", rr.Code)
	}

	// Logout.
	if rr := e.doAuthed("POST", "/auth/logout", tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("logout status %d, body=%s", rr.Code, rr.Body)
	}

	// Token is now revoked → 401.
	if rr := e.doAuthed("GET", "/health", tok, nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("post-logout health status %d, want 401", rr.Code)
	}
}

func TestAuth_LogoutRequiresAuth(t *testing.T) {
	e := newAuthedTestEnv(t)
	rr := e.doAuthed("POST", "/auth/logout", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}
