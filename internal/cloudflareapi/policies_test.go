package cloudflareapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPoliciesReadsEveryPage(t *testing.T) {
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || r.URL.Path != "/accounts/account/access/apps/app/policies" || r.URL.Query().Get("page") != fmt.Sprint(requests) {
			t.Error("wrong policy discovery request")
		}
		fmt.Fprintf(w, `{"success":true,"result":[{"id":"policy-%d","name":"policy"}],"result_info":{"page":%d,"total_pages":2}}`, requests, requests)
	}))
	defer s.Close()
	policies, err := New(s.URL, "fixture-token", s.Client()).ListPolicies(t.Context(), "account", "app")
	if err != nil || len(policies) != 2 || requests != 2 || policies[1].ID != "policy-2" {
		t.Fatalf("policies=%+v requests=%d err=%v", policies, requests, err)
	}
}

func TestTunnelCurrentConfigurationAndDeletionFields(t *testing.T) {
	var tunnel Tunnel
	if err := json.Unmarshal([]byte(`{"id":"owned","name":"aura-owned","config_src":"cloudflare","deleted_at":"2026-09-20T12:00:00Z"}`), &tunnel); err != nil {
		t.Fatal(err)
	}
	if tunnel.ConfigSource != "cloudflare" || tunnel.DeletedAt == nil || tunnel.DeletedAt.Year() != 2026 {
		t.Fatalf("tunnel=%+v", tunnel)
	}
}
