package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflaresupervisor"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestHealthcheckCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	if run(context.Background(), []string{"healthcheck", server.URL}) != 0 {
		t.Fatal("healthy command failed")
	}
	if run(context.Background(), []string{"healthcheck", "http://127.0.0.1:1"}) != 1 {
		t.Fatal("unhealthy command succeeded")
	}
	if run(context.Background(), []string{"invalid"}) != 2 {
		t.Fatal("invalid command succeeded")
	}
}

func TestServerShutdownAndBindFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if run(ctx, nil) != 0 {
		t.Fatal("default daemon did not shut down")
	}
	s := cloudflaresupervisor.NewSupervisor(t.TempDir(), nil, cloudflaresupervisor.Options{})
	if serve(ctx, "127.0.0.1:0", s) != 0 {
		t.Fatal("cancelled server failed")
	}
	if serve(context.Background(), "invalid", s) != 1 {
		t.Fatal("bind failure not reported")
	}
}
