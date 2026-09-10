package openrouterprovision_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// mintResponsePayload is the POST /api/v1/keys 201 shape transcribed from
// 02-OPENROUTER-API.md, retrieved 2026-09-08.
const mintResponsePayload = `{"key":"sk-or-v1-raw-once","data":{"hash":"hash-abc","label":"sk-or-v1-caa...61c","name":"identity-42","disabled":false,"created_at":"2026-09-08T00:00:00Z","updated_at":"2026-09-08T00:00:00Z","expires_at":null,"limit":0,"limit_remaining":0,"limit_reset":"monthly","usage":0,"usage_daily":0,"usage_weekly":0,"usage_monthly":0,"byok_usage":0,"byok_usage_daily":0,"byok_usage_weekly":0,"byok_usage_monthly":0,"external_user":"identity-42","include_byok_in_limit":false,"creator_user_id":null,"workspace_id":"ws-1"}}`

// mintServer models POST /api/v1/keys, asserting the request path AND
// method so a mistyped path shows up as a test failure rather than a 404
// the client swallows. capturedBody receives the raw request bytes so a
// test can assert on the wire form directly — a decoded struct cannot tell
// an omitted field from a zero one (TestMintAtZeroCap's whole point).
func mintServer(t *testing.T, status int, response string, capturedBody *[]byte, capturedAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/keys" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if capturedBody != nil {
			*capturedBody = body
		}
		if capturedAuth != nil {
			*capturedAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMintAtZeroCap(t *testing.T) {
	var body []byte
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, &body, nil)

	req := openrouterprovision.MintRequest{
		IdentityID: "identity-42",
		Name:       "identity-42",
		Limit:      new(openrouterprovision.USDCap),
		LimitReset: openrouterprovision.LimitResetMonthly,
	}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if !bytes.Contains(body, []byte(`"limit":0`)) {
		t.Errorf("captured mint body = %s, want it to contain a literal \"limit\":0 — an omitted limit mints an UNCAPPED key", body)
	}
}

func TestMintSendsExternalUser(t *testing.T) {
	var body []byte
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, &body, nil)

	req := openrouterprovision.MintRequest{IdentityID: "identity-77", Name: "identity-77", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitResetDaily}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if !bytes.Contains(body, []byte(`"external":{"user":"identity-77"}`)) {
		t.Errorf("captured mint body = %s, want external.user == identity-77", body)
	}
}

func TestMintSendsLimitReset(t *testing.T) {
	var body []byte
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, &body, nil)

	for _, interval := range []openrouterprovision.LimitReset{openrouterprovision.LimitResetDaily, openrouterprovision.LimitResetWeekly, openrouterprovision.LimitResetMonthly} {
		req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: interval}
		if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
			t.Fatalf("MintKey(%s): %v", interval, err)
		}
		if !bytes.Contains(body, []byte(`"limit_reset":"`+string(interval)+`"`)) {
			t.Errorf("captured mint body = %s, want limit_reset == %s", body, interval)
		}
	}

	t.Run("invalid_interval_refused_locally", func(t *testing.T) {
		var requests int
		countServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(mintResponsePayload))
		}))
		defer countServer.Close()

		req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitReset("yearly")}
		_, err := openrouterprovision.MintKey(context.Background(), countServer.Client(), countServer.URL, "sk-mgmt", req)
		if !errors.Is(err, openrouterprovision.ErrInvalidLimitReset) {
			t.Fatalf("err = %v, want ErrInvalidLimitReset", err)
		}
		if requests != 0 {
			t.Errorf("requests = %d, want 0 — an invalid interval must be refused before any request is made", requests)
		}
	})
}

func TestMintReturnsRawKeyOnce(t *testing.T) {
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, nil, nil)

	req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitResetMonthly}
	result, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req)
	if err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if result.Key != "sk-or-v1-raw-once" {
		t.Errorf("Key = %q, want the raw key from the 201 response", result.Key)
	}
	if result.Record.Hash != "hash-abc" || result.Record.Label != "sk-or-v1-caa...61c" {
		t.Errorf("Record = %+v, want hash/label from the response alongside the raw key", result.Record)
	}
}

func TestMintSendsManagementAuthorization(t *testing.T) {
	var auth string
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, nil, &auth)

	req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitResetMonthly}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-management-key", req); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if auth != "Bearer sk-management-key" {
		t.Errorf("Authorization = %q, want Bearer sk-management-key", auth)
	}
}

func TestMintCapPrecision(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{500, "5.00"},
		{525, "5.25"},
	}
	for _, tc := range cases {
		cap := openrouterprovision.USDCap(tc.cents)
		encoded, err := json.Marshal(cap)
		if err != nil {
			t.Fatalf("marshal %d cents: %v", tc.cents, err)
		}
		if string(encoded) != tc.want {
			t.Errorf("cents=%d marshalled = %s, want %s", tc.cents, encoded, tc.want)
		}
		var decoded openrouterprovision.USDCap
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", encoded, err)
		}
		if decoded != cap {
			t.Errorf("round trip: got %d cents, want %d", decoded, cap)
		}
	}
}

// getKeyServer models GET /api/v1/keys/{hash}.
func getKeyServer(t *testing.T, status int, response string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/keys/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetKeyDecodesZeroAndNullDistinctly(t *testing.T) {
	zeroPayload := `{"data":{"hash":"h1","label":"l1","name":"n1","limit":1,"limit_remaining":0,"limit_reset":"daily","external_user":"id-1"}}`
	nullPayload := `{"data":{"hash":"h2","label":"l2","name":"n2","limit":1,"limit_remaining":null,"limit_reset":"daily","external_user":"id-2"}}`

	zeroSrv := getKeyServer(t, http.StatusOK, zeroPayload)
	got, err := openrouterprovision.GetKey(context.Background(), zeroSrv.Client(), zeroSrv.URL, "sk-mgmt", "h1")
	if err != nil {
		t.Fatalf("GetKey (zero): %v", err)
	}
	if got.LimitRemaining == nil || *got.LimitRemaining != 0 {
		t.Errorf("LimitRemaining = %v, want a non-nil pointer to 0 (zero is data)", got.LimitRemaining)
	}

	nullSrv := getKeyServer(t, http.StatusOK, nullPayload)
	got2, err := openrouterprovision.GetKey(context.Background(), nullSrv.Client(), nullSrv.URL, "sk-mgmt", "h2")
	if err != nil {
		t.Fatalf("GetKey (null): %v", err)
	}
	if got2.LimitRemaining != nil {
		t.Errorf("LimitRemaining = %v, want nil (absence)", got2.LimitRemaining)
	}
}

func TestGetKeyNotFound(t *testing.T) {
	srv := getKeyServer(t, http.StatusNotFound, `{"error":{"message":"key not found"}}`)
	_, err := openrouterprovision.GetKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "missing-hash")
	if !errors.Is(err, openrouterprovision.ErrKeyNotFound) {
		t.Fatalf("err = %v, want ErrKeyNotFound", err)
	}
}

// revokeStep is one scripted response for the revoke fixture below.
type revokeStep struct {
	status int
	body   string
}

// revokeServer models the DELETE+GET pair CRED-08 requires, recording the
// ordered method+path of every request it sees so a test can assert BOTH
// that the pair happened and that it happened in that order.
func revokeServer(t *testing.T, deleteResp, getResp revokeStep) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		var step revokeStep
		switch r.Method {
		case http.MethodDelete:
			step = deleteResp
		case http.MethodGet:
			step = getResp
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		_, _ = w.Write([]byte(step.body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestRevokeVerifiesDeletion(t *testing.T) {
	srv, seen := revokeServer(t,
		revokeStep{http.StatusOK, `{"deleted":true}`},
		revokeStep{http.StatusNotFound, `{"error":{"message":"key not found"}}`},
	)
	if err := openrouterprovision.RevokeKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1"); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	want := []string{"DELETE /keys/h1", "GET /keys/h1"}
	if len(*seen) != len(want) {
		t.Fatalf("requests = %v, want %v", *seen, want)
	}
	for i := range want {
		if (*seen)[i] != want[i] {
			t.Errorf("request[%d] = %q, want %q", i, (*seen)[i], want[i])
		}
	}
}

func TestRevokeFailsWhenKeyStillReadable(t *testing.T) {
	srv, _ := revokeServer(t,
		revokeStep{http.StatusOK, `{"deleted":true}`},
		revokeStep{http.StatusOK, `{"data":{"hash":"h1"}}`},
	)
	err := openrouterprovision.RevokeKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1")
	if err == nil {
		t.Fatal("RevokeKey: want an error when the follow-up GET still returns 200 — a revoke that cannot verify itself is a failure, not a success")
	}
}

func TestRevokeIsIdempotent(t *testing.T) {
	srv, seen := revokeServer(t,
		revokeStep{http.StatusNotFound, `{"error":{"message":"key not found"}}`},
		revokeStep{http.StatusNotFound, `{"error":{"message":"key not found"}}`},
	)
	if err := openrouterprovision.RevokeKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1"); err != nil {
		t.Fatalf("RevokeKey (re-run after interruption): %v", err)
	}
	if len(*seen) != 2 {
		t.Errorf("requests = %v, want a DELETE and a GET even when DELETE itself 404s", *seen)
	}
}

func TestRevokeKeyDeleteProviderError(t *testing.T) {
	srv, seen := revokeServer(t,
		revokeStep{http.StatusInternalServerError, `{"error":{"message":"boom"}}`},
		revokeStep{http.StatusNotFound, `{"error":{"message":"key not found"}}`},
	)
	err := openrouterprovision.RevokeKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1")
	if err == nil {
		t.Fatal("RevokeKey: want an error when DELETE itself fails with a provider error")
	}
	if len(*seen) != 1 {
		t.Errorf("requests = %v, want only the DELETE — a failed delete must not be followed by a verifying GET", *seen)
	}
}

func TestRevokeKeyDeleteConfirmationFalse(t *testing.T) {
	srv, _ := revokeServer(t,
		revokeStep{http.StatusOK, `{"deleted":false}`},
		revokeStep{http.StatusNotFound, `{"error":{"message":"key not found"}}`},
	)
	err := openrouterprovision.RevokeKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1")
	if err == nil {
		t.Fatal("RevokeKey: want an error when the provider's own delete response says deleted:false")
	}
}

func TestRevokeKeyEmptyHash(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := openrouterprovision.RevokeKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "  "); err == nil {
		t.Fatal("RevokeKey: want an error for an empty hash")
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 — an empty hash must be refused before any request is made", requests)
	}
}

func TestGetKeyEmptyHash(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := openrouterprovision.GetKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", ""); err == nil {
		t.Fatal("GetKey: want an error for an empty hash")
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 — an empty hash must be refused before any request is made", requests)
	}
}

func TestMintKeyEmptyIdentity(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(mintResponsePayload))
	}))
	defer srv.Close()

	req := openrouterprovision.MintRequest{IdentityID: "  ", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitResetDaily}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err == nil {
		t.Fatal("MintKey: want an error for an empty identity id")
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 — an empty identity id must be refused before any request is made", requests)
	}
}

func TestMintKeyProviderError(t *testing.T) {
	srv := mintServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`, nil, nil)
	req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitResetDaily}
	_, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req)
	if err == nil {
		t.Fatal("MintKey: want an error when the provider returns a non-201 status")
	}
	if errors.Is(err, openrouterprovision.ErrKeyLimitExceeded) || errors.Is(err, openrouterprovision.ErrKeyRevoked) || errors.Is(err, openrouterprovision.ErrKeyNotFound) {
		t.Errorf("err = %v, a generic 500 must not classify as any named sentinel", err)
	}
}

func TestMintKeyDecodeError(t *testing.T) {
	srv := mintServer(t, http.StatusCreated, "not json", nil, nil)
	req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", Limit: new(openrouterprovision.USDCap), LimitReset: openrouterprovision.LimitResetDaily}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err == nil {
		t.Fatal("MintKey: want an error when the 201 body cannot be decoded")
	}
}

func TestGetKeyProviderError(t *testing.T) {
	srv := getKeyServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	if _, err := openrouterprovision.GetKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1"); err == nil {
		t.Fatal("GetKey: want an error when the provider returns a non-200 status")
	}
}

func TestGetKeyDecodeError(t *testing.T) {
	srv := getKeyServer(t, http.StatusOK, "not json")
	if _, err := openrouterprovision.GetKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1"); err == nil {
		t.Fatal("GetKey: want an error when the 200 body cannot be decoded")
	}
}

func TestNewUSDCapFromString(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    openrouterprovision.USDCap
		wantErr bool
	}{
		{name: "whole", in: "5", want: 500},
		{name: "two_decimals", in: "5.25", want: 525},
		{name: "half_up_rounds_up", in: "5.255", want: 526},
		{name: "zero", in: "0", want: 0},
		{name: "empty", in: "  ", wantErr: true},
		{name: "negative", in: "-1", wantErr: true},
		{name: "not_a_number", in: "abc", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := openrouterprovision.NewUSDCapFromString(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewUSDCapFromString(%q): want an error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewUSDCapFromString(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("NewUSDCapFromString(%q) = %d cents, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestUSDCapUnmarshalJSONNull(t *testing.T) {
	var decoded openrouterprovision.USDCap = 999
	if err := json.Unmarshal([]byte("null"), &decoded); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if decoded != 0 {
		t.Errorf("decoded = %d, want 0 for a JSON null", decoded)
	}
}

func TestUSDCapUnmarshalJSONInvalid(t *testing.T) {
	var decoded openrouterprovision.USDCap
	if err := json.Unmarshal([]byte(`"not-a-number"`), &decoded); err == nil {
		t.Fatal("unmarshal: want an error for a non-numeric token")
	}
}

func TestMintWithNoLimitSendsNull(t *testing.T) {
	var body []byte
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, &body, nil)

	req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", LimitReset: openrouterprovision.LimitResetMonthly}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if !bytes.Contains(body, []byte(`"limit":null`)) {
		t.Errorf("captured mint body = %s, want \"limit\":null for a key with no limit", body)
	}
}
