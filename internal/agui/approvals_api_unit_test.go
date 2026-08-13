package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// errDSNLeak is a sentinel runner/store error embedding a fake DSN password
// ("supersecret"). The resolve (500) and list (500) error branches assert this
// substring never reaches the wire — proving sanitizeErr redacts internal errors
// (T-25-06 / T-25-09). Distinct from the question-sanitization tests, which exercise
// SanitizeString on surfaced content rather than error redaction.
var errDSNLeak = errors.New("dial tcp: failed to connect to postgres://aura_app:supersecret@db:5432/aura: connection refused")

// approvals_api_unit_test.go covers the APRV-02 resolve verb→action mapping + the
// edge-case branches DB-free, via the scriptedRunner double (which records the
// answers map it received) and a fakeApprovalStore. This keeps the resolve mapping
// (the three-verb bridge, the deny≠accept distinction, the 400/404 mappings) and the
// read error/503 branches covered in CI without the live stack; the real cross-thread
// aggregation round-trip is the db_integration suite's job.

// fakeApprovalStore is an in-memory ApprovalStore for the read-path branches.
type fakeApprovalStore struct {
	pendings []askuser.Pending
	err      error
}

func (f *fakeApprovalStore) ListPendingAll(_ context.Context, _ int) ([]askuser.Pending, error) {
	return f.pendings, f.err
}

// Phase 36 owner-scoped surface. ListPendingAllForIdentity mirrors ListPendingAll (the fake
// models no ownership) so the list-projection/error branches stay covered. GetByTokenForIdentity
// satisfy the resolve ownership-gate interface; the resolve suites wire approvals=nil (the
// gate is skipped), so these are not exercised here — the real gate is db_integration.
func (f *fakeApprovalStore) ListPendingAllForIdentity(_ context.Context, _ string, _ int) ([]askuser.Pending, error) {
	return f.pendings, f.err
}

func (f *fakeApprovalStore) GetByTokenForIdentity(_ context.Context, _, _ string) (askuser.Pending, error) {
	return askuser.Pending{}, nil
}

func (f *fakeApprovalStore) GetByToken(_ context.Context, _ string) (askuser.Pending, error) {
	return askuser.Pending{}, nil
}

// newResolveTestServer builds a server over the scripted runner (optionally also a
// fake approval store) and returns the httptest server.
func newResolveTestServer(t *testing.T, run Runner, approvals ApprovalStore) *httptest.Server {
	t.Helper()
	s := NewServer(run, &errConvStore{}, ServerConfig{})
	if approvals != nil {
		s.SetApprovalStore(approvals)
	}
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

func postResolve(t *testing.T, srv *httptest.Server, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/approvals/"+token+"/resolve", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST resolve: %v", err)
	}
	defer resp.Body.Close()
	raw := make([]byte, 0)
	buf := make([]byte, 512)
	for {
		n, e := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if e != nil {
			break
		}
	}
	return resp.StatusCode, string(raw)
}

// TestApprovalsAPI_ResolveReturnsDirective pins the Phase A wire contract: resolve answers 200
// with the runner's verdict projected as {outcome, remaining}, so the cockpit renders the SAME
// decision Telegram does instead of inferring a re-drive client-side.
func TestApprovalsAPI_ResolveReturnsDirective(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directive runner.ResolveDirective
		want      string
	}{
		{"scheduled approval accepted", runner.ResolveDirective{Outcome: runner.OutcomeApproved}, "approved"},
		{"scheduled approval rejected", runner.ResolveDirective{Outcome: runner.OutcomeRejected}, "rejected"},
		{"ordinary pause continues", runner.ResolveDirective{Outcome: runner.OutcomeContinue}, "continue"},
		{"more pauses pending", runner.ResolveDirective{Outcome: runner.OutcomePending, Remaining: 2}, "pending"},
		{"cancel terminates", runner.ResolveDirective{Outcome: runner.OutcomeTerminated}, "terminated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := &scriptedRunner{directive: tc.directive}
			srv := newResolveTestServer(t, run, nil)
			token := uuid.Must(uuid.NewV7()).String()

			code, body := postResolve(t, srv, token, `{"action":"accept","content":"ok"}`)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", code, body)
			}
			var got struct {
				Outcome   string `json:"outcome"`
				Remaining int    `json:"remaining"`
			}
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("decode %q: %v", body, err)
			}
			if got.Outcome != tc.want {
				t.Errorf("outcome = %q, want %q", got.Outcome, tc.want)
			}
			if got.Remaining != tc.directive.Remaining {
				t.Errorf("remaining = %d, want %d", got.Remaining, tc.directive.Remaining)
			}
		})
	}
}

// TestApprovalsAPI_ResolveVerbMapping asserts each verb maps to the correct
// askuser.Action* in the single-entry map handed to SubmitAnswer. accept carries the
// operator content; decline/cancel pass the action through (the Runner overrides the
// content for those — proven in internal/runner/runner_resume_test.go).
func TestApprovalsAPI_ResolveVerbMapping(t *testing.T) {
	cases := []struct {
		name, body, wantAction, wantContent string
	}{
		{"accept", `{"action":"accept","content":"yes please"}`, askuser.ActionAccept, "yes please"},
		{"decline", `{"action":"decline","content":"ignored-by-runner"}`, askuser.ActionDecline, "ignored-by-runner"},
		{"cancel", `{"action":"cancel"}`, askuser.ActionCancel, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := &scriptedRunner{}
			srv := newResolveTestServer(t, run, nil)
			token := uuid.Must(uuid.NewV7()).String()

			code, body := postResolve(t, srv, token, c.body)
			if code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200: %s", c.name, code, body)
			}
			if run.gotAnswers == nil {
				t.Fatalf("%s: SubmitAnswer was not called", c.name)
			}
			got, ok := run.gotAnswers[token]
			if !ok {
				t.Fatalf("%s: answers map keyed by %q missing; got %v", c.name, token, run.gotAnswers)
			}
			if got.Action != c.wantAction {
				t.Errorf("%s: action = %q, want %q", c.name, got.Action, c.wantAction)
			}
			if got.Content != c.wantContent {
				t.Errorf("%s: content = %q, want %q", c.name, got.Content, c.wantContent)
			}
		})
	}
}

// TestApprovalsAPI_ResolveNonUUIDToken asserts a malformed (non-UUID) token is a clean
// 404 BEFORE the runner round-trip (T-25-09 — never a 500 from a parse leak).
func TestApprovalsAPI_ResolveNonUUIDToken(t *testing.T) {
	run := &scriptedRunner{}
	srv := newResolveTestServer(t, run, nil)

	code, body := postResolve(t, srv, "not-a-uuid", `{"action":"accept"}`)
	if code != http.StatusNotFound {
		t.Errorf("non-UUID token status = %d, want 404: %s", code, body)
	}
	if run.gotAnswers != nil {
		t.Errorf("a malformed token must not reach SubmitAnswers; got %v", run.gotAnswers)
	}
}

// TestApprovalsAPI_ResolveUnknownAction asserts an unrecognized action verb is a 400
// and never reaches the runner (Tampering guard: deny≠accept, only the three known
// verbs are admitted).
func TestApprovalsAPI_ResolveUnknownAction(t *testing.T) {
	run := &scriptedRunner{}
	srv := newResolveTestServer(t, run, nil)
	token := uuid.Must(uuid.NewV7()).String()

	code, body := postResolve(t, srv, token, `{"action":"approve"}`) // "approve" is not a valid verb
	if code != http.StatusBadRequest {
		t.Errorf("unknown action status = %d, want 400: %s", code, body)
	}
	if run.gotAnswers != nil {
		t.Errorf("an unknown action must not reach SubmitAnswers; got %v", run.gotAnswers)
	}
}

// TestApprovalsAPI_ResolveInvalidBody asserts a malformed JSON body is a 400.
func TestApprovalsAPI_ResolveInvalidBody(t *testing.T) {
	srv := newResolveTestServer(t, &scriptedRunner{}, nil)
	token := uuid.Must(uuid.NewV7()).String()
	code, body := postResolve(t, srv, token, `{not json`)
	if code != http.StatusBadRequest {
		t.Errorf("invalid body status = %d, want 400: %s", code, body)
	}
}

// TestApprovalsAPI_ResolvePauseNotFound asserts ErrPauseNotFound from the runner maps
// to 404 (APRV-03 — an already-resolved / auto-terminated pending surfaces a clear
// "gone", never a false success).
func TestApprovalsAPI_ResolvePauseNotFound(t *testing.T) {
	run := &scriptedRunner{answersErr: askuser.ErrPauseNotFound}
	srv := newResolveTestServer(t, run, nil)
	token := uuid.Must(uuid.NewV7()).String()

	code, body := postResolve(t, srv, token, `{"action":"accept"}`)
	if code != http.StatusNotFound {
		t.Errorf("ErrPauseNotFound status = %d, want 404: %s", code, body)
	}
}

// TestApprovalsAPI_ResolveRunnerError asserts a generic runner error is a 500 with the
// message redacted (sanitizeErr) — a runner error embedding a DSN must not leak.
func TestApprovalsAPI_ResolveRunnerError(t *testing.T) {
	run := &scriptedRunner{answersErr: errDSNLeak}
	srv := newResolveTestServer(t, run, nil)
	token := uuid.Must(uuid.NewV7()).String()

	code, body := postResolve(t, srv, token, `{"action":"accept"}`)
	if code != http.StatusInternalServerError {
		t.Fatalf("runner error status = %d, want 500: %s", code, body)
	}
	if strings.Contains(body, "supersecret") {
		t.Errorf("runner error leaked a DSN password on the wire: %s", body)
	}
}

// TestApprovalsAPI_ListNoStore asserts GET /api/approvals answers 503 when no
// ApprovalStore is wired (optional wiring — the daemon sets it via SetApprovalStore).
func TestApprovalsAPI_ListNoStore(t *testing.T) {
	srv := newResolveTestServer(t, &scriptedRunner{}, nil) // no approval store
	resp, err := http.Get(srv.URL + "/api/approvals")
	if err != nil {
		t.Fatalf("GET /api/approvals: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no-store GET status = %d, want 503", resp.StatusCode)
	}
}

// TestApprovalsAPI_ListStoreError asserts a store read failure is a 500 with the error
// redacted on the wire.
func TestApprovalsAPI_ListStoreError(t *testing.T) {
	srv := newResolveTestServer(t, &scriptedRunner{}, &fakeApprovalStore{err: errDSNLeak})
	resp, err := http.Get(srv.URL + "/api/approvals")
	if err != nil {
		t.Fatalf("GET /api/approvals: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("store-error GET status = %d, want 500", resp.StatusCode)
	}
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	if strings.Contains(string(buf[:n]), "supersecret") {
		t.Errorf("store error leaked a DSN password on the wire: %s", buf[:n])
	}
}

// TestApprovalsAPI_ListProjectionSanitizes asserts the read projection runs the
// question through SanitizeString even with an in-memory store (V7 redaction is in the
// handler, not the store).
func TestApprovalsAPI_ListProjectionSanitizes(t *testing.T) {
	store := &fakeApprovalStore{pendings: []askuser.Pending{
		{
			Token:          uuid.Must(uuid.NewV7()).String(),
			ConversationID: uuid.Must(uuid.NewV7()).String(),
			Kind:           "approval",
			Question:       "use api_key=sk-supersecret to call it?",
			Priority:       5,
		},
	}}
	srv := newResolveTestServer(t, &scriptedRunner{}, store)
	resp, err := http.Get(srv.URL + "/api/approvals")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if strings.Contains(body, "sk-supersecret") {
		t.Errorf("projection did not sanitize the question (V7): %s", body)
	}
	if !strings.Contains(body, redact.Placeholder) {
		t.Errorf("projection missing the %s marker: %s", redact.Placeholder, body)
	}
}
