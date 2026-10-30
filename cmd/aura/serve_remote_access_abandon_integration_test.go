//go:build db_integration

package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/remotetunnel"
	"github.com/chetto1983/aura/internal/settings"
)

const abandonTestSecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

type abandonFixture struct {
	ctx     context.Context
	secrets *settings.Store
	state   *remotetunnel.Store
	commit  remoteDesiredCommit
}

func newAbandonFixture(t *testing.T) *abandonFixture {
	t.Helper()
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "remote access abandon integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	ctx := context.Background()
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	secrets, err := settings.NewStore(pool, abandonTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	f := &abandonFixture{ctx: ctx, secrets: secrets, state: remotetunnel.NewStore(pool), commit: remoteAccessCommit(secrets)}
	t.Cleanup(func() {
		current, e := f.state.Load(ctx)
		if e == nil {
			_, _ = f.state.Abandon(ctx, current.Generation, nil)
			_ = f.state.DismissAbandoned(ctx)
		}
		_ = secrets.Delete(ctx, "CLOUDFLARE_API_TOKEN")
		_ = secrets.Delete(ctx, "CLOUDFLARE_TUNNEL_TOKEN")
	})
	return f
}

// provision puts the row in the state of an install built in account A, with its connector
// credential and the API token that built it stored.
func (f *abandonFixture) provision(t *testing.T) remotetunnel.State {
	t.Helper()
	s, err := f.state.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	built := provisionedInAccountA()
	s.Phase, s.TunnelName, s.OwnedHostname, s.OwnedZoneName, s.Resources = built.Phase, built.TunnelName, built.OwnedHostname, built.OwnedZoneName, built.Resources
	if s, err = f.state.Advance(f.ctx, s); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"CLOUDFLARE_API_TOKEN": "token-a", "CLOUDFLARE_TUNNEL_TOKEN": connectorToken(t, oldTunnelUUID)} {
		if _, err = f.secrets.Upsert(f.ctx, key, value, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (f *abandonFixture) secret(t *testing.T, key string) string {
	t.Helper()
	value, err := f.secrets.Secret(f.ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

var accountB = remotetunnel.Desired{Enabled: true, AccountID: "account-b", ZoneName: "b.it", PublicLabel: "aura"}

func TestAbandonClearsTheCredentialInTheSameCommit(t *testing.T) {
	f := newAbandonFixture(t)
	before := f.provision(t)
	listed := remotetunnel.AbandonedOf(before)

	err := f.commit(f.ctx, before.Generation-1, accountB, "token-b", listed, "admin")
	if !errors.Is(err, remotetunnel.ErrStaleGeneration) {
		t.Fatalf("err=%v, want a stale generation", err)
	}
	after, _ := f.state.Load(f.ctx)
	if f.secret(t, "CLOUDFLARE_TUNNEL_TOKEN") == "" || f.secret(t, "CLOUDFLARE_API_TOKEN") != "token-a" ||
		after.Resources != before.Resources || len(after.Abandoned) != 0 || after.Generation != before.Generation {
		t.Fatalf("a failing commit changed something: %+v", after)
	}

	if err = f.commit(f.ctx, before.Generation, accountB, "token-b", listed, "admin"); err != nil {
		t.Fatal(err)
	}
	after, _ = f.state.Load(f.ctx)
	if f.secret(t, "CLOUDFLARE_TUNNEL_TOKEN") != "" || f.secret(t, "CLOUDFLARE_API_TOKEN") != "token-b" {
		t.Fatal("the old connector credential survives the abandon")
	}
	if after.Resources != (remotetunnel.Resources{}) || after.TunnelName != "" || after.OwnedHostname != "" || after.OwnedZoneName != "" {
		t.Fatalf("resources or ownership survive: %+v", after)
	}
	if !reflect.DeepEqual(after.Abandoned, listed) || after.Desired != accountB || after.Generation != before.Generation+1 {
		t.Fatalf("abandoned=%v desired=%+v generation=%d", after.Abandoned, after.Desired, after.Generation)
	}
}

func TestSuccessiveTokenReplacementsKeepTheList(t *testing.T) {
	f := newAbandonFixture(t)
	posture := remotetunnel.AbandonedResource{Kind: "posture", Name: oldOwner + "-gateway"}
	before := f.provision(t)
	if err := f.state.AddAbandoned(f.ctx, []remotetunnel.AbandonedResource{posture}); err != nil {
		t.Fatal(err)
	}
	first := remotetunnel.AbandonedOf(before)
	if err := f.commit(f.ctx, before.Generation, accountB, "token-b", first, "admin"); err != nil {
		t.Fatal(err)
	}
	again := f.provision(t)
	second := remotetunnel.AbandonedOf(again)
	if err := f.commit(f.ctx, again.Generation, accountB, "token-c", second, "admin"); err != nil {
		t.Fatal(err)
	}
	after, _ := f.state.Load(f.ctx)
	want := append(append([]remotetunnel.AbandonedResource{posture}, first...), second...)
	if !reflect.DeepEqual(after.Abandoned, want) {
		t.Fatalf("abandoned=%v\nwant     %v", after.Abandoned, want)
	}
	if err := f.state.DismissAbandoned(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after, _ = f.state.Load(f.ctx); len(after.Abandoned) != 0 {
		t.Fatalf("dismiss left %v", after.Abandoned)
	}
}

func TestSaveThatAbandonsNothingLeavesTheCredentialAlone(t *testing.T) {
	f := newAbandonFixture(t)
	before := f.provision(t)
	if err := f.commit(f.ctx, before.Generation, accountB, "token-b", nil, "admin"); err != nil {
		t.Fatal(err)
	}
	after, _ := f.state.Load(f.ctx)
	if f.secret(t, "CLOUDFLARE_TUNNEL_TOKEN") == "" || after.Resources != before.Resources || len(after.Abandoned) != 0 {
		t.Fatalf("a plain save forgot something: %+v", after)
	}
}

func TestFirstStepSaveAgainstTheRealStore(t *testing.T) {
	f := newAbandonFixture(t)
	before, err := f.state.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	cloud := newWizardCloudflare(t, acmeAccounts("account"), 200)
	c := newRemoteAccessController(f.state, f.secrets, &remoteRecordingProjection{}, nil, f.commit)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(cloud.URL, token, cloud.Client()) }

	in := decodeConfiguration(t, firstStepBody)
	in.Generation = before.Generation
	if err = c.Configure(f.ctx, in, "admin"); err != nil {
		t.Fatalf("the table refused a row with no domain: %v", err)
	}
	after, err := f.state.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Desired != (remotetunnel.Desired{AccountID: "account"}) || after.Phase != remotetunnel.PhaseDisabled || after.Generation != before.Generation+1 {
		t.Fatalf("desired=%+v phase=%s generation=%d", after.Desired, after.Phase, after.Generation)
	}
	if f.secret(t, "CLOUDFLARE_API_TOKEN") != "candidate" || cloud.zones.Load() != 0 {
		t.Fatalf("token=%q zone calls=%d", f.secret(t, "CLOUDFLARE_API_TOKEN"), cloud.zones.Load())
	}
	status, err := c.Status(f.ctx)
	if err != nil || !status.APITokenSet || status.AccountName != "Acme" || status.PublicHostname != "" || status.Accepted || status.Enabled {
		t.Fatalf("err=%v status=%+v", err, status)
	}
}
