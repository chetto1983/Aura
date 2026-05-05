package tray

import "testing"

func TestValidateDashboardURLAcceptsHTTPAndHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"https://example.local/dashboard",
	} {
		if _, err := validateDashboardURL(raw); err != nil {
			t.Fatalf("validateDashboardURL(%q): %v", raw, err)
		}
	}
}

func TestValidateDashboardURLRejectsUnsafeValues(t *testing.T) {
	for _, raw := range []string{
		"",
		"javascript:alert(1)",
		"file:///C:/Windows/System32/calc.exe",
		"http://",
		"://missing-scheme",
	} {
		if _, err := validateDashboardURL(raw); err == nil {
			t.Fatalf("validateDashboardURL(%q) error = nil, want error", raw)
		}
	}
}
