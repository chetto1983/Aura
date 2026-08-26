package webauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistrationHandlerUsesRFC7591NumericTimestamps(t *testing.T) {
	issuedAt := time.Unix(1_788_303_600, 0).UTC()
	server := &OAuthServer{now: func() time.Time { return issuedAt }}
	body := `{"redirect_uris":["http://127.0.0.1:9080/api/governance/mcp/authorization/callback"],"token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.RegistrationHandler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if got := response["client_id_issued_at"]; got != float64(issuedAt.Unix()) {
		t.Errorf("client_id_issued_at = %#v, want Unix timestamp %d", got, issuedAt.Unix())
	}
	if _, exists := response["client_secret_expires_at"]; exists {
		t.Error("public client response must omit client_secret_expires_at")
	}
}

func TestOAuthRedirectsRejectNonHTTPAndUserinfoCallbacks(t *testing.T) {
	server := &OAuthServer{}
	for _, raw := range []string{
		"javascript://localhost/api/governance/mcp/authorization/callback",
		"http://operator@127.0.0.1:9080/api/governance/mcp/authorization/callback",
		" http://127.0.0.1:9080/api/governance/mcp/authorization/callback",
	} {
		if server.redirectAllowed(raw) {
			t.Errorf("redirectAllowed(%q) = true, want false", raw)
		}
	}
}
