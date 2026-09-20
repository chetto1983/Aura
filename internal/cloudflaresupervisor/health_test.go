package cloudflaresupervisor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthAndStatusDoNotExposeSecrets(t *testing.T) {
	s := NewSupervisor(t.TempDir(), &fakeLauncher{}, Options{})
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	if !Healthcheck(context.Background(), server.URL+"/healthz") {
		t.Fatal("idle must be healthy")
	}
	for _, status := range []Status{{State: "degraded", ErrorCode: "candidate_not_ready"}, {State: "healthy", Ready: true, Generation: 2, ActiveGeneration: 2}} {
		s.mu.Lock()
		s.status = status
		s.mu.Unlock()
		if got := Healthcheck(context.Background(), server.URL+"/healthz"); got != status.Ready {
			t.Fatalf("health = %v", got)
		}
		response, err := http.Get(server.URL + "/status")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"token", "secret", "hash", "path", s.root} {
			if strings.Contains(string(body), forbidden) {
				t.Fatalf("status leaked %q", forbidden)
			}
		}
	}
	if Healthcheck(context.Background(), "://bad") || Healthcheck(context.Background(), "http://127.0.0.1:1") {
		t.Fatal("invalid health endpoint accepted")
	}
}
