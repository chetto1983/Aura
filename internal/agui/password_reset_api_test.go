package agui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordResetRoutes(t *testing.T) {
	store := newFakePasswordResetStore(t)
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  &fakePasswordResetter{},
		Clock:     func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) },
	})
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetPasswordResetService(svc)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	startBody := `{"email":"reset@example.com"}`
	startResp, startRaw := postPasswordResetJSON(t, srv.URL+"/api/auth/password-reset/start", startBody)
	if startResp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202: %s", startResp.StatusCode, startRaw)
	}
	if !strings.Contains(startRaw, `"status":"ok"`) {
		t.Fatalf("start body = %s, want status ok", startRaw)
	}

	questionBody := `{"email":"reset@example.com","code":"raw-code-123"}`
	questionResp, questionRaw := postPasswordResetJSON(t, srv.URL+"/api/auth/password-reset/question", questionBody)
	if questionResp.StatusCode != http.StatusOK {
		t.Fatalf("question status = %d, want 200: %s", questionResp.StatusCode, questionRaw)
	}
	if !strings.Contains(questionRaw, store.record.Question) {
		t.Fatalf("question body = %s, want the security question", questionRaw)
	}

	verifyBody := `{"email":"reset@example.com","code":"raw-code-123","answer":"Blue bicycle"}`
	verifyResp, verifyRaw := postPasswordResetJSON(t, srv.URL+"/api/auth/password-reset/verify", verifyBody)
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("verify status = %d, want 200: %s", verifyResp.StatusCode, verifyRaw)
	}
	var verify struct {
		ResetToken string `json:"resetToken"`
	}
	if err := json.Unmarshal([]byte(verifyRaw), &verify); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if verify.ResetToken != store.resetToken {
		t.Fatalf("reset token = %q, want %q", verify.ResetToken, store.resetToken)
	}

	completeBody := `{"resetToken":"` + store.resetToken + `","password":"new-pass-123"}`
	completeResp, completeRaw := postPasswordResetJSON(t, srv.URL+"/api/auth/password-reset/complete", completeBody)
	if completeResp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, want 200: %s", completeResp.StatusCode, completeRaw)
	}
	if !strings.Contains(completeRaw, `"status":"password_updated"`) {
		t.Fatalf("complete body = %s, want password_updated", completeRaw)
	}

	allBodies := startRaw + verifyRaw + completeRaw
	for _, secret := range []string{"raw-code-123", "Blue bicycle", "new-pass-123"} {
		if strings.Contains(allBodies, secret) {
			t.Fatalf("password reset route leaked %q in response bodies: %s", secret, allBodies)
		}
	}
}

// TestPasswordResetCompleteSamePasswordRoute proves the same-password sentinel surfaces as a
// distinguishable 409 (so the browser can show a specific message) without leaking the password.
func TestPasswordResetCompleteSamePasswordRoute(t *testing.T) {
	store := newFakePasswordResetStore(t)
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  &fakePasswordResetter{err: ErrPasswordResetSamePassword},
	})
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetPasswordResetService(svc)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	body := `{"resetToken":"` + store.resetToken + `","password":"same-as-current"}`
	resp, raw := postPasswordResetJSON(t, srv.URL+"/api/auth/password-reset/complete", body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("complete status = %d, want 409 for same password: %s", resp.StatusCode, raw)
	}
	if strings.Contains(raw, "same-as-current") {
		t.Fatalf("409 body leaked the password: %s", raw)
	}
}

func postPasswordResetJSON(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp, string(raw)
}
