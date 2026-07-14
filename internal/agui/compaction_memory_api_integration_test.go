//go:build db_integration

package agui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/memory"
	"github.com/google/uuid"
)

func postCompactionMemoryAction(t *testing.T, s *Server, action string, body any, out any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compaction-memory/"+action, strings.NewReader(string(raw)))
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", action, rec.Code, rec.Body.String())
	}
	if out != nil && json.Unmarshal(rec.Body.Bytes(), out) != nil {
		t.Fatalf("decode %s response: %s", action, rec.Body.String())
	}
}

func createPromotedMemory(t *testing.T, s *Server, tenant, evidence string, expiresAt time.Time) (memory.Candidate, map[string]any, map[string]any) {
	t.Helper()
	sourceID := uuid.NewString()
	candidateBody := map[string]any{"candidate": map[string]any{
		"tenant_id": tenant, "owner_id": localIdentityID, "class": "preference",
		"purpose": "assistant_personalization", "consent_basis": "explicit",
		"source_manifest": []map[string]any{{"kind": "artifact", "id": sourceID, "digest": strings.Repeat("a", 64)}},
		"evidence":        evidence, "confidence": 0.9, "authority": "user", "sensitivity": "normal",
		"region": "eu", "encryption_class": "identity", "retention_class": "durable",
		"expires_at": expiresAt.UTC().Format(time.RFC3339Nano),
	}}
	var candidate memory.Candidate
	postCompactionMemoryAction(t, s, "create", candidateBody, &candidate)
	policy := map[string]any{"review_approved": true, "classes": map[string]any{
		"preference": map[string]any{"allow_promotion": true, "allow_retrieval": true},
	}}
	principal := map[string]any{
		"tenant_id": tenant, "owner_id": localIdentityID, "purpose": "assistant_personalization",
		"region": "eu", "capabilities": []string{"memory.read"}, "max_sensitivity": "normal",
	}
	postCompactionMemoryAction(t, s, "promote", map[string]any{
		"candidate_id": candidate.ID, "policy": policy, "principal": principal,
	}, &memory.Memory{})
	return candidate, policy, principal
}

func retrieveCompactionMemories(t *testing.T, s *Server, policy, principal map[string]any) []memory.Memory {
	t.Helper()
	var got []memory.Memory
	postCompactionMemoryAction(t, s, "retrieve", map[string]any{
		"policy": policy, "principal": principal, "limit": 20,
	}, &got)
	return got
}

func TestCompactionMemoryAdminLifecyclePostgresE2E(t *testing.T) {
	pool := disposableLiveSagaPool(t, t.Context())
	s := NewServer(nil, nil, ServerConfig{})
	s.SetCompactionMemoryStore(memory.NewStore(pool))
	now := time.Now().UTC()

	for _, event := range []string{"withdraw_consent", "delete_source", "forget_me", "expire"} {
		t.Run(event, func(t *testing.T) {
			tenant := uuid.NewString()
			candidate, policy, principal := createPromotedMemory(t, s, tenant, "evidence "+event, now.Add(time.Hour))
			if got := retrieveCompactionMemories(t, s, policy, principal); len(got) != 1 {
				t.Fatalf("before %s: got %d memories, want 1", event, len(got))
			}
			switch event {
			case "withdraw_consent":
				postCompactionMemoryAction(t, s, event, map[string]any{"tenant": tenant, "owner": localIdentityID, "purpose": "assistant_personalization"}, nil)
			case "delete_source":
				var sourceID string
				if err := pool.QueryRow(t.Context(), `SELECT source_id FROM aura.compaction_memory_sources WHERE candidate_id=$1`, candidate.ID).Scan(&sourceID); err != nil {
					t.Fatal(err)
				}
				postCompactionMemoryAction(t, s, event, map[string]any{"source_kind": "artifact", "source_id": sourceID}, nil)
			case "forget_me":
				postCompactionMemoryAction(t, s, event, map[string]any{"tenant": tenant, "owner": localIdentityID}, nil)
			case "expire":
				postCompactionMemoryAction(t, s, event, map[string]any{"now": now.Add(2 * time.Hour).Format(time.RFC3339Nano)}, nil)
			}
			if got := retrieveCompactionMemories(t, s, policy, principal); len(got) != 0 {
				t.Fatalf("after %s: got %d memories, want 0", event, len(got))
			}
		})
	}

	t.Run("supersede", func(t *testing.T) {
		tenant := uuid.NewString()
		oldCandidate, policy, principal := createPromotedMemory(t, s, tenant, "old preference", now.Add(time.Hour))
		newCandidate, _, _ := createPromotedMemory(t, s, tenant, "new preference", now.Add(time.Hour))
		postCompactionMemoryAction(t, s, "supersede", map[string]any{
			"old_candidate": oldCandidate.ID, "new_candidate": newCandidate.ID,
		}, nil)
		got := retrieveCompactionMemories(t, s, policy, principal)
		if len(got) != 1 || got[0].CandidateID != newCandidate.ID {
			t.Fatalf("superseded retrieval=%s", fmt.Sprint(got))
		}
	})
}
