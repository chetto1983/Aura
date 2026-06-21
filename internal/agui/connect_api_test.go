package agui

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// connect_api_test.go covers the cockpit "Connect" WhatsApp proxy (connect_api.go) against a
// scripted httptest fake bridge: status passthrough (body + code), the server-side QR PNG
// render (the \x89PNG magic bytes), the POST logout forward, the unset-bridge 503, and the
// bridge 409 (already paired) propagation. The capability gate is covered by the cross-
// surface auth tests below (connectGatedRoutes), parity with governance_write_auth_sweep_test.

// fakeBridge is a scripted aura-whatsapp bridge: each path returns a canned status + body.
type fakeBridge struct {
	statusCode int
	statusBody string
	qrCode     int
	qrBody     string
	logoutCode int
	logoutBody string
	gotLogout  bool
}

func (b *fakeBridge) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(b.statusCode)
		_, _ = io.WriteString(w, b.statusBody)
	})
	mux.HandleFunc("GET /api/qr", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(b.qrCode)
		_, _ = io.WriteString(w, b.qrBody)
	})
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, _ *http.Request) {
		b.gotLogout = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(b.logoutCode)
		_, _ = io.WriteString(w, b.logoutBody)
	})
	return mux
}

// connectServer builds an agui Server whose Mux is fronted by an httptest server, with the
// WhatsApp bridge optionally wired to a fake-bridge base URL.
func connectServer(bridgeURL string) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if bridgeURL != "" {
		s.SetWhatsAppBridge(bridgeURL)
	}
	return httptest.NewServer(s.Mux())
}

func TestWhatsAppStatusPassthrough(t *testing.T) {
	b := &fakeBridge{statusCode: http.StatusOK, statusBody: `{"state":"connected","paired":true,"jid":"123@s.whatsapp.net","connected":true}`}
	bridge := httptest.NewServer(b.handler())
	defer bridge.Close()
	srv := connectServer(bridge.URL)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/whatsapp/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"state":"connected"`)) || !bytes.Contains(body, []byte(`"jid":"123@s.whatsapp.net"`)) {
		t.Fatalf("status body not passed through: %q", body)
	}
}

func TestWhatsAppQRReturnsPNG(t *testing.T) {
	b := &fakeBridge{qrCode: http.StatusOK, qrBody: `{"code":"2@abc/def+linked-devices-payload"}`}
	bridge := httptest.NewServer(b.handler())
	defer bridge.Close()
	srv := connectServer(bridge.URL)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/whatsapp/qr.png")
	if err != nil {
		t.Fatalf("GET qr.png: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("qr.png status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if no := resp.Header.Get("X-Content-Type-Options"); no != "nosniff" {
		t.Fatalf("nosniff header = %q, want nosniff", no)
	}
	png, _ := io.ReadAll(resp.Body)
	// A valid PNG starts with the 8-byte magic signature \x89PNG\r\n\x1a\n.
	if len(png) < 8 || !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("body is not a PNG (magic bytes missing): %x", png[:min(8, len(png))])
	}
}

func TestWhatsAppQRPaired409(t *testing.T) {
	b := &fakeBridge{qrCode: http.StatusConflict, qrBody: `{"error":"already paired"}`}
	bridge := httptest.NewServer(b.handler())
	defer bridge.Close()
	srv := connectServer(bridge.URL)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/whatsapp/qr.png")
	if err != nil {
		t.Fatalf("GET qr.png: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("qr.png paired status = %d, want 409", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"state":"paired"`)) {
		t.Fatalf("409 body should carry the paired state, got %q", body)
	}
}

func TestWhatsAppQRNotReady503(t *testing.T) {
	b := &fakeBridge{qrCode: http.StatusServiceUnavailable, qrBody: `{"error":"qr not ready"}`}
	bridge := httptest.NewServer(b.handler())
	defer bridge.Close()
	srv := connectServer(bridge.URL)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/whatsapp/qr.png")
	if err != nil {
		t.Fatalf("GET qr.png: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("qr.png not-ready status = %d, want 503", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"state":"qr_not_ready"`)) {
		t.Fatalf("503 body should carry the qr_not_ready state, got %q", body)
	}
}

func TestWhatsAppLogoutForwardsPOST(t *testing.T) {
	b := &fakeBridge{logoutCode: http.StatusOK, logoutBody: `{"success":true}`}
	bridge := httptest.NewServer(b.handler())
	defer bridge.Close()
	srv := connectServer(bridge.URL)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/connect/whatsapp/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", resp.StatusCode)
	}
	if !b.gotLogout {
		t.Fatal("bridge POST /api/logout was not forwarded")
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"success":true`)) {
		t.Fatalf("logout body not passed through: %q", body)
	}
}

// TestWhatsAppUnwired503: no bridge wired → every connect route answers 503 (graceful — a
// stack without the sidecar boots fine).
func TestWhatsAppUnwired503(t *testing.T) {
	srv := connectServer("")
	defer srv.Close()

	for _, tc := range []struct {
		name, method, path string
	}{
		{"status", http.MethodGet, "/api/connect/whatsapp/status"},
		{"qr", http.MethodGet, "/api/connect/whatsapp/qr.png"},
		{"logout", http.MethodPost, "/api/connect/whatsapp/logout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("unwired %s = %d, want 503", tc.path, resp.StatusCode)
			}
		})
	}
}

// TestWhatsAppBridgeUnreachable502: a wired-but-dead bridge → a sanitized 502 (the bridge
// host never leaks).
func TestWhatsAppBridgeUnreachable502(t *testing.T) {
	// A closed httptest server → connection refused.
	dead := httptest.NewServer(http.NewServeMux())
	deadURL := dead.URL
	dead.Close()

	srv := connectServer(deadURL)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/whatsapp/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("dead-bridge status = %d, want 502", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte("127.0.0.1")) {
		t.Fatalf("502 body leaked the bridge host: %q", body)
	}
}
