package httputil

import (
	"testing"
	"time"
)

func TestNewHTTPClient_ExplicitTimeoutPassesThrough(t *testing.T) {
	got := NewHTTPClient(7*time.Second, 30*time.Second)
	if got.Timeout != 7*time.Second {
		t.Fatalf("Timeout = %v, want 7s", got.Timeout)
	}
}

func TestNewHTTPClient_ZeroUsesFallback(t *testing.T) {
	got := NewHTTPClient(0, 30*time.Second)
	if got.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s fallback", got.Timeout)
	}
}

func TestNewHTTPClient_NegativeUsesFallback(t *testing.T) {
	got := NewHTTPClient(-5*time.Second, 30*time.Second)
	if got.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s fallback for negative input", got.Timeout)
	}
}

func TestNewHTTPClient_BothZeroReturnsNoTimeout(t *testing.T) {
	got := NewHTTPClient(0, 0)
	if got.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0 when both inputs are 0", got.Timeout)
	}
}
