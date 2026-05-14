package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// testServer creates a mock Qdrant HTTP server and a Client pointed at it.
func testServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewClient(Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return srv, client
}

func TestHealth_OK(t *testing.T) {
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Health(ctx); err != nil {
		t.Fatalf("Health() unexpected error: %v", err)
	}
}

func TestHealth_Error(t *testing.T) {
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"error"}`))
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Health(ctx); err == nil {
		t.Fatal("Health() expected error on 503, got nil")
	}
}

func TestWaitForReady_Immediate(t *testing.T) {
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitForReady(ctx, client, 2*time.Second); err != nil {
		t.Fatalf("WaitForReady() unexpected error: %v", err)
	}
}

func TestWaitForReady_Timeout(t *testing.T) {
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := WaitForReady(ctx, client, 200*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WaitForReady() expected error on timeout, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("WaitForReady() took %v, expected < 2s", elapsed)
	}
	errMsg := err.Error()
	found := false
	for _, phrase := range []string{"not ready after", "not ready"} {
		if containsStr(errMsg, phrase) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WaitForReady() error %q does not contain 'not ready after'", errMsg)
	}
}

func TestWaitForReady_EventualSuccess(t *testing.T) {
	var callCount atomic.Int32
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			n := callCount.Add(1)
			if n <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusOK)
			}
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitForReady(ctx, client, 5*time.Second); err != nil {
		t.Fatalf("WaitForReady() expected eventual success, got: %v", err)
	}
}

func TestCollectionInfo_Success(t *testing.T) {
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collections/mytest" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"status":                "green",
					"points_count":          42,
					"indexed_vectors_count": 42,
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := client.CollectionInfo(ctx, "mytest")
	if err != nil {
		t.Fatalf("CollectionInfo() unexpected error: %v", err)
	}
	if info.Status != "green" {
		t.Errorf("CollectionInfo().Status = %q, want %q", info.Status, "green")
	}
	if info.PointsCount != 42 {
		t.Errorf("CollectionInfo().PointsCount = %d, want 42", info.PointsCount)
	}
}

func TestCollectionInfo_NotFound(t *testing.T) {
	_, client := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collections/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := client.CollectionInfo(ctx, "missing")
	if err != nil {
		t.Fatalf("CollectionInfo() on 404 must not return error (Pitfall 3), got: %v", err)
	}
	if info.PointsCount != 0 {
		t.Errorf("CollectionInfo() on 404: PointsCount = %d, want 0", info.PointsCount)
	}
}

func TestNewClient_EmptyBaseURL(t *testing.T) {
	_, err := NewClient(Config{BaseURL: ""})
	if err == nil {
		t.Fatal("NewClient() with empty BaseURL must return error, got nil")
	}
}

func TestUpsert(t *testing.T) {
	var requestCount atomic.Int32
	var capturedBodies []map[string]any
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collections/test/points" && r.Method == http.MethodPut {
			// Verify query param
			if r.URL.RawQuery != "wait=true" {
				http.Error(w, "missing wait=true", http.StatusBadRequest)
				return
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			<-mu
			capturedBodies = append(capturedBodies, body)
			mu <- struct{}{}
			requestCount.Add(1)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
			return
		}
		http.NotFound(w, r)
	})

	_, client := testServer(t, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send 100 points -- should result in 2 batches (64 + 36)
	points := make([]Point, 100)
	for i := range points {
		points[i] = Point{
			ID:      "id",
			Vector:  []float32{0.1, 0.2},
			Payload: map[string]string{"k": "v"},
		}
	}
	if err := client.Upsert(ctx, "test", points); err != nil {
		t.Fatalf("Upsert() unexpected error: %v", err)
	}
	if requestCount.Load() != 2 {
		t.Errorf("Upsert() sent %d requests, want 2 (batch split)", requestCount.Load())
	}

	// Verify body has "points" key
	<-mu
	defer func() { mu <- struct{}{} }()
	for i, body := range capturedBodies {
		if _, ok := body["points"]; !ok {
			t.Errorf("Upsert() batch %d body missing 'points' key", i)
		}
	}
}

func TestSearch(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collections/test/points/query" && r.Method == http.MethodPost {
			// Verify request body has query and limit
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["query"]; !ok {
				http.Error(w, "missing query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"result": map[string]any{
					"points": []map[string]any{
						{"id": "1", "score": 0.95, "payload": map[string]string{"content": "test"}},
						{"id": "2", "score": 0.88, "payload": map[string]string{"content": "test2"}},
						{"id": "3", "score": 0.80, "payload": map[string]string{"content": "test3"}},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	_, client := testServer(t, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vec := []float32{0.1, 0.2, 0.3}
	results, err := client.Search(ctx, "test", vec, 5, true)
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Search() returned %d points, want 3", len(results))
	}
}

// containsStr is a simple substring check helper.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
