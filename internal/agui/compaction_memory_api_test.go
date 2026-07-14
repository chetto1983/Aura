package agui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/memory"
)

type fakeCompactionMemoryStore struct {
	actions   []string
	candidate memory.Candidate
	promoted  memory.Memory
	retrieved []memory.Memory
	err       error
}

func (f *fakeCompactionMemoryStore) CreateCandidate(context.Context, memory.CandidateInput) (memory.Candidate, error) {
	f.actions = append(f.actions, "create")
	return f.candidate, f.err
}
func (f *fakeCompactionMemoryStore) Promote(context.Context, memory.Policy, memory.Principal, string) (memory.Memory, error) {
	f.actions = append(f.actions, "promote")
	return f.promoted, f.err
}
func (f *fakeCompactionMemoryStore) Retrieve(context.Context, memory.Policy, memory.Principal, int) ([]memory.Memory, error) {
	f.actions = append(f.actions, "retrieve")
	return f.retrieved, f.err
}
func (f *fakeCompactionMemoryStore) WithdrawConsent(context.Context, string, string, string) error {
	f.actions = append(f.actions, "withdraw_consent")
	return f.err
}
func (f *fakeCompactionMemoryStore) DeleteSource(context.Context, string, string) error {
	f.actions = append(f.actions, "delete_source")
	return f.err
}
func (f *fakeCompactionMemoryStore) ForgetMe(context.Context, string, string) error {
	f.actions = append(f.actions, "forget_me")
	return f.err
}
func (f *fakeCompactionMemoryStore) Expire(context.Context, time.Time) error {
	f.actions = append(f.actions, "expire")
	return f.err
}
func (f *fakeCompactionMemoryStore) Supersede(context.Context, string, string) error {
	f.actions = append(f.actions, "supersede")
	return f.err
}

func TestCompactionMemoryAdminActionsReachStore(t *testing.T) {
	store := &fakeCompactionMemoryStore{
		candidate: memory.Candidate{ID: "candidate-1"},
		promoted:  memory.Memory{ID: "memory-1"},
		retrieved: []memory.Memory{{ID: "memory-1"}},
	}
	s := NewServer(nil, nil, ServerConfig{})
	s.SetCompactionMemoryStore(store)
	validCandidate := `{"candidate":{"tenant_id":"11111111-1111-4111-8111-111111111111","owner_id":"22222222-2222-4222-8222-222222222222","class":"preference","purpose":"assistant_personalization","consent_basis":"explicit","source_manifest":[{"kind":"turn","id":"turn-1","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"evidence":"concise answers","confidence":0.9,"authority":"user","sensitivity":"normal","region":"eu","encryption_class":"identity","retention_class":"durable","expires_at":"2026-07-15T00:00:00Z"}}`
	policyPrincipal := `"policy":{"review_approved":true,"classes":{"preference":{"allow_promotion":true,"allow_retrieval":true}}},"principal":{"tenant_id":"11111111-1111-4111-8111-111111111111","owner_id":"22222222-2222-4222-8222-222222222222","purpose":"assistant_personalization","region":"eu","capabilities":["memory.read"],"max_sensitivity":"normal"}`
	for _, tc := range []struct {
		action, body, want string
	}{
		{"create", validCandidate, "create"},
		{"promote", `{"candidate_id":"candidate-1",` + policyPrincipal + `}`, "promote"},
		{"retrieve", `{` + policyPrincipal + `,"limit":10}`, "retrieve"},
		{"withdraw_consent", `{"tenant":"tenant-1","owner":"owner-1","purpose":"assistant_personalization"}`, "withdraw_consent"},
		{"delete_source", `{"source_kind":"artifact","source_id":"asset-1"}`, "delete_source"},
		{"forget_me", `{"tenant":"tenant-1","owner":"owner-1"}`, "forget_me"},
		{"expire", `{"now":"2026-07-14T00:00:00Z"}`, "expire"},
		{"supersede", `{"old_candidate":"candidate-1","new_candidate":"candidate-2"}`, "supersede"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			store.actions = nil
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/admin/compaction-memory/"+tc.action, strings.NewReader(tc.body))
			s.Mux().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || len(store.actions) != 1 || store.actions[0] != tc.want {
				t.Fatalf("status=%d actions=%v body=%s", rec.Code, store.actions, rec.Body.String())
			}
		})
	}
}

func TestCompactionMemoryAdminRejectsInvalidRequestsAndHidesStoreErrors(t *testing.T) {
	t.Run("unwired", func(t *testing.T) {
		s := NewServer(nil, nil, ServerConfig{})
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/compaction-memory/expire", strings.NewReader(`{"now":"2026-07-14T00:00:00Z"}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	store := &fakeCompactionMemoryStore{err: errors.New("postgres password=secret")}
	s := NewServer(nil, nil, ServerConfig{})
	s.SetCompactionMemoryStore(store)
	for _, tc := range []struct {
		name, action, body string
		status             int
	}{
		{"unknown action", "unknown", `{}`, http.StatusBadRequest},
		{"invalid JSON", "expire", `{`, http.StatusBadRequest},
		{"store error", "expire", `{"now":"2026-07-14T00:00:00Z"}`, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/compaction-memory/"+tc.action, strings.NewReader(tc.body)))
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "secret") {
				t.Fatalf("backend error leaked: %s", rec.Body.String())
			}
		})
	}
}
