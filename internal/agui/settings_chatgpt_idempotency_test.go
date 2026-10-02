package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatGPTLoginDoesNotJournalReauthorizationCredentials(t *testing.T) {
	registry := &memoryHTTPRegistry{}
	server := &Server{operations: registry}
	calls := 0
	handler := server.idempotencyMutation(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]string{"auth_url": "https://auth.openai.com/api/accounts/authorize?id_token_hint=private-token"})
	}), httpMutationRoutes["POST /api/settings/chatgpt/login"])
	for i := range 2 {
		request := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/settings/chatgpt/login", nil), "00000000-0000-0000-0000-000000000007")
		request.Header.Set("Idempotency-Key", "login-attempt")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if i == 0 && (recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "private-token")) {
			t.Fatal("authorization URL was not delivered to its caller")
		}
		if i == 1 && recorder.Code < 400 {
			t.Fatal("same operation started another authorization")
		}
	}
	if calls != 1 || registry.marked != 1 || registry.replay != nil {
		t.Fatal("authorization credentials were journaled or authorization repeated")
	}
}
