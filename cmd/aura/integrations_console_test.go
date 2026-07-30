package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleBindGuard(t *testing.T) {
	tests := []struct {
		name         string
		addr         string
		unsafe       bool
		token        string
		wantAuth     bool
		wantErrMatch string
	}{
		{name: "IPv4 loopback", addr: "127.0.0.1:9099"},
		{name: "localhost", addr: "localhost:9099"},
		{name: "IPv6 loopback", addr: "[::1]:9099"},
		{name: "wildcard denied by default", addr: ":9099", wantErrMatch: "--unsafe-non-loopback"},
		{name: "IPv4 wildcard denied by default", addr: "0.0.0.0:9099", wantErrMatch: "--unsafe-non-loopback"},
		{name: "remote denied by default", addr: "192.0.2.1:9099", wantErrMatch: "--unsafe-non-loopback"},
		{name: "unsafe requires token", addr: "0.0.0.0:9099", unsafe: true, wantErrMatch: "AURA_INTEGRATIONS_CONSOLE_TOKEN"},
		{name: "unsafe remote requires token", addr: "192.0.2.1:9099", unsafe: true, wantErrMatch: "AURA_INTEGRATIONS_CONSOLE_TOKEN"},
		{name: "unsafe with token enables auth", addr: "0.0.0.0:9099", unsafe: true, token: "console-secret", wantAuth: true},
		{name: "malformed denied by default", addr: "not-an-address", wantErrMatch: "--unsafe-non-loopback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAuth, err := consoleBindGuard(tt.addr, tt.unsafe, tt.token)
			if tt.wantErrMatch == "" {
				if err != nil {
					t.Fatalf("consoleBindGuard() error = %v", err)
				}
				if gotAuth != tt.wantAuth {
					t.Fatalf("consoleBindGuard() require auth = %v, want %v", gotAuth, tt.wantAuth)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrMatch) {
				t.Fatalf("consoleBindGuard() error = %v, want containing %q", err, tt.wantErrMatch)
			}
			if strings.Contains(err.Error(), tt.token) && tt.token != "" {
				t.Fatal("consoleBindGuard() leaked the configured token")
			}
		})
	}
}

func TestConsoleTokenAuth(t *testing.T) {
	const token = "console-secret"
	handler := consoleTokenAuth(token, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name       string
		auth       string
		header     string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "wrong bearer", auth: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "malformed bearer", auth: token, wantStatus: http.StatusUnauthorized},
		{name: "valid bearer", auth: "Bearer " + token, wantStatus: http.StatusNoContent},
		{name: "valid console header", header: token, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			if tt.header != "" {
				req.Header.Set("X-Aura-Console-Token", tt.header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
				}
			}
		})
	}
}

func TestMCPConsoleRefusesUnauthenticatedNetworkBind(t *testing.T) {
	t.Setenv("AURA_INTEGRATIONS_CONSOLE_TOKEN", "")
	var out strings.Builder

	err := mcpConsole([]string{"--addr", "0.0.0.0:9099"}, &out)

	if err == nil || !strings.Contains(err.Error(), "--unsafe-non-loopback") {
		t.Fatalf("mcpConsole() error = %v, want explicit unsafe-bind refusal", err)
	}
	if out.Len() != 0 {
		t.Fatalf("mcpConsole() output = %q, want no ready banner before bind validation", out.String())
	}
}

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
