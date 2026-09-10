package openrouterprovision_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// patchResponsePayload is the PATCH /api/v1/keys/{hash} 200 shape.
const patchResponsePayload = `{"data":{"hash":"h1","label":"l1","name":"n1","limit":0,"limit_remaining":0,"limit_reset":"daily","external_user":"id-1"}}`

// patchServer models PATCH /api/v1/keys/{hash}.
func patchServer(t *testing.T, status int, response string, capturedBody *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || !strings.HasPrefix(r.URL.Path, "/keys/") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if capturedBody != nil {
			*capturedBody = body
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPatchSendsOnlyChangedFields(t *testing.T) {
	var body []byte
	srv := patchServer(t, http.StatusOK, patchResponsePayload, &body)

	limit := openrouterprovision.USDCap(500)
	patch := openrouterprovision.KeyPatch{Limit: &limit}
	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch); err != nil {
		t.Fatalf("PatchKey: %v", err)
	}
	if !bytes.Contains(body, []byte(`"limit":5.00`)) {
		t.Errorf("captured patch body = %s, want it to contain the changed limit", body)
	}
	for _, absent := range []string{`"limit_reset"`, `"name"`, `"disabled"`} {
		if bytes.Contains(body, []byte(absent)) {
			t.Errorf("captured patch body = %s, must NOT contain %s — only changed fields are sent", body, absent)
		}
	}
}

func TestPatchZeroLimit(t *testing.T) {
	var body []byte
	srv := patchServer(t, http.StatusOK, patchResponsePayload, &body)

	zero := openrouterprovision.USDCap(0)
	patch := openrouterprovision.KeyPatch{Limit: &zero}
	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch); err != nil {
		t.Fatalf("PatchKey: %v", err)
	}
	if !bytes.Contains(body, []byte(`"limit":0`)) {
		t.Errorf("captured patch body = %s, want a literal \"limit\":0 — the same omitempty trap as the mint path", body)
	}
}

func TestPatchInvalidResetInterval(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(patchResponsePayload))
	}))
	defer srv.Close()

	bad := openrouterprovision.LimitReset("yearly")
	patch := openrouterprovision.KeyPatch{LimitReset: &bad}
	_, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch)
	if !errors.Is(err, openrouterprovision.ErrInvalidLimitReset) {
		t.Fatalf("err = %v, want ErrInvalidLimitReset", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 — an invalid interval must be refused before the request is made", requests)
	}
}

func TestPatchKeyEmptyHash(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "", openrouterprovision.KeyPatch{}); err == nil {
		t.Fatal("PatchKey: want an error for an empty hash")
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 — an empty hash must be refused before any request is made", requests)
	}
}

func TestPatchKeyProviderError(t *testing.T) {
	srv := patchServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`, nil)
	limit := openrouterprovision.USDCap(100)
	patch := openrouterprovision.KeyPatch{Limit: &limit}
	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch); err == nil {
		t.Fatal("PatchKey: want an error when the provider returns a non-200 status")
	}
}

func TestPatchKeyDecodeError(t *testing.T) {
	srv := patchServer(t, http.StatusOK, "not json", nil)
	limit := openrouterprovision.USDCap(100)
	patch := openrouterprovision.KeyPatch{Limit: &limit}
	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch); err == nil {
		t.Fatal("PatchKey: want an error when the 200 body cannot be decoded")
	}
}

func TestPatchClearLimitSendsNull(t *testing.T) {
	var body []byte
	srv := patchServer(t, http.StatusOK, patchResponsePayload, &body)

	patch := openrouterprovision.KeyPatch{ClearLimit: true}
	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch); err != nil {
		t.Fatalf("PatchKey: %v", err)
	}
	if string(body) != `{"limit":null}` {
		t.Errorf("captured patch body = %s, want {\"limit\":null}", body)
	}
}

func TestPatchRefusesSettingAndClearingTheLimit(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(patchResponsePayload))
	}))
	defer srv.Close()

	five := openrouterprovision.USDCap(500)
	patch := openrouterprovision.KeyPatch{Limit: &five, ClearLimit: true}
	_, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch)
	if !errors.Is(err, openrouterprovision.ErrConflictingLimitPatch) {
		t.Fatalf("err = %v, want ErrConflictingLimitPatch", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0: a contradictory patch is refused before any request", requests)
	}
}
