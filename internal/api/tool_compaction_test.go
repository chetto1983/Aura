package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tokenJuiceFixtureInput struct {
	ToolName string   `json:"toolName"`
	Argv     []string `json:"argv"`
	Command  string   `json:"command,omitempty"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr,omitempty"`
	ExitCode *int     `json:"exitCode,omitempty"`
}

type tokenJuiceFixture struct {
	Description         string                 `json:"description"`
	Input               tokenJuiceFixtureInput `json:"input"`
	ExpectApplied       *bool                  `json:"expectApplied,omitempty"`
	ExpectedContains    []string               `json:"expectedContains,omitempty"`
	ExpectedNotContains []string               `json:"expectedNotContains,omitempty"`
}

func readTokenJuiceFixture(t *testing.T, name string) tokenJuiceFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "tokenjuice", "tests", "fixtures", name))
	if err != nil {
		t.Fatalf("read tokenjuice fixture %s: %v", name, err)
	}
	var fix tokenJuiceFixture
	if err := json.Unmarshal(data, &fix); err != nil {
		t.Fatalf("parse tokenjuice fixture %s: %v", name, err)
	}
	return fix
}

func postToolCompaction(t *testing.T, body any) (toolCompactionResponse, int, string) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	router := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodPost, "/tools/compact", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp toolCompactionResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp, w.Code, w.Body.String()
}

func TestHandleToolCompactTokenJuiceGoTestFixture(t *testing.T) {
	fix := readTokenJuiceFixture(t, "go_test_failure.json")
	req := toolCompactionRequest{
		Mode:     "tokenjuice",
		ToolName: fix.Input.ToolName,
		Argv:     fix.Input.Argv,
		Command:  fix.Input.Command,
		Stdout:   fix.Input.Stdout,
		Stderr:   fix.Input.Stderr,
		ExitCode: fix.Input.ExitCode,
		MinRatio: 0.95,
	}

	resp, code, body := postToolCompaction(t, req)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	if !resp.Applied {
		t.Fatalf("expected compaction to apply for %s: %+v", fix.Description, resp)
	}
	if resp.RuleID != "tests/go-test" {
		t.Fatalf("rule_id = %q, want tests/go-test", resp.RuleID)
	}
	if resp.Stats.Family != "test-results" {
		t.Fatalf("family = %q, want test-results", resp.Stats.Family)
	}
	if resp.Stats.OriginalBytes <= resp.Stats.CompactedBytes {
		t.Fatalf("expected bytes to shrink: %+v", resp.Stats)
	}
	for _, want := range fix.ExpectedContains {
		if !strings.Contains(resp.InlineText, want) {
			t.Fatalf("inline_text missing %q:\n%s", want, resp.InlineText)
		}
	}
	for _, forbid := range fix.ExpectedNotContains {
		if strings.Contains(resp.InlineText, forbid) {
			t.Fatalf("inline_text should not contain %q:\n%s", forbid, resp.InlineText)
		}
	}
}

func TestHandleToolCompactTokenJuiceGitStatusFixture(t *testing.T) {
	fix := readTokenJuiceFixture(t, "git_status_modified.json")
	req := toolCompactionRequest{
		Mode:     "tokenjuice",
		ToolName: fix.Input.ToolName,
		Argv:     fix.Input.Argv,
		Stdout:   fix.Input.Stdout,
	}

	resp, code, body := postToolCompaction(t, req)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	if !resp.Applied {
		t.Fatalf("expected git status fixture to compact: %+v", resp)
	}
	if resp.RuleID != "git/status" {
		t.Fatalf("rule_id = %q, want git/status", resp.RuleID)
	}
	for _, want := range fix.ExpectedContains {
		if !strings.Contains(resp.InlineText, want) {
			t.Fatalf("inline_text missing %q:\n%s", want, resp.InlineText)
		}
	}
	for _, forbid := range fix.ExpectedNotContains {
		if strings.Contains(resp.InlineText, forbid) {
			t.Fatalf("inline_text should not contain %q:\n%s", forbid, resp.InlineText)
		}
	}
}

func TestHandleToolCompactToolResultPreviewRedactsSecretLines(t *testing.T) {
	content := strings.Join([]string{
		"source_id=src_0123456789abcdef",
		"title=Aura operator runbook",
		"Authorization: Bearer token-one",
		"password=super-secret-password",
		strings.Repeat("section: wiki retrieval and source citation\n", 40),
	}, "\n")
	req := toolCompactionRequest{
		Mode:           "tool_result",
		ToolName:       "source",
		Content:        content,
		MaxInlineChars: 420,
	}

	resp, code, body := postToolCompaction(t, req)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	if !resp.Applied {
		t.Fatalf("expected tool_result compaction to apply: %+v", resp)
	}
	for _, want := range []string{"[tool result compacted]", "tool: source", "original_chars:", "source_id=src_0123456789abcdef"} {
		if !strings.Contains(resp.InlineText, want) {
			t.Fatalf("inline_text missing %q:\n%s", want, resp.InlineText)
		}
	}
	for _, leaked := range []string{"token-one", "super-secret-password", "Authorization: Bearer"} {
		if strings.Contains(resp.InlineText, leaked) {
			t.Fatalf("inline_text leaked %q:\n%s", leaked, resp.InlineText)
		}
	}
	if resp.Stats.OriginalBytes <= resp.Stats.CompactedBytes {
		t.Fatalf("expected preview to shrink realistic payload: %+v", resp.Stats)
	}
}

func TestHandleToolCompactRejectsInvalidContract(t *testing.T) {
	resp, code, body := postToolCompaction(t, toolCompactionRequest{
		Mode:     "unknown",
		ToolName: "execute_shell",
		Stdout:   strings.Repeat("line\n", 200),
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; resp=%+v body=%s", code, resp, body)
	}
	if !strings.Contains(body, "unsupported mode") {
		t.Fatalf("error body missing unsupported mode: %s", body)
	}
}
