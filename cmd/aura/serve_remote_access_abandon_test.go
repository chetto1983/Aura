package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

const (
	oldTunnelUUID = "6f1c1d0e-2a52-4a0b-9f0c-1d6d6a7f3b11"
	oldOwner      = "aura-" + oldTunnelUUID
)

// connectorToken is what Cloudflare hands out for a tunnel: base64 of JSON with the account, the
// tunnel and its secret (cloudflared connection/connection.go, TunnelToken).
func connectorToken(t *testing.T, tunnelID string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"a": "account-a", "t": tunnelID, "s": base64.StdEncoding.EncodeToString([]byte("secret"))})
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// provisionedInAccountA is an install whose public address was built in account A.
func provisionedInAccountA() remotetunnel.State {
	return remotetunnel.State{
		Generation: 3, Phase: remotetunnel.PhaseHealthy, TunnelName: oldOwner, OwnedHostname: "aura.a.it", OwnedZoneName: "a.it",
		Desired: remotetunnel.Desired{Enabled: true, AccountID: "account-a", ZoneName: "a.it", PublicLabel: "aura"},
		Resources: remotetunnel.Resources{AccountID: "account-a", ZoneID: "zone", TunnelID: oldTunnelUUID, PublicDNSID: "dns",
			PublicAppID: "app", PublicPolicyID: "policy", OTPProviderID: "otp"},
	}
}

// accountsServer is a Cloudflare that only verifies a token and lists accounts and zones, and
// fails the test on anything else: deleting is not what a token replacement does.
func accountsServer(t *testing.T, accountIDs ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user/tokens/verify":
			_, _ = w.Write([]byte(`{"success":true,"result":{"status":"active"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts":
			accounts := []string{}
			for _, id := range accountIDs {
				accounts = append(accounts, `{"id":"`+id+`"}`)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":[` + strings.Join(accounts, ",") + `],"result_info":{"page":1,"total_pages":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`))
		default:
			t.Errorf("unexpected Cloudflare request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

type commitCall struct {
	generation int64
	token      string
	abandoned  []remotetunnel.AbandonedResource
}

// configureAs saves desired through a controller whose store holds state and whose token sees
// only the given accounts. It reports what the commit was asked to do.
func configureAs(t *testing.T, state remotetunnel.State, in agui.RemoteAccessConfiguration, accounts ...string) (*commitCall, error) {
	t.Helper()
	server := accountsServer(t, accounts...)
	var got *commitCall
	commit := func(_ context.Context, g int64, _ remotetunnel.Desired, token string, abandoned []remotetunnel.AbandonedResource, _ string) error {
		got = &commitCall{g, token, abandoned}
		return nil
	}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "stored"}}
	c := newRemoteAccessController(&remoteMemoryStore{state: state}, secrets, &remoteRecordingProjection{}, nil, commit)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
	err := c.Configure(t.Context(), in, "admin")
	return got, err
}

func toAccountB(token string) agui.RemoteAccessConfiguration {
	return agui.RemoteAccessConfiguration{Enabled: true, Generation: 3, AccountID: "account-b", ZoneName: "b.it", APIToken: token}
}

func TestConfigureWithATokenBlindToTheOldAccountAbandonsAndSaves(t *testing.T) {
	got, err := configureAs(t, provisionedInAccountA(), toAccountB("token-b"), "account-b")
	if err != nil || got == nil {
		t.Fatalf("err=%v commit=%v", err, got)
	}
	want := []remotetunnel.AbandonedResource{
		{Kind: "tunnel", Name: oldOwner},
		{Kind: "dns", Name: "aura.a.it"},
		{Kind: "access_app", Name: oldOwner + "-public"},
		{Kind: "access_policy", Name: oldOwner + "-public-members"},
	}
	if !reflect.DeepEqual(got.abandoned, want) || got.token != "token-b" || got.generation != 3 {
		t.Fatalf("commit=%+v\nwant abandoned %v", got, want)
	}
}

func TestSameTokenSaveAbandonsWhatItCannotSee(t *testing.T) {
	state := provisionedInAccountA()
	state.Phase, state.Reason = remotetunnel.PhaseError, remotetunnel.ReasonAccountNotVisible
	got, err := configureAs(t, state, toAccountB(""), "account-b")
	if err != nil || got == nil || got.token != "stored" || len(got.abandoned) != 4 {
		t.Fatalf("err=%v commit=%+v: a blind token has no way out of account_not_visible", err, got)
	}
}

func TestConfigureMidTeardownAbandonsWhatItCannotSee(t *testing.T) {
	state := provisionedInAccountA()
	state.Phase, state.Reason = remotetunnel.PhaseProvisioning, remotetunnel.ReasonAccountNotVisible
	state.Resources.PublicDNSID = "" // teardown deletes the CNAME first
	got, err := configureAs(t, state, toAccountB("token-b"), "account-b")
	want := []remotetunnel.AbandonedResource{
		{Kind: "tunnel", Name: oldOwner},
		{Kind: "access_app", Name: oldOwner + "-public"},
		{Kind: "access_policy", Name: oldOwner + "-public-members"},
	}
	if err != nil || got == nil || !reflect.DeepEqual(got.abandoned, want) {
		t.Fatalf("err=%v commit=%+v, want what teardown had not reached", err, got)
	}
}

func TestAbandonWhileTheZoneIsPending(t *testing.T) {
	state := provisionedInAccountA()
	state.Phase = remotetunnel.PhaseWaitingNameservers
	state.Resources.PublicDNSID, state.Resources.PublicAppID, state.Resources.PublicPolicyID, state.Resources.OTPProviderID = "", "", "", ""
	got, err := configureAs(t, state, toAccountB("token-b"), "account-b")
	want := []remotetunnel.AbandonedResource{{Kind: "tunnel", Name: oldOwner}}
	if err != nil || got == nil || !reflect.DeepEqual(got.abandoned, want) {
		t.Fatalf("err=%v commit=%+v", err, got)
	}
}

func TestAbandonOfARowWithoutARecordedAccountStillForgetsIt(t *testing.T) {
	state := provisionedInAccountA()
	state.Resources.AccountID = ""
	got, err := configureAs(t, state, toAccountB("token-b"), "account-b")
	if err != nil || got == nil || len(got.abandoned) != 4 {
		t.Fatalf("err=%v commit=%+v", err, got)
	}
}

func TestAbandonWithOnlyAZoneStillForgetsIt(t *testing.T) {
	state := remotetunnel.State{Generation: 3, Phase: remotetunnel.PhaseWaitingNameservers,
		Desired:   remotetunnel.Desired{Enabled: true, AccountID: "account-a", ZoneName: "a.it", PublicLabel: "aura"},
		Resources: remotetunnel.Resources{AccountID: "account-a", ZoneID: "zone"}}
	got, err := configureAs(t, state, toAccountB("token-b"), "account-b")
	if err != nil || got == nil || got.abandoned == nil || len(got.abandoned) != 0 {
		t.Fatalf("err=%v commit=%+v, want an abandonment with nothing to list", err, got)
	}
}

func TestConfigureWithATokenThatSeesTheOldAccountAbandonsNothing(t *testing.T) {
	in := toAccountB("token-b")
	for name, tc := range map[string]struct {
		state remotetunnel.State
		in    agui.RemoteAccessConfiguration
	}{
		"another domain in the same account": {provisionedInAccountA(), agui.RemoteAccessConfiguration{Enabled: true, Generation: 3, AccountID: "account-a", ZoneName: "c.it", APIToken: "token-b"}},
		"both accounts":                      {provisionedInAccountA(), in},
		"nothing built yet":                  {remotetunnel.State{Generation: 3, Resources: remotetunnel.Resources{AccountID: "account-a"}}, in},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := configureAs(t, tc.state, tc.in, "account-a", "account-b")
			if err != nil || got == nil || got.abandoned != nil {
				t.Fatalf("err=%v commit=%+v", err, got)
			}
		})
	}
}

func TestRestartRefusesACredentialForAnotherTunnel(t *testing.T) {
	const newer = "0c6b6a20-7d9d-4f55-a0c9-5a1d2f2b8e22"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	state := &remoteMemoryStore{state: remotetunnel.State{Generation: 7, Desired: remotetunnel.Desired{Enabled: true}, Resources: remotetunnel.Resources{TunnelID: newer}}}
	for name, token := range map[string]string{"another tunnel": connectorToken(t, oldTunnelUUID), "not a credential": "stored-tunnel-token"} {
		t.Run(name, func(t *testing.T) {
			projection := &remoteRecordingProjection{}
			secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_TUNNEL_TOKEN": token}}
			c := newRemoteAccessController(state, secrets, projection, nil, nil)
			if err := c.rebuild(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(projection.states) != 1 || projection.states[0].Enabled || projection.states[0].Token.Reveal() != "" {
				t.Fatalf("a credential for another tunnel was projected: %+v", projection.states)
			}
		})
	}
	if !strings.Contains(logs.String(), newer) || strings.Contains(logs.String(), "stored-tunnel-token") {
		t.Fatalf("the mismatch must be logged without the credential: %s", logs.String())
	}
}

func TestTunnelOfCredential(t *testing.T) {
	if id, err := tunnelOfCredential(connectorToken(t, oldTunnelUUID)); err != nil || id != oldTunnelUUID {
		t.Fatalf("id=%q err=%v", id, err)
	}
	for name, token := range map[string]string{
		"empty":        "",
		"not base64":   "%%%",
		"not json":     base64.StdEncoding.EncodeToString([]byte("tunnel")),
		"no tunnel":    base64.StdEncoding.EncodeToString([]byte(`{"a":"account"}`)),
		"empty tunnel": base64.StdEncoding.EncodeToString([]byte(`{"t":""}`)),
	} {
		if id, err := tunnelOfCredential(token); err == nil || id != "" {
			t.Fatalf("%s: id=%q err=%v", name, id, err)
		}
	}
}

// flakyProjection fails exactly one Apply, the failOn-th.
type flakyProjection struct {
	mu     sync.Mutex
	failOn int
	states []remotetunnel.ProjectionState
}

func (p *flakyProjection) Apply(_ context.Context, state remotetunnel.ProjectionState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states = append(p.states, state)
	if len(p.states) == p.failOn {
		return errors.New("read-only file system")
	}
	return nil
}

func (p *flakyProjection) applied() []remotetunnel.ProjectionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]remotetunnel.ProjectionState(nil), p.states...)
}

func TestDaemonStopsTheConnectorAfterAFailedProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))
	defer server.Close()
	state := &remoteMemoryStore{state: remotetunnel.State{Generation: 5, Phase: remotetunnel.PhaseValidating,
		Desired:   remotetunnel.Desired{Enabled: true, AccountID: "account-b", ZoneName: "b.it", PublicLabel: "aura"},
		Resources: remotetunnel.Resources{AccountID: "account-b"}}}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "token-b"}}
	members := remoteIdentityMembers{store: remoteTestIdentities{identities: []identity.Identity{{ID: "admin", Name: "admin@example.com", Kind: "user"}}}}
	// The first Apply is the boot rebuild; the second is the first pass, which fails.
	projection := &flakyProjection{failOn: 2}
	c := newRemoteAccessController(state, secrets, projection, members, nil)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
	c.Start(t.Context())
	defer c.Close()
	deadline := time.After(10 * time.Second)
	for len(projection.applied()) < 3 {
		select {
		case <-deadline:
			t.Fatalf("the loop never retried the failed projection: %+v", projection.applied())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if retry := projection.applied()[2]; retry.Enabled {
		t.Fatalf("the retry started a connector: %+v", retry)
	}
}

func TestDismissAbandonedActionClearsTheList(t *testing.T) {
	state := &remoteMemoryStore{state: remotetunnel.State{Generation: 3, Abandoned: []remotetunnel.AbandonedResource{{Kind: "tunnel", Name: oldOwner}}}}
	c := newRemoteAccessController(state, &remoteMemorySecrets{values: map[string]string{}}, &remoteRecordingProjection{}, nil, nil)
	if err := c.Action(t.Context(), "dismiss-abandoned", "", 3, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(state.state.Abandoned) != 0 || state.state.Generation != 3 {
		t.Fatalf("state=%+v: dismissing must only clear the list", state.state)
	}
}
