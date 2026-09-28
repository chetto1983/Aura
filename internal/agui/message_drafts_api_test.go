package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/runner"
)

type messageDraftAPIRunner struct {
	*scriptedRunner
	drafts       []messagedrafts.Draft
	listOwner    string
	resolveOwner string
	resolveID    string
	action       string
	overrides    json.RawMessage
}

func (f *messageDraftAPIRunner) ListMessageDrafts(ctx context.Context, _ string) ([]messagedrafts.Draft, error) {
	f.listOwner = identityctx.IdentityID(ctx)
	return f.drafts, nil
}

func (f *messageDraftAPIRunner) ResolveMessageDraft(ctx context.Context, id, action string, overrides json.RawMessage) (runner.MessageDraftResolution, error) {
	f.resolveOwner = identityctx.IdentityID(ctx)
	f.resolveID, f.action, f.overrides = id, action, overrides
	return runner.MessageDraftResolution{Status: messagedrafts.StatusSent, Directive: runner.ResolveDirective{Outcome: runner.OutcomeContinue}}, nil
}

func TestMessageDraftAPIReviewProjectionAndResolve(t *testing.T) {
	conversationID := "11111111-1111-4111-8111-111111111111"
	draftID := "22222222-2222-4222-8222-222222222222"
	fake := &messageDraftAPIRunner{scriptedRunner: &scriptedRunner{}, drafts: []messagedrafts.Draft{{
		ID: draftID, ConversationID: conversationID, Target: messagedrafts.Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		OriginalArgs: json.RawMessage(`{"action":"send_email","to":["person@example.test"],"subject":"Private subject","body":"Private body","attachments":[{"name":"report.pdf","base64Content":"secret-file-bytes","attachmentId":"private-asset"}]}`),
		Status:       messagedrafts.StatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}}}
	server := NewServer(fake, &fakeConvStore{known: map[string]bool{conversationID: true}}, ServerConfig{})
	handler := server.Mux()

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/message-drafts?conversation_id="+conversationID, nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Private body") || !strings.Contains(list.Body.String(), "report.pdf") || fake.listOwner != localIdentityID ||
		strings.Contains(list.Body.String(), "registered_tool_name") || strings.Contains(list.Body.String(), "secret-file-bytes") || strings.Contains(list.Body.String(), "private-asset") {
		t.Fatalf("review list projection failed: code=%d body=%s owner=%q", list.Code, list.Body.String(), fake.listOwner)
	}

	resolve := httptest.NewRecorder()
	handler.ServeHTTP(resolve, httptest.NewRequest(http.MethodPost, "/api/message-drafts/"+draftID+"/resolve",
		strings.NewReader(`{"action":"send","overrides":{"to":["edited@example.test"],"body":"Edited"}}`)))
	if resolve.Code != http.StatusOK || fake.resolveOwner != localIdentityID || fake.resolveID != draftID || fake.action != "send" ||
		!strings.Contains(string(fake.overrides), "edited@example.test") || !strings.Contains(resolve.Body.String(), `"status":"sent"`) {
		t.Fatalf("review resolution failed: code=%d body=%s", resolve.Code, resolve.Body.String())
	}
}

func TestMessageDraftAPIRejectsInvalidOrForeignRequests(t *testing.T) {
	known := "11111111-1111-4111-8111-111111111111"
	server := NewServer(&messageDraftAPIRunner{scriptedRunner: &scriptedRunner{}}, &fakeConvStore{known: map[string]bool{known: true}}, ServerConfig{})
	handler := server.Mux()
	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/message-drafts?conversation_id=invalid", "", http.StatusNotFound},
		{http.MethodGet, "/api/message-drafts?conversation_id=22222222-2222-4222-8222-222222222222", "", http.StatusNotFound},
		{http.MethodPost, "/api/message-drafts/invalid/resolve", `{"action":"send"}`, http.StatusNotFound},
		{http.MethodPost, "/api/message-drafts/22222222-2222-4222-8222-222222222222/resolve", `{"action":"cancel"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/message-drafts/22222222-2222-4222-8222-222222222222/resolve", `{"action":"decline","overrides":{}}`, http.StatusBadRequest},
		{http.MethodPost, "/api/message-drafts/22222222-2222-4222-8222-222222222222/resolve", `{"action":"send","unexpected":true}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if response.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, response.Code, tc.want)
		}
	}
}
