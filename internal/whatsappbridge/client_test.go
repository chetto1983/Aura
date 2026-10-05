package whatsappbridge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const (
	testToken    = "bridge-secret"
	testIdentity = "448ddbe1-96ea-405d-8219-4a3d52a425c0"
)

// statusBridge answers GET /api/status like the tenant gateway, refusing any request
// without the bearer or the tenant header so a missing header fails the test loudly.
func statusBridge(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("X-Tenant-ID") != testIdentity {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(func() {
		srv.Close()
		httpClient.CloseIdleConnections()
	})
	return srv
}

// TestLinkedNumberStripsTheDeviceSuffix uses the shape measured on the lab VM bridge on
// 2026-10-05: the paired JID is this linked device ("…:32@s.whatsapp.net"), and the
// account send_message reaches is the number before the device suffix.
func TestLinkedNumberStripsTheDeviceSuffix(t *testing.T) {
	srv := statusBridge(t, http.StatusOK, `{"connected":true,"jid":"393331112222:32@s.whatsapp.net","paired":true,"qr_available":false,"state":"connected"}`)
	got, err := New(srv.URL+"/", " "+testToken+" ").LinkedNumber(context.Background(), testIdentity)
	if err != nil || got != "393331112222" {
		t.Fatalf("LinkedNumber = %q, %v; want 393331112222", got, err)
	}
}

func TestLinkedNumberFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		body    string
		wantErr string
		is      error
	}{
		{name: "not paired", code: http.StatusOK, body: `{"paired":false,"jid":"","state":"waiting_qr"}`, is: ErrNotLinked},
		{name: "not a phone JID", code: http.StatusOK, body: `{"paired":true,"jid":"123456789:3@lid"}`, wantErr: "not a phone-number JID"},
		{name: "bridge error", code: http.StatusInternalServerError, body: `oops`, wantErr: "HTTP 500"},
		{name: "invalid payload", code: http.StatusOK, body: `not json`, wantErr: "whatsapp bridge status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := statusBridge(t, tc.code, tc.body)
			got, err := New(srv.URL, testToken).LinkedNumber(context.Background(), testIdentity)
			if got != "" || err == nil {
				t.Fatalf("LinkedNumber = %q, %v; want an error", got, err)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("error = %v, want %v", err, tc.is)
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestUnconfiguredClientNeverDials(t *testing.T) {
	dialled := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dialled = true }))
	defer srv.Close()
	for _, c := range []Client{{}, New(srv.URL, ""), New("", testToken)} {
		if _, err := c.LinkedNumber(context.Background(), testIdentity); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("LinkedNumber on %+v = %v, want ErrNotConfigured", c, err)
		}
	}
	if dialled {
		t.Fatal("an unconfigured client reached the bridge")
	}
}

func TestLinkedNumberUnreachableBridge(t *testing.T) {
	dead := httptest.NewServer(http.NewServeMux())
	deadURL := dead.URL
	dead.Close()
	if _, err := New(deadURL, testToken).LinkedNumber(context.Background(), testIdentity); err == nil {
		t.Fatal("an unreachable bridge returned no error")
	}
}

func TestPhoneNumber(t *testing.T) {
	for jid, want := range map[string]string{
		"393331112222@s.whatsapp.net":    "393331112222",
		"393331112222:32@s.whatsapp.net": "393331112222",
		"":                               "",
		"393331112222":                   "",
		":32@s.whatsapp.net":             "",
		"120363000000000000@g.us":        "",
	} {
		got, err := phoneNumber(jid)
		if got != want || (want == "") != (err != nil) {
			t.Errorf("phoneNumber(%q) = %q, %v; want %q", jid, got, err, want)
		}
	}
}
