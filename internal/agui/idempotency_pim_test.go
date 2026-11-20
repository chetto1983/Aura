package agui

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
)

// idempotency_pim_test.go pins amendment A9: the idempotency ledger of the PIM account create and
// provider-config PATCH keeps no password, nor any unkeyed hash of one, while the handler still
// forwards the real body.

var pimSecretRoutes = []struct{ route, method, target string }{
	{"POST /api/connect/pim/accounts", http.MethodPost, "/api/connect/pim/accounts"},
	{"PATCH /api/connect/pim/accounts/{id}/provider-config", http.MethodPatch, "/api/connect/pim/accounts/work/provider-config"},
}

func pimSecretBody(passwordKey, password string) string {
	return `{"id":"work","provider":"imap","providerConfig":{"username":"someone@example.com","` + passwordKey + `":"` + password + `"}}`
}

func TestTheFingerprintHidesThePassword(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	for _, rt := range pimSecretRoutes {
		meta, ok := httpMutationRoutes[rt.route]
		if !ok || !meta.HidesPIMSecrets {
			t.Fatalf("%s: inventory entry %+v does not hide its secrets", rt.route, meta)
		}
		fingerprint := func(body string) ([32]byte, httpMutationIntent) {
			t.Helper()
			req := httptest.NewRequest(rt.method, rt.target, strings.NewReader(body))
			intent, err := normalizePIMSecretMutation(key)(req, meta)
			if err != nil {
				t.Fatalf("%s: normalize: %v", rt.route, err)
			}
			if forwarded, _ := io.ReadAll(req.Body); string(forwarded) != body {
				t.Fatalf("%s: the handler would read %q, want the real body", rt.route, forwarded)
			}
			fp, err := idempotency.FingerprintTyped(intent)
			if err != nil {
				t.Fatalf("%s: fingerprint: %v", rt.route, err)
			}
			return fp, intent
		}
		bodyA, bodyB := pimSecretBody("password", "first-s3cret"), pimSecretBody("password", "second-s3cret")
		a, intentA := fingerprint(bodyA)
		b, _ := fingerprint(bodyB)
		again, _ := fingerprint(bodyA)
		if a == b {
			t.Fatalf("%s: two passwords gave one fingerprint; a replay with another password would be served", rt.route)
		}
		if a != again {
			t.Fatalf("%s: the same request gave two fingerprints", rt.route)
		}
		if a == sha256.Sum256([]byte(bodyA)) || b == sha256.Sum256([]byte(bodyB)) {
			t.Fatalf("%s: a fingerprint is the plain SHA-256 of its body", rt.route)
		}
		plainReq := httptest.NewRequest(rt.method, rt.target, strings.NewReader(bodyA))
		plainIntent, err := normalizeHTTPMutation(plainReq, meta)
		if err != nil {
			t.Fatal(err)
		}
		if plain, _ := idempotency.FingerprintTyped(plainIntent); plain == a {
			t.Fatalf("%s: the fingerprint is the unkeyed one", rt.route)
		}
		if strings.Contains(string(intentA.Body), "first-s3cret") {
			t.Fatalf("%s: the intent keeps the password: %s", rt.route, intentA.Body)
		}
		if _, folded := fingerprint(pimSecretBody("PassWord", "third-s3cret")); strings.Contains(string(folded.Body), "third-s3cret") {
			t.Fatalf("%s: a PassWord key kept its value: %s", rt.route, folded.Body)
		}
	}
}

func TestAPIMSecretRouteWithoutAKeyIs503(t *testing.T) {
	for _, rt := range pimSecretRoutes {
		server := &Server{operations: &memoryHTTPRegistry{}}
		calls := 0
		handler := server.idempotencyMutation(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			calls++
		}), httpMutationRoutes[rt.route])
		req := withPrincipal(httptest.NewRequest(rt.method, rt.target, strings.NewReader(pimSecretBody("password", "s3cret"))), fakePIMIdentity)
		req.Header.Set("Idempotency-Key", "attempt-1")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable || calls != 0 {
			t.Fatalf("%s without a key = %d with %d handler calls, want 503 before the handler", rt.route, rec.Code, calls)
		}
	}
}

func TestTheLedgerGetsTheKeyedFingerprintAndTheHandlerThePassword(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	registry := &memoryHTTPRegistry{}
	server := &Server{operations: registry}
	server.SetIdempotencySecretKey(key)
	var forwarded string
	meta := httpMutationRoutes["POST /api/connect/pim/accounts"]
	handler := server.idempotencyMutation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		forwarded = string(body)
		writeJSONStatus(w, http.StatusCreated, map[string]string{"id": "work"})
	}), meta)
	body := pimSecretBody("password", "s3cret")
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/connect/pim/accounts", strings.NewReader(body)), fakePIMIdentity)
	req.Header.Set("Idempotency-Key", "attempt-1")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if forwarded != body {
		t.Fatalf("the handler read %q, want the real body", forwarded)
	}
	intent, err := normalizePIMSecretMutation(key)(httptest.NewRequest(http.MethodPost, "/api/connect/pim/accounts", strings.NewReader(body)), meta)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := idempotency.FingerprintTyped(intent)
	if registry.request == nil || registry.request.Fingerprint != want {
		t.Fatal("the ledger did not receive the keyed fingerprint")
	}
}

func TestDeriveIdempotencySecretKey(t *testing.T) {
	secretHex := strings.Repeat("01", 32)
	key, err := DeriveIdempotencySecretKey(secretHex)
	if err != nil || len(key) != 32 {
		t.Fatalf("DeriveIdempotencySecretKey(valid) = %d bytes, %v; want 32 bytes", len(key), err)
	}
	pepper, err := DeriveResetTokenPepper(secretHex)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key, pepper) {
		t.Fatal("the idempotency key equals the reset pepper; each use needs its own key")
	}
	if _, err := DeriveIdempotencySecretKey("short"); err == nil {
		t.Fatal("an invalid secret derived a key")
	}
}
