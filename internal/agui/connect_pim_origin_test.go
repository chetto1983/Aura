package agui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPIMOriginFallsBackToTheRequestsOwnAddress(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/x", nil)
	if got := pimRequestOrigin(req); got != "http://localhost:8080" {
		t.Fatalf("origin = %q", got)
	}
}

func TestPIMOriginHonoursTheForwardedScheme(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "http://aura.example/x", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := pimRequestOrigin(req); got != "https://aura.example" {
		t.Fatalf("origin = %q", got)
	}
}
