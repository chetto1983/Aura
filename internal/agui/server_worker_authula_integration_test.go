//go:build db_integration

package agui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/webauth"
	"github.com/google/uuid"
)

func TestWorkerControlsWithRealAuthulaCookies(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	provider, err := webauth.New(webauth.Config{
		DSN:            envOrSkip(t, "AURA_DB_URL"),
		Secret:         "00000000000000000000000000000000000000000000000000000000000000a1",
		TrustedOrigins: []string{"https://127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Error(err)
		}
	})
	linker := webauth.NewIdentityLinker(pool)
	foreignID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id,name,kind) VALUES ($1,$2,'user')`, foreignID, "worker-auth-"+foreignID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.identities WHERE id=$1`, foreignID) })

	mintCookie := func(identityID string) *http.Cookie {
		t.Helper()
		core := provider.CoreServices()
		user, err := core.UserService.Create(ctx, "worker-control-test", uuid.NewString()+"@aura.local", true, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.identity_auth_links WHERE authula_user_id=$1`, user.ID)
			_ = core.UserService.Delete(context.Background(), user.ID)
		})
		if err := linker.LinkOperator(ctx, identityID, user.ID); err != nil {
			t.Fatal(err)
		}
		token, err := core.TokenService.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := core.SessionService.Create(ctx, user.ID, core.TokenService.Hash(token), nil, nil, time.Hour); err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: webauth.SessionCookieName, Value: token, Secure: true, HttpOnly: true, Path: "/"}
	}
	ownerCookie, foreignCookie := mintCookie(localIdentityID), mintCookie(foreignID)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	conv := seedConversation(t, convStore, pool, nil)
	s := NewServer(&scriptedRunner{}, convStore, ServerConfig{})
	s.runs = NewRunRegistry(ServerConfig{})
	t.Cleanup(s.runs.Close)
	inbox := steer.NewPostgresStore(pool, steer.Config{MaxBytes: 64})
	s.SetSteerInbox(inbox)
	s.SetOperationRegistry(idempotency.New(pool, idempotency.Config{}))
	jobs := documents.NewPostgresIngestionJobStore(pool)
	s.SetWorkerControlJobs(jobs)
	workerCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	var stops atomic.Int32
	control, err := s.runs.StartWorker(ctx, agent.WorkerControlParams{
		ConversationID: conv, ChildID: "auth-running", SteerEnabled: true, Cancel: cancel,
		Stop: func(context.Context) error { stops.Add(1); cancel(); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Finish() })
	queued, err := jobs.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: localIdentityID, JobType: "swarm_delegation", Status: "queued", MaxAttempts: 3,
		IdempotencyKey: "auth-queued-" + conv,
		Payload:        map[string]any{"conversation_id": conv, "child_id": "auth-queued", "goal": "ownership regression", "fanout_key": "f-" + conv},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.ingestion_jobs WHERE id=$1`, queued.ID) })
	validator := webauth.NewValidator(provider, linker)
	server := httptest.NewTLSServer(RequireAuth(s.Mux(), AuthDeps{
		SecretConfigured: true, Identities: storeChecker{store: identity.New(pool)},
		SessionValidator: func(r *http.Request) (string, bool) { id, err := validator.Validate(r); return id, err == nil },
	}))
	t.Cleanup(server.Close)
	call := func(cookie *http.Cookie, child, action, body, key string) int {
		t.Helper()
		method := http.MethodPost
		if action == "controls" {
			method = http.MethodGet
		}
		req, err := http.NewRequest(method, server.URL+"/api/conversations/"+conv+"/swarm/"+child+"/"+action, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		// A client assertion cannot override the principal from its real session.
		req.Header.Set("X-Aura-Identity", localIdentityID)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	queuedBody, _ := json.Marshal(map[string]any{"job_id": queued.ID, "attempt_count": 0})
	for _, tc := range []struct{ child, action, body string }{
		{"auth-running", "controls", ""},
		{"auth-running", "steer", workerBody(control.RunID, "foreign instruction")},
		{"auth-running", "cancel", workerBody(control.RunID, "")},
		{"auth-queued", "controls", ""},
		{"auth-queued", "cancel", string(queuedBody)},
	} {
		t.Run("foreign_"+tc.child+"_"+tc.action, func(t *testing.T) {
			if status := call(foreignCookie, tc.child, tc.action, tc.body, "cross-owner-"+tc.child+tc.action); status != http.StatusNotFound {
				t.Fatalf("foreign HTTP status=%d, want404", status)
			}
		})
	}
	if stops.Load() != 0 || workerCtx.Err() != nil {
		t.Fatal("foreign identity canceled the owner worker")
	}
	receipts, err := inbox.WorkerHistory(ctx, conv, "auth-running")
	if err != nil || len(receipts) != 0 {
		t.Fatalf("foreign steer persisted: receipts=%d error=%v", len(receipts), err)
	}
	job, found, err := jobs.FindDelegationJob(ctx, localIdentityID, conv, "auth-queued")
	if err != nil || !found || job.OperatorCancelled {
		t.Fatal("foreign identity changed queued cancellation")
	}
	if status := call(&http.Cookie{Name: webauth.SessionCookieName, Value: "invalid-session"}, "auth-running", "cancel", workerBody(control.RunID, ""), "invalid-cookie"); status != http.StatusUnauthorized {
		t.Fatalf("invalid cookie status=%d", status)
	}
	if status := call(ownerCookie, "auth-running", "controls", "", ""); status != http.StatusOK {
		t.Fatalf("owner history status=%d", status)
	}
	staleQueuedBody, _ := json.Marshal(map[string]any{"job_id": queued.ID, "attempt_count": 1})
	for _, tc := range []struct {
		name, child, action, body string
		status                    int
	}{
		{"malformed", "auth-running", "steer", "{", http.StatusBadRequest},
		{"oversized", "auth-running", "steer", workerBody(control.RunID, strings.Repeat("x", 65)), http.StatusBadRequest},
		{"stale_run", "auth-running", "cancel", workerBody("run-"+uuid.NewString(), ""), http.StatusNotFound},
		{"stale_attempt", "auth-queued", "cancel", string(staleQueuedBody), http.StatusGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status := call(ownerCookie, tc.child, tc.action, tc.body, tc.name); status != tc.status {
				t.Fatalf("HTTP status=%d, want %d", status, tc.status)
			}
		})
	}
	for range 2 {
		if status := call(ownerCookie, "auth-running", "steer", workerBody(control.RunID, "use JSON"), "owner-steer"); status != http.StatusAccepted {
			t.Fatalf("owner steer status=%d", status)
		}
	}
	receipts, err = inbox.WorkerHistory(ctx, conv, "auth-running")
	if err != nil || len(receipts) != 1 || receipts[0].Text != "use JSON" {
		t.Fatalf("owner replay receipts=%d error=%v", len(receipts), err)
	}
	if status := call(ownerCookie, "auth-queued", "cancel", string(queuedBody), "owner-queued-cancel"); status != http.StatusAccepted {
		t.Fatalf("owner queued cancel status=%d", status)
	}
	job, found, err = jobs.FindDelegationJob(ctx, localIdentityID, conv, "auth-queued")
	if err != nil || !found || !job.OperatorCancelled {
		t.Fatal("owner queued cancellation was not persisted")
	}
	for range 2 {
		if status := call(ownerCookie, "auth-running", "cancel", workerBody(control.RunID, ""), "owner-running-cancel"); status != http.StatusAccepted {
			t.Fatalf("owner cancel status=%d", status)
		}
	}
	if stops.Load() != 1 || workerCtx.Err() != context.Canceled {
		t.Fatal("owner cancellation/replay did not stop exactly once")
	}
}
