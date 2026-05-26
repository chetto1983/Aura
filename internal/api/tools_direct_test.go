package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

type fakeToolExecutor struct {
	names []string
	out   string
	err   error
	got   struct {
		name           string
		args           map[string]any
		userID         string
		conversationID string
	}
}

func (f *fakeToolExecutor) Names() []string {
	return append([]string(nil), f.names...)
}

func (f *fakeToolExecutor) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	f.got.name = name
	f.got.args = args
	f.got.userID = toolregistry.UserIDFromContext(ctx)
	f.got.conversationID = toolregistry.ConversationIDFromContext(ctx)
	if f.err != nil {
		return "", f.err
	}
	if f.out != "" {
		return f.out, nil
	}
	return `{"ok":true}`, nil
}

func TestToolRegistryCallExecutesLiveRegistry(t *testing.T) {
	executor := &fakeToolExecutor{names: []string{"search", "text_response"}}
	router := NewRouter(Deps{Tools: executor})
	req := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(`{
		"name":"search",
		"args":{"action":"user_facts","query":"Davide"},
		"user_id":"u1",
		"conversation_id":"thread-1"
	}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if executor.got.name != "search" {
		t.Fatalf("tool name = %q", executor.got.name)
	}
	if executor.got.args["action"] != "user_facts" {
		t.Fatalf("args = %#v", executor.got.args)
	}
	if executor.got.userID != "u1" {
		t.Fatalf("user id = %q", executor.got.userID)
	}
	if executor.got.conversationID != "thread-1" {
		t.Fatalf("conversation id = %q", executor.got.conversationID)
	}
	var resp toolCallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OutputBytes == 0 || resp.Name != "search" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestToolRegistryCallPersistsProbeAttempt(t *testing.T) {
	reader := newAttemptsTestReader(t)
	executor := &fakeToolExecutor{names: []string{"search", "text_response"}}
	router := NewRouter(Deps{
		Tools:             executor,
		ToolProbeRecorder: NewSQLiteToolProbeRecorder(reader.db),
		ToolAttemptsStats: reader,
	})
	req := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(`{
		"name":"search",
		"args":{"action":"user_facts","query":"Davide"},
		"user_id":"u1",
		"conversation_id":"thread-1"
	}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp toolCallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RunID == "" || !resp.AttemptRecorded {
		t.Fatalf("response missing persisted probe metadata: %#v", resp)
	}

	var channel, status, threadID, principalID, toolName, outcome, class, argKeys, errRedacted string
	err := reader.db.QueryRowContext(context.Background(), `
SELECT r.channel, r.status, r.thread_id, r.principal_id,
       ta.tool_name, ta.outcome, ta.class, ta.arg_keys_json, ta.error_redacted
FROM runs r
JOIN tool_attempts ta ON ta.run_id = r.id
WHERE r.id = ?`, resp.RunID).Scan(
		&channel, &status, &threadID, &principalID,
		&toolName, &outcome, &class, &argKeys, &errRedacted,
	)
	if err != nil {
		t.Fatalf("query persisted probe: %v", err)
	}
	if channel != "api" || status != "completed" || threadID != "thread-1" || principalID != "u1" {
		t.Fatalf("run row = channel=%q status=%q thread=%q principal=%q", channel, status, threadID, principalID)
	}
	if toolName != "search" || outcome != "ok" || class != "" {
		t.Fatalf("tool_attempt row = tool=%q outcome=%q class=%q", toolName, outcome, class)
	}
	if argKeys != `["action","query"]` {
		t.Fatalf("arg keys = %s", argKeys)
	}
	if strings.Contains(argKeys, "Davide") || strings.Contains(errRedacted, "Davide") || strings.Contains(errRedacted, "user_facts") {
		t.Fatalf("tool_attempt leaked raw arg value: arg_keys=%q error=%q", argKeys, errRedacted)
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/maintenance/tool-attempts", nil)
	statsRec := httptest.NewRecorder()
	router.ServeHTTP(statsRec, statsReq)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("maintenance status=%d body=%s", statsRec.Code, statsRec.Body.String())
	}
	var stats ToolAttemptsResponse
	if err := json.Unmarshal(statsRec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.PerTool["search"].OK != 1 {
		t.Fatalf("maintenance search ok = %d, want 1", stats.PerTool["search"].OK)
	}
}

func TestToolRegistryListReturnsNames(t *testing.T) {
	router := NewRouter(Deps{Tools: &fakeToolExecutor{names: []string{"search", "source"}}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tools", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Tools) != 2 || resp.Tools[0] != "search" || resp.Tools[1] != "source" {
		t.Fatalf("tools = %#v", resp.Tools)
	}
}

func TestToolRegistryCallReturnsAwaitingUserInputAsInspectableOutput(t *testing.T) {
	executor := &fakeToolExecutor{
		names: []string{"ask_user"},
		err: &toolregistry.ErrAwaitingUserInput{
			Question: "Which target?",
			Options:  []string{"A", "B"},
			Kind:     "choice",
		},
	}
	router := NewRouter(Deps{Tools: executor})
	req := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(`{
		"name":"ask_user",
		"args":{"question":"Which target?","options":["A","B"],"kind":"choice"}
	}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp toolCallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var output struct {
		Awaiting bool     `json:"awaiting_user_input"`
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Kind     string   `json:"kind"`
	}
	if err := json.Unmarshal([]byte(resp.Output), &output); err != nil {
		t.Fatalf("output is not JSON: %v; %q", err, resp.Output)
	}
	if !output.Awaiting || output.Question != "Which target?" || output.Kind != "choice" || len(output.Options) != 2 {
		t.Fatalf("awaiting output = %#v", output)
	}
}

func TestToolRegistryCallMapsSchemaValidationToBadRequest(t *testing.T) {
	reader := newAttemptsTestReader(t)
	executor := &fakeToolExecutor{
		names: []string{"agent_note"},
		err:   errors.Join(errors.New("agent_note: content is required"), llm.ErrSchemaValidation),
	}
	router := NewRouter(Deps{Tools: executor, ToolProbeRecorder: NewSQLiteToolProbeRecorder(reader.db)})
	req := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(`{
		"name":"agent_note",
		"args":{"action":"set","note":"wrong key"}
	}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var outcome, class, reason, argKeys, errRedacted string
	if err := reader.db.QueryRowContext(context.Background(),
		`SELECT outcome, class, reason, arg_keys_json, error_redacted FROM tool_attempts WHERE tool_name = 'agent_note'`).Scan(
		&outcome, &class, &reason, &argKeys, &errRedacted,
	); err != nil {
		t.Fatalf("query validation attempt: %v", err)
	}
	if outcome != "recoverable" || class != "validation" || reason != "schema_validation" {
		t.Fatalf("attempt classification = outcome=%q class=%q reason=%q", outcome, class, reason)
	}
	if argKeys != `["action","note"]` {
		t.Fatalf("arg keys = %s", argKeys)
	}
	if strings.Contains(errRedacted, "wrong key") {
		t.Fatalf("error_redacted leaked raw arg value: %q", errRedacted)
	}
}

func TestToolRegistryCallMapsMissingSkillToNotFoundAndPersistsAttempt(t *testing.T) {
	reader := newAttemptsTestReader(t)
	executor := &fakeToolExecutor{
		names: []string{"skill"},
		err:   fmt.Errorf("skill %q: %w", "definitely-missing", os.ErrNotExist),
	}
	router := NewRouter(Deps{Tools: executor, ToolProbeRecorder: NewSQLiteToolProbeRecorder(reader.db)})
	req := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(`{
		"name":"skill",
		"args":{"action":"info","name":"definitely-missing"}
	}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var outcome, class, reason, argKeys, errRedacted string
	if err := reader.db.QueryRowContext(context.Background(),
		`SELECT outcome, class, reason, arg_keys_json, error_redacted FROM tool_attempts WHERE tool_name = 'skill'`).Scan(
		&outcome, &class, &reason, &argKeys, &errRedacted,
	); err != nil {
		t.Fatalf("query missing-skill attempt: %v", err)
	}
	if outcome != "recoverable" || class != "not_found" || reason != "not_found" {
		t.Fatalf("attempt classification = outcome=%q class=%q reason=%q", outcome, class, reason)
	}
	if argKeys != `["action","name"]` {
		t.Fatalf("arg keys = %s", argKeys)
	}
	if strings.Contains(errRedacted, "definitely-missing") || strings.Contains(errRedacted, "info") {
		t.Fatalf("error_redacted leaked raw arg value: %q", errRedacted)
	}
}
