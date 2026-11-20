package agui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
)

// connect_pim_account_api_test.go covers the relay of discovery, verification and the partial
// provider-config change (connect_pim_account_api.go) against an httptest sidecar: the long client,
// the 422 pass-through, the owned keys dropped from a PATCH, the private-hosts header built from the
// caller's capability alone, the per-identity limit and the watch woken only by a repair that held.

// pimSidecarCall is one request the recording sidecar answered.
type pimSidecarCall struct {
	method, path, body string
	privateHosts       []string
}

// recordingPIM answers every request with one status and body after an optional delay.
type recordingPIM struct {
	mu     sync.Mutex
	calls  []pimSidecarCall
	status int
	answer string
	delay  time.Duration
}

func (p *recordingPIM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.calls = append(p.calls, pimSidecarCall{
		method: r.Method, path: r.URL.EscapedPath(), body: string(body),
		privateHosts: r.Header.Values(pimAllowPrivateHostsHeader),
	})
	p.mu.Unlock()
	time.Sleep(p.delay)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(p.status)
	_, _ = io.WriteString(w, p.answer)
}

func (p *recordingPIM) recorded() []pimSidecarCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]pimSidecarCall(nil), p.calls...)
}

// pimAccountServer wires a Server to the recording sidecar; caps are the caller's capabilities.
func pimAccountServer(t *testing.T, p *recordingPIM, caps ...string) *Server {
	t.Helper()
	sidecar := httptest.NewServer(p)
	t.Cleanup(sidecar.Close)
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecar.URL, "", staticMCPAccessTokenProvider{token: fakePIMToken})
	s.SetIdentityAdmin(&fakeIdentityAdmin{caps: map[string][]string{fakePIMIdentity: caps}})
	return s
}

func pimAccountCall(s *Server, method, target, body string, header ...string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, withPrincipal(req, fakePIMIdentity))
	return rec
}

const imapCreateBody = `{"id":"work","displayName":"Work","provider":"imap","providerConfig":{"imapHost":"imap.example.com","username":"someone@example.com","password":"s3cret"}}`

func TestDiscoverForwardsTheAddress(t *testing.T) {
	p := &recordingPIM{status: http.StatusOK, answer: `{"kind":"imap","source":"ispdb","imapHost":"imap.example.com"}`}
	s := pimAccountServer(t, p, identity.CapIdentityCreate)

	rec := pimAccountCall(s, http.MethodPost, "/api/connect/pim/discover", `{"email":"someone@example.com"}`)

	if rec.Code != http.StatusOK || rec.Body.String() != p.answer {
		t.Fatalf("discover = %d %q, want 200 with the sidecar's answer", rec.Code, rec.Body.String())
	}
	calls := p.recorded()
	if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != "/admin/discover" {
		t.Fatalf("sidecar calls = %+v, want one POST /admin/discover", calls)
	}
	if calls[0].body != `{"email":"someone@example.com"}` {
		t.Fatalf("forwarded body = %q, want the address", calls[0].body)
	}
	if len(calls[0].privateHosts) != 0 {
		t.Fatalf("discover carried %s = %v; discovery never reaches a private host", pimAllowPrivateHostsHeader, calls[0].privateHosts)
	}
}

func TestCreateUsesTheLongClient(t *testing.T) {
	short, long := pimClient, pimVerifyClient
	pimClient = &http.Client{Timeout: 20 * time.Millisecond}
	pimVerifyClient = &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(func() { pimClient, pimVerifyClient = short, long })
	p := &recordingPIM{status: http.StatusCreated, answer: `{"id":"work"}`, delay: 100 * time.Millisecond}
	s := pimAccountServer(t, p)

	rec := pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts", imapCreateBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create through a sidecar slower than pimClient = %d %q, want 201", rec.Code, rec.Body.String())
	}
}

func TestVerifyPassesA422Through(t *testing.T) {
	answer := `{"error":"verify_failed","step":"imap","reason":"auth_failed"}`
	p := &recordingPIM{status: http.StatusUnprocessableEntity, answer: answer}
	s := pimAccountServer(t, p)

	rec := pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/work/verify", "")

	if rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != answer {
		t.Fatalf("verify = %d %q, want 422 %q byte for byte", rec.Code, rec.Body.String(), answer)
	}
	calls := p.recorded()
	if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != "/admin/accounts/work/verify" {
		t.Fatalf("sidecar calls = %+v, want one POST /admin/accounts/work/verify", calls)
	}
}

func TestPatchForwardsAsPatch_WithoutOwnedKeys(t *testing.T) {
	p := &recordingPIM{status: http.StatusOK, answer: `{"id":"work"}`}
	s := pimAccountServer(t, p)

	rec := pimAccountCall(s, http.MethodPatch, "/api/connect/pim/accounts/work/provider-config",
		`{"providerConfig":{"password":"n3w","clientId":"browser-app","ClientSecret":"x","TENANTID":"t"}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %q, want 200", rec.Code, rec.Body.String())
	}
	calls := p.recorded()
	if len(calls) != 1 || calls[0].method != http.MethodPatch || calls[0].path != "/admin/accounts/work/provider-config" {
		t.Fatalf("sidecar calls = %+v, want one PATCH /admin/accounts/work/provider-config", calls)
	}
	var forwarded map[string]map[string]string
	if err := json.Unmarshal([]byte(calls[0].body), &forwarded); err != nil {
		t.Fatalf("forwarded body %q: %v", calls[0].body, err)
	}
	want := map[string]string{"password": "n3w"}
	if got := forwarded["providerConfig"]; len(got) != len(want) || got["password"] != want["password"] {
		t.Fatalf("forwarded providerConfig = %v, want only %v", got, want)
	}
}

func TestTheAdminSendsThePrivateHostsHeader_AMemberDoesNot(t *testing.T) {
	routes := []struct{ method, target, body string }{
		{http.MethodPost, "/api/connect/pim/accounts", imapCreateBody},
		{http.MethodPost, "/api/connect/pim/accounts/work/verify", ""},
		{http.MethodPatch, "/api/connect/pim/accounts/work/provider-config", `{"providerConfig":{"password":"n3w"}}`},
	}
	for _, caller := range []struct {
		name string
		caps []string
		want []string
	}{
		{"admin", []string{identity.CapIdentityCreate, identity.CapGovernanceWrite}, []string{"true"}},
		{"member", []string{identity.CapGovernanceWrite}, nil},
	} {
		for _, route := range routes {
			p := &recordingPIM{status: http.StatusOK, answer: `{}`}
			s := pimAccountServer(t, p, caller.caps...)
			pimAccountCall(s, route.method, route.target, route.body)
			calls := p.recorded()
			if len(calls) != 1 || strings.Join(calls[0].privateHosts, ",") != strings.Join(caller.want, ",") {
				t.Fatalf("%s %s %s: sidecar saw %s = %+v, want %v", caller.name, route.method, route.target, pimAllowPrivateHostsHeader, calls, caller.want)
			}
		}
	}
}

func TestTheClientCannotSendThePrivateHostsHeader(t *testing.T) {
	p := &recordingPIM{status: http.StatusOK, answer: `{}`}
	s := pimAccountServer(t, p, identity.CapGovernanceWrite)

	pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/work/verify", "", pimAllowPrivateHostsHeader, "true")

	calls := p.recorded()
	if len(calls) != 1 || len(calls[0].privateHosts) != 0 {
		t.Fatalf("a member's own %s reached the sidecar: %+v", pimAllowPrivateHostsHeader, calls)
	}
}

func TestTheEleventhAttemptInAMinuteIs429(t *testing.T) {
	p := &recordingPIM{status: http.StatusOK, answer: `{"ok":true}`}
	s := pimAccountServer(t, p)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	s.pimAttempts = newPIMAttemptLimiter(func() time.Time { return now })

	for i := range 10 {
		if rec := pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/work/verify", ""); rec.Code != http.StatusOK {
			t.Fatalf("attempt %d = %d, want 200", i+1, rec.Code)
		}
		now = now.Add(5 * time.Second)
	}
	rec := pimAccountCall(s, http.MethodPatch, "/api/connect/pim/accounts/work/provider-config", `{"providerConfig":{"password":"n3w"}}`)
	if rec.Code != http.StatusTooManyRequests || strings.TrimSpace(rec.Body.String()) != `{"error":"too_many_attempts"}` {
		t.Fatalf("eleventh attempt = %d %q, want 429 too_many_attempts", rec.Code, rec.Body.String())
	}
	if got := len(p.recorded()); got != 10 {
		t.Fatalf("sidecar calls = %d, want 10: the refused attempt must not dial", got)
	}

	now = now.Add(15 * time.Second)
	if rec := pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/work/verify", ""); rec.Code != http.StatusOK {
		t.Fatalf("attempt after the first left the window = %d, want 200", rec.Code)
	}
}

func TestASuccessfulVerifyWakesTheWatch(t *testing.T) {
	p := &recordingPIM{status: http.StatusOK, answer: `{"ok":true}`}
	s := pimAccountServer(t, p)
	watch := &fakeWatchService{}
	s.SetMailWatch(watch)

	rec := pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/work/verify", "")

	if rec.Code != http.StatusOK || watch.repaired != "work" {
		t.Fatalf("verify = %d, repaired %q; want 200 and the watch of work woken", rec.Code, watch.repaired)
	}
}

func TestAFailedPatchLeavesTheWatchAlone(t *testing.T) {
	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusConflict, http.StatusNotFound} {
		p := &recordingPIM{status: status, answer: `{"error":"x"}`}
		s := pimAccountServer(t, p)
		watch := &fakeWatchService{}
		s.SetMailWatch(watch)

		rec := pimAccountCall(s, http.MethodPatch, "/api/connect/pim/accounts/work/provider-config", `{"providerConfig":{"password":"n3w"}}`)

		if rec.Code != status || watch.repaired != "" {
			t.Fatalf("patch answered %d: relayed %d, repaired %q; want the status and no wake", status, rec.Code, watch.repaired)
		}
	}
}

func TestAnEscapedIdStaysOneSegment(t *testing.T) {
	p := &recordingPIM{status: http.StatusOK, answer: `{"ok":true}`}
	s := pimAccountServer(t, p)
	watch := &fakeWatchService{}
	s.SetMailWatch(watch)

	pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/a%2Fb/verify", "")

	calls := p.recorded()
	if len(calls) != 1 || calls[0].path != "/admin/accounts/a%2Fb/verify" {
		t.Fatalf("sidecar calls = %+v, want one call to /admin/accounts/a%%2Fb/verify", calls)
	}
	if watch.repaired != "a/b" {
		t.Fatalf("repaired %q, want the decoded id a/b", watch.repaired)
	}
}
