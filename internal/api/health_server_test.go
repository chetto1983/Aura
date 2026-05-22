package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type mockProvider struct {
	status string
	detail string
}

func (m *mockProvider) HealthStatus() HealthComponent {
	return HealthComponent{Status: m.status, Detail: m.detail}
}

func TestStatusEndpointHealthy(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
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
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
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
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)

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
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
	s.SetBotUsername("@aura_test_bot")

	req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
	w := httptest.NewRecorder()
	s.handleTelegram(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp HealthTelegramInfo
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
	if resp.QRURL != "/telegram/qr.png" {
		t.Errorf("qr_url = %q", resp.QRURL)
	}
}

func TestTelegramEndpointUnavailable(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
	w := httptest.NewRecorder()
	s.handleTelegram(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestTelegramEndpointRejectsInvalidUsername(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
	s.SetBotUsername("../bad")

	req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
	w := httptest.NewRecorder()
	s.handleTelegram(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestTelegramQREndpoint(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
	s.SetBotUsername("aura_test_bot")

	req := httptest.NewRequest(http.MethodGet, "/telegram/qr.png", nil)
	w := httptest.NewRecorder()
	s.handleTelegramQR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if body := w.Body.Bytes(); len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("body is not a PNG, first bytes %q", body[:min(len(body), 8)])
	}
}

func TestTelegramQREndpointHead(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
	s.SetBotUsername("aura_test_bot")

	req := httptest.NewRequest(http.MethodHead, "/telegram/qr.png", nil)
	w := httptest.NewRecorder()
	s.handleTelegramQR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", w.Body.Len())
	}
}

func TestTelegramEndpointRejectsUnsupportedMethods(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
	s.SetBotUsername("aura_test_bot")

	for _, tc := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"metadata", "/telegram", s.handleTelegram},
		{"qr", "/telegram/qr.png", s.handleTelegramQR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			w := httptest.NewRecorder()
			tc.handler(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestUptimeInStatus(t *testing.T) {
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, nil)
	time.Sleep(10 * time.Millisecond) // small delay to ensure uptime > 0

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	var status HealthStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.Uptime == "" {
		t.Error("uptime should not be empty")
	}
}

// Slice 10b deleted the QR landing page. The two TestLandingPage* tests
// that lived here previously asserted the QR/HTML response shape; they're
// removed alongside the handler. The dashboard SPA owns / now and is
// covered by internal/api/static_test.go.

// failResponseWriter is an http.ResponseWriter whose Write always returns an error.
type failResponseWriter struct {
	header   http.Header
	writeErr error
}

func (f *failResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}

func (f *failResponseWriter) Write(_ []byte) (int, error) { return 0, f.writeErr }
func (f *failResponseWriter) WriteHeader(_ int)           {}

// captureSlogHandler captures slog message strings for test assertions.
type captureSlogHandler struct {
	msgs []string
}

func (h *captureSlogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureSlogHandler) Handle(_ context.Context, r slog.Record) error {
	h.msgs = append(h.msgs, r.Message)
	return nil
}
func (h *captureSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureSlogHandler) WithGroup(_ string) slog.Handler      { return h }

func TestHealthWriteErrorLogged(t *testing.T) {
	handler := &captureSlogHandler{}
	logger := slog.New(handler)
	s := NewHealthServer(HealthServerConfig{Addr: ":0"}, logger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := &failResponseWriter{writeErr: errors.New("broken pipe")}
	s.handleHealth(w, req)

	found := false
	for _, msg := range handler.msgs {
		if msg == "health: response write failed" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'health: response write failed' warn log, got: %v", handler.msgs)
	}
}
