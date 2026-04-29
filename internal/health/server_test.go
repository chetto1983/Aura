package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLandingPageWithoutUsername(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleLanding(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestLandingPageWithUsername(t *testing.T) {
	s := NewServer(ServerConfig{Addr: ":0", Version: "3.0"}, nil)
	s.SetBotUsername("aura_test_bot")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleLanding(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "https://t.me/aura_test_bot?start=invite") {
		t.Error("response should contain Telegram link with start=invite")
	}
	if !strings.Contains(body, "data:image/png;base64,") {
		t.Error("response should contain base64 QR code image")
	}
	if !strings.Contains(body, "Skills") {
		t.Error("response should contain Skills section")
	}
	if !strings.Contains(body, "MCP") {
		t.Error("response should contain MCP section")
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}