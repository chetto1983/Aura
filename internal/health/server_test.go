package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type mockProvider struct {
	status string
	detail string
}

func (m *mockProvider) HealthStatus() ComponentHealth {
	return ComponentHealth{Status: m.status, Detail: m.detail}
}

func TestStatusEndpointHealthy(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)
	s.RegisterProvider("test", &mockProvider{status: "ok"})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var status HealthStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Status != "ok" {
		t.Errorf("status = %q, want %q", status.Status, "ok")
	}
	if status.Components["test"].Status != "ok" {
		t.Errorf("component status = %q, want %q", status.Components["test"].Status, "ok")
	}
}

func TestStatusEndpointDegraded(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)
	s.RegisterProvider("db", &mockProvider{status: "error", detail: "connection refused"})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var status HealthStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Status != "degraded" {
		t.Errorf("status = %q, want %q", status.Status, "degraded")
	}
}

func TestHealthEndpoint(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "alive" {
		t.Errorf("status = %q, want %q", resp["status"], "alive")
	}
}

func TestTelegramEndpoint(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)
	s.SetBotUsername("@aura_test_bot")

	req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
	w := httptest.NewRecorder()
	s.handleTelegram(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp TelegramInfo
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Username != "aura_test_bot" {
		t.Errorf("username = %q, want aura_test_bot", resp.Username)
	}
	if resp.URL != "https://t.me/aura_test_bot" {
		t.Errorf("url = %q", resp.URL)
	}
	if resp.StartURL != "https://t.me/aura_test_bot?start=login" {
		t.Errorf("start_url = %q", resp.StartURL)
	}
}

func TestTelegramEndpointUnavailable(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
	w := httptest.NewRecorder()
	s.handleTelegram(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestUptimeInStatus(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)
	time.Sleep(10 * time.Millisecond) // small delay to ensure uptime > 0

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	var status HealthStatus
	json.NewDecoder(w.Body).Decode(&status)
	if status.Uptime == "" {
		t.Error("uptime should not be empty")
	}
}

// Slice 10b deleted the QR landing page. The two TestLandingPage* tests
// that lived here previously asserted the QR/HTML response shape; they're
// removed alongside the handler. The dashboard SPA owns / now and is
// covered by internal/api/static_test.go.
