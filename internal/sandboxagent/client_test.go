package sandboxagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientRunPostsOneShotCommand(t *testing.T) {
	var got RunRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/processes/run" {
			t.Fatalf("request = %s %s, want POST /v1/processes/run", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(RunResponse{
			Stdout:          "42\n",
			Stderr:          "",
			ExitCode:        intPtr(0),
			TimedOut:        false,
			StdoutTruncated: false,
			StderrTruncated: false,
			DurationMs:      12,
		})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TimeoutSec: 5})
	out, err := c.Run(context.Background(), RunRequest{
		Command:        "python",
		Args:           []string{"-c", "print(40+2)"},
		Cwd:            "/workspace",
		TimeoutMs:      int64Ptr(30000),
		MaxOutputBytes: intPtr(4096),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Command != "python" || len(got.Args) != 2 || got.Cwd != "/workspace" {
		t.Fatalf("request not threaded: %+v", got)
	}
	if out.Stdout != "42\n" || out.ExitCode == nil || *out.ExitCode != 0 {
		t.Fatalf("response not decoded: %+v", out)
	}
}

func TestClientRunHTTPErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "process API unavailable", http.StatusNotImplemented)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TimeoutSec: 5})
	_, err := c.Run(context.Background(), RunRequest{Command: "true"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "HTTP 501") || !strings.Contains(err.Error(), "process API unavailable") {
		t.Fatalf("error should include status and body, got %q", err.Error())
	}
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
