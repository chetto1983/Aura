package sandboxagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewDefaultsAndTrimsBaseURL(t *testing.T) {
	c := New(Config{BaseURL: "http://sandbox.local///", TimeoutSec: 0})

	if c.baseURL != "http://sandbox.local" {
		t.Fatalf("baseURL = %q, want trimmed URL", c.baseURL)
	}
	if c.http.Timeout != time.Duration(DefaultTimeoutSec)*time.Second {
		t.Fatalf("timeout = %s, want default %ds", c.http.Timeout, DefaultTimeoutSec)
	}

	defaulted := New(Config{})
	if defaulted.baseURL != DefaultBaseURL {
		t.Fatalf("default baseURL = %q, want %q", defaulted.baseURL, DefaultBaseURL)
	}
}

func TestClientRunPostsOneShotCommand(t *testing.T) {
	var got RunRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/processes/run" {
			t.Fatalf("request = %s %s, want POST /v1/processes/run", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers = Content-Type:%q Accept:%q", r.Header.Get("Content-Type"), r.Header.Get("Accept"))
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

func TestClientSendsBearerToken(t *testing.T) {
	const token = "sandbox-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		switch r.URL.Path {
		case "/v1/processes/run":
			_ = json.NewEncoder(w).Encode(RunResponse{ExitCode: intPtr(0)})
		case "/v1/health":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TimeoutSec: 5, Token: token})
	if _, err := c.Run(context.Background(), RunRequest{Command: "true"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClientRunRequestBuildError(t *testing.T) {
	c := New(Config{BaseURL: "http://[::1"})

	_, err := c.Run(context.Background(), RunRequest{Command: "true"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "run request") {
		t.Fatalf("error = %q, want request-build context", err.Error())
	}
}

func TestClientRunTransportError(t *testing.T) {
	want := errors.New("dial blocked")
	c := &Client{
		baseURL: "http://sandbox.local",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, want
		})},
	}

	_, err := c.Run(context.Background(), RunRequest{Command: "true"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "sandbox-agent run:") || !errors.Is(err, want) {
		t.Fatalf("error should wrap transport failure, got %q", err.Error())
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

func TestClientRunDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TimeoutSec: 5})
	_, err := c.Run(context.Background(), RunRequest{Command: "true"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "run decode") {
		t.Fatalf("error should include decode context, got %q", err.Error())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
