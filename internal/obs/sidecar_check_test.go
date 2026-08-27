package obs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSidecarCheckerRequiresSuccessfulAuraScrape(t *testing.T) {
	var scrapeValue = "1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ready", "/-/ready", "/api/health", "/api/datasources/uid/aura-tempo/health":
			_, _ = w.Write([]byte("ready"))
		case "/api/v1/query", "/api/datasources/proxy/uid/aura-prometheus/api/v1/query":
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{"job":"aura"},"value":[1,"` + scrapeValue + `"]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	checker := NewSidecarChecker(server.Client(), server.URL, server.URL, server.URL)
	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("healthy Check: %v", err)
	}
	scrapeValue = "0"
	if err := checker.Check(context.Background()); err == nil || !strings.Contains(err.Error(), `up{job="aura"} != 1`) {
		t.Fatalf("blind Prometheus error = %v", err)
	}
}

func TestSidecarCheckerRequiresGrafanaDatasourcePath(t *testing.T) {
	grafanaValue := "1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ready", "/-/ready", "/api/health":
			_, _ = w.Write([]byte("ready"))
		case "/api/datasources/uid/aura-tempo/health":
			_, _ = w.Write([]byte(`{"message":"Data source is working","status":"OK"}`))
		case "/api/v1/query":
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{"job":"aura"},"value":[1,"1"]}]}}`))
		case "/api/datasources/proxy/uid/aura-prometheus/api/v1/query":
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{"job":"aura"},"value":[1,"` + grafanaValue + `"]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	checker := NewSidecarChecker(server.Client(), server.URL, server.URL, server.URL)
	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("healthy Check: %v", err)
	}
	grafanaValue = "0"
	if err := checker.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "Grafana Prometheus datasource") {
		t.Fatalf("broken Grafana datasource error = %v", err)
	}
}

func TestSidecarCheckerSurfacesReadinessAndBoundsResponses(t *testing.T) {
	t.Run("readiness", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "warming", http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)
		err := NewSidecarChecker(server.Client(), server.URL, server.URL, server.URL).Check(context.Background())
		if err == nil || !strings.Contains(err.Error(), "tempo readiness") || !strings.Contains(err.Error(), "503") {
			t.Fatalf("readiness error = %v", err)
		}
	})

	t.Run("response cap", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", sidecarResponseLimit+1)))
		}))
		t.Cleanup(server.Close)
		err := NewSidecarChecker(server.Client(), server.URL, server.URL, server.URL).Check(context.Background())
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("bounded error = %v", err)
		}
	})

	t.Run("context deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		t.Cleanup(server.Close)
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		if err := NewSidecarChecker(server.Client(), server.URL, server.URL, server.URL).Check(ctx); err == nil {
			t.Fatal("deadline Check returned nil")
		}
	})
}
