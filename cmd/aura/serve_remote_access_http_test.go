package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

// remoteHTTPAdmin makes every caller an administrator: these tests are about what the remote-access
// routes answer, not about who may ask.
type remoteHTTPAdmin struct{}

func (remoteHTTPAdmin) ListIdentities(context.Context) ([]identity.Identity, error) { return nil, nil }
func (remoteHTTPAdmin) GetIdentityByID(context.Context, string) (identity.Identity, error) {
	return identity.Identity{}, nil
}
func (remoteHTTPAdmin) ListCapabilities(context.Context, string) ([]string, error) { return nil, nil }
func (remoteHTTPAdmin) GrantCapability(context.Context, string, string) error      { return nil }
func (remoteHTTPAdmin) RevokeCapability(context.Context, string, string) error     { return nil }
func (remoteHTTPAdmin) HasCapability(context.Context, string, string) (bool, error) {
	return true, nil
}

type remoteHTTP struct{ *httptest.Server }

// serveRemoteAccess mounts the real AG-UI routes over the controller, behind RequireAuth with a
// session that is always the administrator.
func serveRemoteAccess(t *testing.T, c *remoteAccessController) *remoteHTTP {
	t.Helper()
	server := agui.NewServer(nil, nil, agui.ServerConfig{})
	server.SetIdentityAdmin(remoteHTTPAdmin{})
	server.SetRemoteAccess(c)
	srv := httptest.NewServer(agui.RequireAuth(server.Mux(), agui.AuthDeps{
		SecretConfigured: true,
		LocalIdentityID:  "admin",
		Identities:       wiringIdentities{id: "admin"},
		SessionValidator: func(*http.Request) (string, bool) { return "admin", true },
	}))
	t.Cleanup(srv.Close)
	return &remoteHTTP{srv}
}

func (s *remoteHTTP) call(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.URL+"/api/settings/remote-access"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet {
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

const firstStepBody = `{"api_token":"candidate","account_id":"account","enabled":false,"zone_name":"","public_label":"","generation":0}`

func TestStatusDuringAnOutageNeverAsksForThePhoneTest(t *testing.T) {
	state := acceptedInstall()
	state.Phase, state.Reason, state.ObservedHealthy = remotetunnel.PhaseDegraded, remotetunnel.ReasonTunnelDown, false
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	srv := serveRemoteAccess(t, cloud.controller(&remoteMemoryStore{state: state}, &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}, nil))
	code, body := srv.call(t, "GET", "", "")
	for _, want := range []string{`"accepted":true`, `"reachable":false`, `"reason":"tunnel_down"`, `"abandoned":[]`, `"nameservers":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s", want)
		}
	}
	if code != 200 || strings.Contains(body, "acceptance_required") {
		t.Fatalf("status=%d body=%s", code, body)
	}
}

func TestStatusWithoutATokenAsksNoOne(t *testing.T) {
	c := newRemoteAccessController(&remoteMemoryStore{state: acceptedInstall()}, &remoteMemorySecrets{values: map[string]string{}}, &remoteRecordingProjection{}, nil, nil)
	c.client = func(string) *cloudflareapi.Client {
		t.Error("a status with no token built a Cloudflare client")
		return cloudflareapi.New("", "", nil)
	}
	got, err := c.Status(t.Context())
	if err != nil || got.APITokenSet || got.AccountName != "" || got.AccountID != "account-a" {
		t.Fatalf("err=%v status=%+v", err, got)
	}
}

func TestStatusDoesNotWaitForASlowAccountLookup(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	cloud.hangAccounts = true
	c := cloud.controller(&remoteMemoryStore{state: acceptedInstall()}, &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}, nil)
	old := accountLookupTimeout
	accountLookupTimeout = 50 * time.Millisecond
	t.Cleanup(func() { accountLookupTimeout = old })
	done := make(chan agui.RemoteAccessStatus, 1)
	go func() {
		got, _ := c.Status(context.Background())
		done <- got
	}()
	select {
	case got := <-done:
		if got.AccountName != "" || got.AccountID != "account-a" {
			t.Fatalf("status=%+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a status read waited on the account name")
	}
}

func TestVerifyWithNoAccountsAnswersNoAccounts(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts(), 200)
	srv := serveRemoteAccess(t, cloud.controller(&remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}, nil))
	code, body := srv.call(t, "POST", "/token/verify", `{"api_token":"candidate"}`)
	if code != 400 || !strings.Contains(body, `"reason":"no_accounts"`) {
		t.Fatalf("status=%d body=%s", code, body)
	}
}

func TestVerifyWithAnInactiveTokenAnswersTokenRejected(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account"), 200)
	cloud.tokenStatus = "expired"
	srv := serveRemoteAccess(t, cloud.controller(&remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}, nil))
	code, body := srv.call(t, "POST", "/token/verify", `{"api_token":"candidate"}`)
	if code != 400 || !strings.Contains(body, `"reason":"token_rejected"`) {
		t.Fatalf("status=%d body=%s", code, body)
	}
}

func TestConfigureUnknownAccountAnswersAccountNotVisible(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account"), 200)
	store, secrets := &remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}
	srv := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets)))
	code, body := srv.call(t, "PUT", "", `{"enabled":true,"account_id":"other","zone_name":"example.com","api_token":"candidate"}`)
	if code != 400 || !strings.Contains(body, `"reason":"account_not_visible"`) || len(secrets.values) != 0 {
		t.Fatalf("status=%d body=%s stored=%v", code, body, secrets.values)
	}
}

func TestConfigureEnabledWithoutADomainIsDomainInvalid(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account"), 200)
	store, secrets := &remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}
	srv := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets)))
	code, body := srv.call(t, "PUT", "", `{"enabled":true,"account_id":"account","api_token":"candidate"}`)
	if code != 400 || !strings.Contains(body, `"reason":"domain_invalid"`) || len(secrets.values) != 0 {
		t.Fatalf("status=%d body=%s stored=%v", code, body, secrets.values)
	}
}

func TestConfigureWithoutADomainSavesTheTokenAndAccount(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account"), 200)
	store, secrets := &remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}
	srv := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets)))
	code, body := srv.call(t, "PUT", "", firstStepBody)
	want := remotetunnel.Desired{AccountID: "account"}
	if code != 202 || secrets.values["CLOUDFLARE_API_TOKEN"] != "candidate" || store.state.Desired != want || cloud.zones.Load() != 0 {
		t.Fatalf("status=%d body=%s token=%q desired=%+v zone calls=%d", code, body, secrets.values["CLOUDFLARE_API_TOKEN"], store.state.Desired, cloud.zones.Load())
	}
}

func TestReloadBetweenCloudflareAndDomainKeepsTheToken(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account"), 200)
	store, secrets := &remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}
	if code, body := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets))).call(t, "PUT", "", firstStepBody); code != 202 {
		t.Fatalf("status=%d body=%s", code, body)
	}
	code, body := serveRemoteAccess(t, cloud.controller(store, secrets, nil)).call(t, "GET", "", "")
	var got agui.RemoteAccessStatus
	if err := json.Unmarshal([]byte(body), &got); err != nil || code != 200 || !got.APITokenSet || got.AccountID != "account" || got.AccountName != "Acme" || got.ZoneName != "" || got.PublicHostname != "" || got.Enabled {
		t.Fatalf("status=%d err=%v body=%s", code, err, body)
	}
}

func TestTokenReplacementKeepsTheAddressAndTheAcceptance(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	store := &remoteMemoryStore{state: acceptedInstall()}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	srv := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets)))
	_, statusBody := srv.call(t, "GET", "", "")
	var before agui.RemoteAccessStatus
	if err := json.Unmarshal([]byte(statusBody), &before); err != nil {
		t.Fatal(err)
	}
	put, _ := json.Marshal(agui.RemoteAccessConfiguration{Enabled: before.Enabled, Generation: before.Generation, AccountID: before.AccountID, ZoneName: before.ZoneName, PublicLabel: before.PublicLabel, APIToken: "token-b"})
	if code, body := srv.call(t, "PUT", "", string(put)); code != 202 {
		t.Fatalf("status=%d body=%s", code, body)
	}
	_, afterBody := srv.call(t, "GET", "", "")
	s := store.state
	if secrets.values["CLOUDFLARE_API_TOKEN"] != "token-b" || !s.Desired.Enabled || s.Desired.ZoneName != "a.it" || s.Desired.PublicLabel != "aura" ||
		s.AcceptedHostname != "aura.a.it" || !strings.Contains(afterBody, `"accepted":true`) || !strings.Contains(afterBody, `"public_hostname":"aura.a.it"`) {
		t.Fatalf("state=%+v body=%s", s, afterBody)
	}
}

func TestBlankBodyOnAConfiguredInstallIsRefused(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	before := acceptedInstall()
	store := &remoteMemoryStore{state: before}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	projection := &remoteRecordingProjection{}
	c := newRemoteAccessController(store, secrets, projection, nil, memoryCommit(store, secrets))
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(cloud.URL, token, cloud.Client()) }
	code, body := serveRemoteAccess(t, c).call(t, "PUT", "", `{"api_token":"token-b","account_id":"account-a","enabled":false,"zone_name":"","public_label":"","generation":3}`)
	if code != 400 || !strings.Contains(body, `"reason":"domain_invalid"`) {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if !reflect.DeepEqual(store.state, before) || secrets.values["CLOUDFLARE_API_TOKEN"] != "token-a" || len(projection.states) != 0 || cloud.accounts.Load() != 0 {
		t.Fatalf("a refused save touched something: state=%+v token=%q projection=%v accounts=%d", store.state, secrets.values["CLOUDFLARE_API_TOKEN"], projection.states, cloud.accounts.Load())
	}
}

// deletedInstall is the row Delete leaves: no resources and no acceptance, with the address the
// operator typed still in place.
func deletedInstall() remotetunnel.State {
	return remotetunnel.State{Generation: 4, Phase: remotetunnel.PhaseDisabled, Desired: remotetunnel.Desired{AccountID: "account-a", ZoneName: "a.it", PublicLabel: "aura"}}
}

func TestStaleBlankSaveAnswersTheRefreshMessage(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	store := &remoteMemoryStore{state: acceptedInstall()}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	code, body := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets))).call(t, "PUT", "", `{"api_token":"token-b","account_id":"account-a","enabled":false,"zone_name":"","public_label":"","generation":2}`)
	if code != 409 || !strings.Contains(body, `"reason":""`) {
		t.Fatalf("status=%d body=%s", code, body)
	}
}

func TestSaveAfterDeleteCarryingTheAddressIsAccepted(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	store := &remoteMemoryStore{state: deletedInstall()}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	code, body := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets))).call(t, "PUT", "", `{"api_token":"token-b","account_id":"account-a","enabled":true,"zone_name":"a.it","public_label":"aura","generation":4}`)
	if code != 202 || !store.state.Desired.Enabled || secrets.values["CLOUDFLARE_API_TOKEN"] != "token-b" {
		t.Fatalf("status=%d body=%s state=%+v", code, body, store.state.Desired)
	}
}

func TestBlankSaveAfterDeleteIsRefused(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	before := deletedInstall()
	store := &remoteMemoryStore{state: before}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	code, body := serveRemoteAccess(t, cloud.controller(store, secrets, memoryCommit(store, secrets))).call(t, "PUT", "", `{"api_token":"token-b","account_id":"account-a","enabled":false,"zone_name":"","public_label":"","generation":4}`)
	if code != 400 || !strings.Contains(body, `"reason":"domain_invalid"`) || !reflect.DeepEqual(store.state, before) || secrets.values["CLOUDFLARE_API_TOKEN"] != "token-a" {
		t.Fatalf("status=%d body=%s state=%+v", code, body, store.state)
	}
}
