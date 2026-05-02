package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeDotEnvValue(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"simple", "simple"},
		{"with space", `"with space"`},
		{"has#hash", `"has#hash"`},
		{`has"quote`, `"has\"quote"`},
		{`back\slash with space`, `"back\\slash with space"`},
		{"line\nbreak", "\"line\nbreak\""},
	}
	for _, tt := range tests {
		got := encodeDotEnvValue(tt.in)
		if got != tt.want {
			t.Errorf("encodeDotEnvValue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteDotEnvKeyAppendsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	if err := writeDotEnvKey(path, "FOO", "bar"); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "FOO=bar\n") {
		t.Errorf("expected FOO=bar, got: %s", data)
	}
}

func TestWriteDotEnvKeyReplacesExistingPreservingOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	initial := "# Header comment\nFOO=old\nBAR=keepme\n# trailing\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := writeDotEnvKey(path, "FOO", "new"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := os.ReadFile(path)
	gotStr := string(got)

	if !strings.Contains(gotStr, "FOO=new") {
		t.Errorf("FOO not updated: %s", gotStr)
	}
	if strings.Contains(gotStr, "FOO=old") {
		t.Errorf("FOO=old still present: %s", gotStr)
	}
	if !strings.Contains(gotStr, "BAR=keepme") {
		t.Errorf("BAR removed: %s", gotStr)
	}
	if !strings.Contains(gotStr, "# Header comment") {
		t.Errorf("comment removed: %s", gotStr)
	}
	if !strings.Contains(gotStr, "# trailing") {
		t.Errorf("trailing comment removed: %s", gotStr)
	}
}

func TestWriteDotEnvKeyAtomicCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("setup: .env should not exist yet")
	}
	if err := writeDotEnvKey(path, "TELEGRAM_TOKEN", "123:ABC"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestProbeProviderConnectFailure(t *testing.T) {
	res := ProbeProvider(context.Background(), "http://127.0.0.1:1", "key", "/models")
	if res.OK {
		t.Errorf("expected OK=false on bad port, got %+v", res)
	}
	if res.Error == "" {
		t.Errorf("expected error message")
	}
}

func TestProbeProvider401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	res := ProbeProvider(context.Background(), srv.URL, "wrong-key", "/v1/models")
	if res.OK {
		t.Errorf("expected OK=false on 401, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.Error), "auth") {
		t.Errorf("expected auth error, got %q", res.Error)
	}
}

func TestProbeProviderHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing bearer header: %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	res := ProbeProvider(context.Background(), srv.URL, "sk-x", "/v1/models")
	if !res.OK {
		t.Errorf("expected OK=true, got %+v", res)
	}
	if len(res.Models) != 2 || res.Models[0] != "gpt-4o-mini" {
		t.Errorf("models = %v, want [gpt-4o-mini, gpt-4o]", res.Models)
	}
}

func TestProbeProviderNoBaseURL(t *testing.T) {
	res := ProbeProvider(context.Background(), "  ", "k", "/v1/models")
	if res.OK || res.Error == "" {
		t.Errorf("expected error for blank base URL, got %+v", res)
	}
}

func TestProbeProviderNonStandardResponseStillOK(t *testing.T) {
	// A provider that returns 200 but a body we can't parse as a model
	// list should still be reported OK — we connected; the user might
	// just be using a non-standard endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv.Close()

	res := ProbeProvider(context.Background(), srv.URL, "k", "/v1/models")
	if !res.OK {
		t.Errorf("expected OK=true for connect-but-unparseable, got %+v", res)
	}
	if len(res.Models) != 0 {
		t.Errorf("expected empty model list, got %v", res.Models)
	}
}

func TestPresetByID(t *testing.T) {
	if p, ok := PresetByID("openai"); !ok || p.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("PresetByID(openai) wrong: %+v", p)
	}
	if _, ok := PresetByID("does-not-exist"); ok {
		t.Errorf("PresetByID returned ok for unknown id")
	}
}

func TestLoopbackOnly(t *testing.T) {
	tests := map[string]string{
		"":                   "127.0.0.1:8080",
		":8081":              "127.0.0.1:8081",
		"0.0.0.0:8081":       "127.0.0.1:8081",
		"127.0.0.1:8090":     "127.0.0.1:8090",
		"192.168.1.5:8081":   "192.168.1.5:8081", // explicit non-loopback host preserved (operator's choice)
		"::":                 "127.0.0.1:8080",
		"garbage-no-port":    "127.0.0.1:8080",
	}
	for in, want := range tests {
		got := loopbackOnly(in)
		if got != want {
			t.Errorf("loopbackOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
