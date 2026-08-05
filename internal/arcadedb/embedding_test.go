package arcadedb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSidecarEmbedderNormalizesConfiguration(t *testing.T) {
	if got := NewSidecarEmbedder("  ", "model", "key", 0); got != nil {
		t.Fatalf("blank endpoint produced %+v", got)
	}
	got := NewSidecarEmbedder(" http://embed.test/v1/// ", " model ", " key ", 0)
	if got.BaseURL != "http://embed.test/v1" || got.Model != "model" || got.APIKey != "key" {
		t.Fatalf("embedder = %+v", got)
	}
	if got.Client.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", got.Client.Timeout)
	}
	explicit := NewSidecarEmbedder("http://embed.test", "", "", time.Second)
	if explicit.Client.Timeout != time.Second {
		t.Fatalf("explicit timeout = %v", explicit.Client.Timeout)
	}
}

func TestSidecarEmbedderUsesOpenAIWireAndResponseIndexes(t *testing.T) {
	var path, auth string
	var payload struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = io.WriteString(w, `{"data":[
			{"index":1,"embedding":[3,4]},
			{"index":0,"embedding":[1,2]}]}`)
	}))
	t.Cleanup(srv.Close)
	embedder := NewSidecarEmbedder(srv.URL+"/v1", "embeddinggemma", "secret", time.Second)
	embedder.Dimensions = 2
	got, err := embedder.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if path != "/v1/embeddings" || auth != "Bearer secret" || payload.Model != "embeddinggemma" {
		t.Fatalf("path=%q auth=%q payload=%+v", path, auth, payload)
	}
	if len(got) != 2 || got[0][0] != 1 || got[1][0] != 3 {
		t.Fatalf("vectors = %v", got)
	}
	if empty, err := embedder.Embed(context.Background(), nil); err != nil || empty != nil {
		t.Fatalf("empty embed = %v, %v", empty, err)
	}
	var none *SidecarEmbedder
	if _, err := none.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("nil embedder accepted a request")
	}
}

func TestSidecarEmbedderRejectsBadResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "status", status: http.StatusServiceUnavailable, body: `{}`},
		{name: "malformed", status: http.StatusOK, body: `{"data":`},
		{name: "wrong cardinality", status: http.StatusOK, body: `{"data":[]}`},
		{name: "bad index", status: http.StatusOK, body: `{"data":[{"index":2,"embedding":[1]}]}`},
		{name: "missing vector", status: http.StatusOK, body: `{"data":[{"index":0,"embedding":[]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)
			embedder := NewSidecarEmbedder(srv.URL+"/embeddings", "", "", time.Second)
			embedder.Dimensions = 1
			if _, err := embedder.Embed(context.Background(), []string{"x"}); err == nil {
				t.Fatal("bad response accepted")
			}
		})
	}
}

func TestSidecarEmbedderWrapsTransportAndRequestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := NewSidecarEmbedder(url, "", "", time.Second).Embed(context.Background(), []string{"x"}); err == nil || !strings.Contains(err.Error(), "embed") {
		t.Fatalf("transport error = %v", err)
	}
	broken := &SidecarEmbedder{BaseURL: "://bad\n", Client: http.DefaultClient}
	if _, err := broken.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("invalid request URL accepted")
	}
}

func TestWithTaskReturnsPrefixedCopy(t *testing.T) {
	input := []string{"one", "two"}
	got := withTask("prefix: ", input)
	if strings.Join(got, "|") != "prefix: one|prefix: two" {
		t.Fatalf("got = %v", got)
	}
	if input[0] != "one" {
		t.Fatalf("input mutated: %v", input)
	}
}
