package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

// wizardCloudflare answers the reads a save and a status make, and counts them. It fails the test on
// anything else: the wizard's steps never write to Cloudflare.
type wizardCloudflare struct {
	*httptest.Server
	accounts, zones atomic.Int32
	// tokenStatus is what /user/tokens/verify reports; hangAccounts holds /accounts open until the caller gives up.
	tokenStatus  string
	hangAccounts bool
}

func newWizardCloudflare(t *testing.T, accountsBody string, accountsStatus int) *wizardCloudflare {
	t.Helper()
	f := &wizardCloudflare{tokenStatus: "active"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user/tokens/verify":
			_, _ = w.Write([]byte(`{"success":true,"result":{"status":"` + f.tokenStatus + `"}}`))
		case "/accounts":
			f.accounts.Add(1)
			if f.hangAccounts {
				<-r.Context().Done()
				return
			}
			w.WriteHeader(accountsStatus)
			_, _ = w.Write([]byte(accountsBody))
		case "/zones":
			f.zones.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`))
		default:
			t.Errorf("unexpected Cloudflare request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func acmeAccounts(ids ...string) string {
	list := []string{}
	for _, id := range ids {
		list = append(list, `{"id":"`+id+`","name":"Acme"}`)
	}
	return `{"success":true,"result":[` + strings.Join(list, ",") + `],"result_info":{"page":1,"total_pages":1}}`
}

func (f *wizardCloudflare) controller(store *remoteMemoryStore, secrets *remoteMemorySecrets, commit remoteDesiredCommit) *remoteAccessController {
	c := newRemoteAccessController(store, secrets, &remoteRecordingProjection{}, nil, commit)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(f.URL, token, f.Client()) }
	return c
}

// memoryCommit is remoteAccessCommit over the memory fakes: the token and the desired state.
func memoryCommit(store *remoteMemoryStore, secrets *remoteMemorySecrets) remoteDesiredCommit {
	return func(ctx context.Context, generation int64, desired remotetunnel.Desired, token string, _ []remotetunnel.AbandonedResource, actor string) error {
		if _, err := store.SaveDesired(ctx, generation, desired, actor); err != nil {
			return err
		}
		_, err := secrets.Upsert(ctx, "CLOUDFLARE_API_TOKEN", token, actor)
		return err
	}
}

func acceptedInstall() remotetunnel.State {
	s := provisionedInAccountA()
	s.AcceptedHostname, s.ObservedHealthy = "aura.a.it", true
	return s
}

func TestStatusCarriesReasonAcceptanceAndAccountName(t *testing.T) {
	checked := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	state := acceptedInstall()
	state.Phase, state.Reason, state.ObservedHealthy, state.LastReconciledAt = remotetunnel.PhaseDegraded, remotetunnel.ReasonTunnelDown, false, &checked
	state.Abandoned = []remotetunnel.AbandonedResource{{Kind: "tunnel", Name: "aura-old"}}
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	c := cloud.controller(&remoteMemoryStore{state: state}, secrets, nil)

	got, err := c.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := agui.RemoteAccessStatus{
		Enabled: true, Phase: "degraded", PublicHostname: "aura.a.it", APITokenSet: true, AccountID: "account-a", AccountName: "Acme",
		ZoneName: "a.it", PublicLabel: "aura", Reason: "tunnel_down", Accepted: true, Reachable: false,
		LastCheckedAt: &checked, Abandoned: state.Abandoned, Generation: 3, HasResources: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status=%+v\nwant   %+v", got, want)
	}
	if _, err = c.Status(t.Context()); err != nil || cloud.accounts.Load() != 1 {
		t.Fatalf("err=%v: the account name is looked up %d times, want once per token", err, cloud.accounts.Load())
	}
}

func TestHasResourcesFollowsCloudflareNotAcceptance(t *testing.T) {
	never := provisionedInAccountA()
	never.Phase, never.Desired.Enabled = remotetunnel.PhaseDisabled, false
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	got, err := cloud.controller(&remoteMemoryStore{state: never}, secrets, nil).Status(t.Context())
	if err != nil || !got.HasResources || got.Accepted {
		t.Fatalf("a disabled row never accepted: err=%v status=%+v", err, got)
	}

	// Only the zone: Delete detaches it rather than deleting it, so this Cloudflare needs no delete route.
	zoneOnly := remotetunnel.State{Generation: 3, Phase: remotetunnel.PhaseDegraded, TunnelName: oldOwner, OwnedHostname: "aura.a.it", OwnedZoneName: "a.it",
		Desired:   remotetunnel.Desired{Enabled: true, AccountID: "account-a", ZoneName: "a.it", PublicLabel: "aura"},
		Resources: remotetunnel.Resources{AccountID: "account-a", ZoneID: "zone"}}
	c := cloud.controller(&remoteMemoryStore{state: zoneOnly}, secrets, nil)
	if got, err = c.Status(t.Context()); err != nil || !got.HasResources {
		t.Fatalf("before Delete: err=%v status=%+v", err, got)
	}
	if err = c.Action(t.Context(), "delete", "aura.a.it", 3, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, err = c.Status(t.Context()); err != nil || got.HasResources || got.Phase != "disabled" || got.PublicHostname != "aura.a.it" {
		t.Fatalf("after Delete: err=%v status=%+v", err, got)
	}
}

func TestStatusSurvivesAFailedAccountLookup(t *testing.T) {
	cloud := newWizardCloudflare(t, `{"success":false,"errors":[{"code":10000,"message":"secret-abc"}]}`, 403)
	c := cloud.controller(&remoteMemoryStore{state: acceptedInstall()}, &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}, nil)
	got, err := c.Status(t.Context())
	if err != nil || got.AccountName != "" || got.AccountID != "account-a" || !got.APITokenSet {
		t.Fatalf("err=%v status=%+v", err, got)
	}
}

func TestAccountNamesAreForgottenWhenTheTokenChanges(t *testing.T) {
	cloud := newWizardCloudflare(t, acmeAccounts("account-a"), 200)
	store := &remoteMemoryStore{state: acceptedInstall()}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}
	c := cloud.controller(store, secrets, memoryCommit(store, secrets))
	if _, err := c.Status(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.accountNames["account-gone"] = "Old name"
	in := agui.RemoteAccessConfiguration{Enabled: true, Generation: 3, AccountID: "account-a", ZoneName: "a.it", PublicLabel: "aura", APIToken: "token-b"}
	if err := c.Configure(t.Context(), in, "admin"); err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"account-a": "Acme"}; !reflect.DeepEqual(c.accountNames, want) {
		t.Fatalf("names=%v want exactly what the new token sees: %v", c.accountNames, want)
	}
}

func TestNameserversAreReadForTheAccountTheZoneWasCreatedIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/zones/zone":
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"zone","name":"a.it","status":"pending","account":{"id":"account-a"},"name_servers":["ada.ns.cloudflare.com","bob.ns.cloudflare.com"]}}`))
		default:
			_, _ = w.Write([]byte(acmeAccounts("account-a", "account-b")))
		}
	}))
	defer server.Close()
	state := provisionedInAccountA()
	state.Phase = remotetunnel.PhaseWaitingNameservers
	state.Desired.AccountID = "account-b"
	c := newRemoteAccessController(&remoteMemoryStore{state: state}, &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-a"}}, &remoteRecordingProjection{}, nil, nil)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
	got, err := c.Status(t.Context())
	if err != nil || !reflect.DeepEqual(got.Nameservers, []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"}) {
		t.Fatalf("err=%v nameservers=%v", err, got.Nameservers)
	}
}
