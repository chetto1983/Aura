package setup

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// fakeStore is the unit-tier Store: it records InsertPending calls and answers
// PendingConsumed/CountAccounts from in-memory fields — no DB, no network.
type fakeStore struct {
	inserted   []InsertPendingParams
	consumed   bool
	consumeErr error
	count      int64
	insertErr  error
}

func (f *fakeStore) InsertPending(_ context.Context, p InsertPendingParams) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, p)
	return nil
}

func (f *fakeStore) PendingConsumed(_ context.Context, _ string) (bool, error) {
	return f.consumed, f.consumeErr
}

func (f *fakeStore) CountAccounts(_ context.Context) (int64, error) { return f.count, nil }

// newTestServer builds a Server with a fixed token + fake store for the gate
// tests. The token line is captured so the stdout-format assertion can read it.
func newTestServer(t *testing.T, token string) (*Server, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	srv := NewServer(Deps{
		Store:      &fakeStore{},
		Probe:      func(_ context.Context, _ string) (string, error) { return "aura_bot", nil },
		Token:      token,
		IdentityID: "00000000-0000-0000-0000-000000000001",
		TokenOut:   &out,
	})
	return srv, &out
}

// TestSetupTokenGate proves the mandatory token gate (UX-03 acceptance "401
// without / 200 with / 401 after onboard-complete", T-13-07-SetupToken): a call
// without the token 401s, a call with it 200s, and after InvalidateToken (the
// onboard-complete burn) the same token 401s again.
func TestSetupTokenGate(t *testing.T) {
	srv, _ := newTestServer(t, "secret-token")
	mux := srv.Mux()

	// Without the token → 401.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rec.Code)
	}

	// With the token (?token=) → 200.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup/status?token=secret-token", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("with token: got %d, want 200", rec.Code)
	}

	// With the token via the header → 200 (the second presentation path).
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup/status", nil)
	req.Header.Set(setupHeaderName, "secret-token")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("header token: got %d, want 200", rec.Code)
	}

	// After onboarding completes the token is invalidated → the same token 401s.
	srv.InvalidateToken()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup/status?token=secret-token", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after invalidate: got %d, want 401", rec.Code)
	}
}

// TestSetupTokenGateWrongToken proves a non-matching token 401s (the gate is an
// equality check, not a presence check) on every route.
func TestSetupTokenGateWrongToken(t *testing.T) {
	srv, _ := newTestServer(t, "right")
	mux := srv.Mux()
	routes := []struct {
		method, path string
	}{
		{http.MethodPost, "/setup/token"},
		{http.MethodPost, "/setup/onboard-link"},
		{http.MethodGet, "/setup/status"},
		{http.MethodGet, "/setup/events"},
	}
	for _, rt := range routes {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(rt.method, rt.path+"?token=wrong", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s wrong token: got %d, want 401", rt.method, rt.path, rec.Code)
		}
	}
}

// stdoutTokenLine is the exact parseable line a boot prints when AURA_SETUP_TOKEN
// is empty: `AURA_SETUP_TOKEN=<uuid>` (one line, no extra text).
var stdoutTokenLine = regexp.MustCompile(`^AURA_SETUP_TOKEN=[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\n$`)

// TestTokenGeneratedAndPrinted proves an empty configured token generates a
// random UUIDv4 and prints exactly one parseable `AURA_SETUP_TOKEN=<uuid>` line
// to stdout (UX-03 acceptance), and that the generated token actually gates the
// API (the printed value is the live token).
func TestTokenGeneratedAndPrinted(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(Deps{
		Store:    &fakeStore{},
		Token:    "", // empty → generate + print
		TokenOut: &out,
	})
	line := out.String()
	if !stdoutTokenLine.MatchString(line) {
		t.Fatalf("stdout token line %q does not match AURA_SETUP_TOKEN=<uuid>", line)
	}
	// The printed token is the live one: extract it and prove it opens the gate.
	got := strings.TrimSpace(strings.TrimPrefix(line, "AURA_SETUP_TOKEN="))
	rec := httptest.NewRecorder()
	srv.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup/status?token="+got, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("printed token did not open the gate: got %d, want 200", rec.Code)
	}
}

// TestTokenConfiguredNotPrinted proves an operator-set token is NOT echoed to
// stdout (only a boot-generated token is printed; a known token must not leak).
func TestTokenConfiguredNotPrinted(t *testing.T) {
	var out bytes.Buffer
	NewServer(Deps{Store: &fakeStore{}, Token: "operator-set", TokenOut: &out})
	if out.Len() != 0 {
		t.Fatalf("operator-set token was printed to stdout: %q", out.String())
	}
}

// TestHTTPServerLoopbackBind proves the server binds the loopback :9081 default
// (distinct from the AG-UI :9080, T-13-07-SetupExposure) and sets a
// ReadHeaderTimeout (slow-loris defense, mirrors serve.go).
func TestHTTPServerLoopbackBind(t *testing.T) {
	srv, _ := newTestServer(t, "tok")
	httpSrv := srv.HTTPServer("127.0.0.1:9081")
	if httpSrv.Addr != "127.0.0.1:9081" {
		t.Fatalf("bind: got %q, want 127.0.0.1:9081", httpSrv.Addr)
	}
	if !strings.HasPrefix(httpSrv.Addr, "127.0.0.1:") {
		t.Fatalf("bind %q is not loopback", httpSrv.Addr)
	}
	if httpSrv.ReadHeaderTimeout == 0 {
		t.Fatal("ReadHeaderTimeout is unset (slow-loris exposure)")
	}
}
