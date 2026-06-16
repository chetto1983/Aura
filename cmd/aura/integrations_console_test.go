package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleHandlerServesPageAndMountsProxy(t *testing.T) {
	ts := httptest.NewServer(newConsoleHandler())
	t.Cleanup(ts.Close)

	t.Run("GET / -> the validation console HTML", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q, want text/html", ct)
		}
		body := string(raw)
		for _, want := range []string{"MCP Integrations Validation", "/api/integrations", "/whatsapp/status", "/calendar/accounts"} {
			if !strings.Contains(body, want) {
				t.Fatalf("console HTML missing %q", want)
			}
		}
	})

	t.Run("proxy mounted: unknown integration -> 404", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/integrations/bogus/x")
		if err != nil {
			t.Fatalf("GET proxy: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (proxy dispatcher mounted)", resp.StatusCode)
		}
	})

	t.Run("GET /nope -> 404 (only / serves the page)", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/nope")
		if err != nil {
			t.Fatalf("GET /nope: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}
