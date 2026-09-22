package cloudflareapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyNeverUsesEveryone(t *testing.T) {
	p, err := EmailPolicy("Aura users", []string{"Admin@Example.com", "admin@example.com"}, "otp-id")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil || strings.Contains(string(b), "everyone") || !strings.Contains(string(b), "admin@example.com") || len(p.Include) != 1 {
		t.Fatalf("policy=%s err=%v", b, err)
	}
	for _, emails := range [][]string{nil, {"*"}, {"@example.com"}, {"Name <admin@example.com>"}} {
		if _, err := EmailPolicy("Aura users", emails, "otp-id"); err == nil {
			t.Fatal("unsafe email accepted")
		}
	}
	if _, err := EmailPolicy("Aura users", []string{"admin@example.com"}, ""); err == nil {
		t.Fatal("missing OTP accepted")
	}
}

func TestResourceWireContracts(t *testing.T) {
	ctx := context.Background()
	policy, _ := GatewayEmailPolicy("Aura users", []string{"admin@example.com"}, "otp-one", "posture-one")
	app := AccessApplication{Name: "aura-owned", Domain: "aura.example.com", Type: "self_hosted", AllowedIDPs: []string{"otp-one"}, SessionDuration: "24h"}
	config := TunnelConfig{Ingress: []Ingress{{Hostname: "aura.example.com", Service: "http://caddy:8080"}, {Service: "http_status:404"}}}
	for _, tc := range []struct {
		name, method, path, response string
		want                         map[string]any
		call                         func(*Client) error
	}{
		{"verify", "GET", "/user/tokens/verify", `{"success":true,"result":{"id":"token-one","status":"active"}}`, nil, func(c *Client) error { _, e := c.VerifyToken(ctx); return e }},
		{"zones", "GET", "/zones", `{"success":true,"result":[]}`, nil, func(c *Client) error { _, e := c.ListZones(ctx, "account-one", "example.com"); return e }},
		{"zone-get", "GET", "/zones/zone-one", string(fixture(t, "zone-pending")), nil, func(c *Client) error {
			z, e := c.GetZone(ctx, "zone-one")
			if e == nil && (z.Status != "pending" || len(z.NameServers) != 2) {
				t.Error("pending zone lost")
			}
			return e
		}},
		{"zone-create", "POST", "/zones", string(fixture(t, "zone-pending")), map[string]any{"name": "example.com", "type": "full", "account": map[string]any{"id": "account-one"}}, func(c *Client) error { _, e := c.CreateZone(ctx, "account-one", "example.com"); return e }},
		{"tunnel-create", "POST", "/accounts/account-one/cfd_tunnel", string(fixture(t, "tunnel-created")), map[string]any{"name": "aura-owned", "config_src": "cloudflare"}, func(c *Client) error {
			v, e := c.CreateTunnel(ctx, "account-one", "aura-owned")
			if strings.Contains(fmt.Sprintf("%+v", v), "never-expose") {
				t.Error("tunnel creation secret leak")
			}
			return e
		}},
		{"tunnel-get", "GET", "/accounts/account-one/cfd_tunnel/tunnel-one", string(fixture(t, "tunnel-created")), nil, func(c *Client) error { _, e := c.GetTunnel(ctx, "account-one", "tunnel-one"); return e }},
		{"tunnel-list", "GET", "/accounts/account-one/cfd_tunnel", `{"success":true,"result":[]}`, nil, func(c *Client) error { _, e := c.ListTunnels(ctx, "account-one"); return e }},
		{"tunnel-delete", "DELETE", "/accounts/account-one/cfd_tunnel/tunnel-one", `{"success":true,"result":null}`, nil, func(c *Client) error { return c.DeleteTunnel(ctx, "account-one", "tunnel-one") }},
		{"token", "GET", "/accounts/account-one/cfd_tunnel/tunnel-one/token", `{"success":true,"result":"tunnel-secret"}`, nil, func(c *Client) error {
			v, e := c.TunnelToken(ctx, "account-one", "tunnel-one")
			if e == nil && v.Reveal() != "tunnel-secret" {
				t.Error("token lost")
			}
			return e
		}},
		{"config", "PUT", "/accounts/account-one/cfd_tunnel/tunnel-one/configurations", `{"success":true,"result":{"config":{"ingress":[]}}}`, map[string]any{"config": map[string]any{"ingress": []any{map[string]any{"hostname": "aura.example.com", "service": "http://caddy:8080"}, map[string]any{"service": "http_status:404"}}}}, func(c *Client) error { return c.PutTunnelConfig(ctx, "account-one", "tunnel-one", config) }},
		{"idps", "GET", "/accounts/account-one/access/identity_providers", `{"success":true,"result":[]}`, nil, func(c *Client) error { _, e := c.ListIdentityProviders(ctx, "account-one"); return e }},
		{"apps", "GET", "/accounts/account-one/access/apps", `{"success":true,"result":[]}`, nil, func(c *Client) error { _, e := c.ListApplications(ctx, "account-one"); return e }},
		{"app-get", "GET", "/accounts/account-one/access/apps/app-one", string(fixture(t, "access-app")), nil, func(c *Client) error { _, e := c.GetApplication(ctx, "account-one", "app-one"); return e }},
		{"app-create", "POST", "/accounts/account-one/access/apps", string(fixture(t, "access-app")), map[string]any{"type": "self_hosted", "allowed_idps": []any{"otp-one"}, "auto_redirect_to_identity": false}, func(c *Client) error { _, e := c.PutApplication(ctx, "account-one", "", app); return e }},
		{"app-update", "PUT", "/accounts/account-one/access/apps/app-one", string(fixture(t, "access-app")), map[string]any{"domain": "aura.example.com"}, func(c *Client) error { _, e := c.PutApplication(ctx, "account-one", "app-one", app); return e }},
		{"app-delete", "DELETE", "/accounts/account-one/access/apps/app-one", `{"success":true}`, nil, func(c *Client) error { return c.DeleteApplication(ctx, "account-one", "app-one") }},
		{"policy-get", "GET", "/accounts/account-one/access/apps/app-one/policies/policy-one", `{"success":true,"result":{"id":"policy-one"}}`, nil, func(c *Client) error { _, e := c.GetPolicy(ctx, "account-one", "app-one", "policy-one"); return e }},
		{"policy-create", "POST", "/accounts/account-one/access/apps/app-one/policies", `{"success":true,"result":{"id":"policy-one"}}`, map[string]any{"decision": "allow", "include": []any{map[string]any{"email": map[string]any{"email": "admin@example.com"}}}, "require": []any{map[string]any{"login_method": map[string]any{"id": "otp-one"}}, map[string]any{"device_posture": map[string]any{"integration_uid": "posture-one"}}}}, func(c *Client) error { _, e := c.PutPolicy(ctx, "account-one", "app-one", "", policy); return e }},
		{"policy-update", "PUT", "/accounts/account-one/access/apps/app-one/policies/policy-one", `{"success":true,"result":{"id":"policy-one"}}`, nil, func(c *Client) error {
			_, e := c.PutPolicy(ctx, "account-one", "app-one", "policy-one", policy)
			return e
		}},
		{"policy-delete", "DELETE", "/accounts/account-one/access/apps/app-one/policies/policy-one", `{"success":true}`, nil, func(c *Client) error { return c.DeletePolicy(ctx, "account-one", "app-one", "policy-one") }},
		{"posture-list", "GET", "/accounts/account-one/devices/posture", `{"success":true,"result":[]}`, nil, func(c *Client) error { _, e := c.ListPosture(ctx, "account-one"); return e }},
		{"posture-create", "POST", "/accounts/account-one/devices/posture", string(fixture(t, "posture-warp")), map[string]any{"type": "gateway", "name": "Aura WARP-required"}, func(c *Client) error {
			_, e := c.EnsureGatewayPosture(ctx, "account-one", "", "Aura WARP-required")
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer api-secret" {
					t.Errorf("unexpected %s %s", r.Method, r.URL)
				}
				if tc.name == "zones" && (r.URL.Query().Get("account.id") != "account-one" || r.URL.Query().Get("name") != "example.com") {
					t.Error("missing account/zone filter")
				}
				if tc.want != nil {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					for k, v := range tc.want {
						expected, _ := json.Marshal(v)
						got, _ := json.Marshal(body[k])
						if string(got) != string(expected) {
							t.Errorf("%s=%s want %s", k, got, expected)
						}
					}
				}
				_, _ = io.WriteString(w, tc.response)
			}))
			defer s.Close()
			if err := tc.call(New(s.URL, "api-secret", s.Client())); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestOTPReuseAndCreation(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/accounts/account-one/access/identity_providers" {
					t.Error(r.URL)
				}
				if r.Method == "GET" {
					result := `[]`
					if exists {
						result = `[{"id":"otp-one","type":"onetimepin"}]`
					}
					_, _ = fmt.Fprintf(w, `{"success":true,"result":%s}`, result)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if r.Method != "POST" || body["type"] != "onetimepin" || body["config"] == nil {
					t.Errorf("OTP body=%v", body)
				}
				_, _ = w.Write(fixture(t, "otp-provider"))
			}))
			defer s.Close()
			p, err := New(s.URL, "fixture-token", s.Client()).EnsureOTPProvider(t.Context(), "account-one")
			want := 2
			if exists {
				want = 1
			}
			if err != nil || p.ID != "otp-one" || calls != want {
				t.Fatalf("p=%v err=%v calls=%d", p, err, calls)
			}
		})
	}
}

func TestDNSOwnershipAndPublication(t *testing.T) {
	for _, mode := range []string{"new", "owned", "foreign", "untracked", "delete-owned", "delete-foreign"} {
		t.Run(mode, func(t *testing.T) {
			mutations := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					switch mode {
					case "new":
						_, _ = io.WriteString(w, `{"success":true,"result":[]}`)
					case "untracked":
						_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"dns-one","comment":"aura-owner"}]}`)
					case "foreign", "delete-foreign":
						_, _ = w.Write(fixture(t, "dns-foreign"))
					default:
						_, _ = io.WriteString(w, `{"success":true,"result":{"id":"dns-one","type":"CNAME","name":"aura.example.com","comment":"aura-owner"}}`)
					}
					return
				}
				mutations++
				if strings.HasPrefix(mode, "delete") {
					if r.Method != "DELETE" || r.URL.Path != "/zones/zone-one/dns_records/dns-one" {
						t.Error("wrong deletion")
					}
					_, _ = io.WriteString(w, `{"success":true}`)
					return
				}
				var body DNSRecord
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body.Content != "tunnel-one.cfargotunnel.com" || body.Comment != "aura-owner" || !body.Proxied || body.TTL != 1 || body.Type != "CNAME" {
					t.Errorf("record=%+v", body)
				}
				if mode == "new" && r.Method != "POST" || mode == "owned" && r.Method != "PUT" {
					t.Error("wrong mutation")
				}
				_, _ = io.WriteString(w, `{"success":true,"result":{"id":"dns-one"}}`)
			}))
			defer s.Close()
			c := New(s.URL, "fixture-token", s.Client())
			id := "dns-one"
			if mode == "new" || mode == "untracked" {
				id = ""
			}
			var err error
			if strings.HasPrefix(mode, "delete") {
				err = c.DeleteDNS(t.Context(), "zone-one", id, "aura-owner")
			} else {
				_, err = c.EnsureCNAME(t.Context(), "zone-one", id, "aura.example.com", "tunnel-one", "aura-owner")
			}
			conflict := mode == "foreign" || mode == "untracked" || mode == "delete-foreign"
			if (err != nil) != conflict || (mutations == 0) != conflict {
				t.Fatalf("mutations=%d err=%v", mutations, err)
			}
		})
	}
}

func TestGatewayPostureOwnership(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "DELETE" {
				if foreign {
					t.Error("foreign posture deleted")
				}
				_, _ = io.WriteString(w, `{"success":true}`)
				return
			}
			body := fixture(t, "posture-warp")
			if foreign {
				body = []byte(strings.ReplaceAll(string(body), "gateway", "warp"))
			}
			_, _ = w.Write(body)
		}))
		c := New(s.URL, "fixture-token", s.Client())
		_, err := c.EnsureGatewayPosture(t.Context(), "account-one", "posture-one", "Aura WARP-required")
		if (err != nil) != foreign {
			t.Fatalf("foreign=%v err=%v", foreign, err)
		}
		err = c.DeletePosture(t.Context(), "account-one", "posture-one", "Aura WARP-required")
		if (err != nil) != foreign {
			t.Fatalf("delete foreign=%v err=%v", foreign, err)
		}
		s.Close()
	}
}
