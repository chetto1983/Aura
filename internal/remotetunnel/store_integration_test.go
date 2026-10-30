//go:build db_integration

package remotetunnel

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStorePersistsGenerationAndEncryptedCredentials(t *testing.T) {
	pool := remoteAccessPool(t)
	s := NewStore(pool)
	initial, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	initial.Resources = Resources{AccountID: initial.Resources.AccountID}
	if _, err := s.Advance(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	testStorePersistence(t, s, pool, initial)
}

func remoteAccessPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("AURA_DB_MIGRATE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_MIGRATE_URL required in CI")
		}
		t.Skip("requires disposable PostgreSQL")
	}
	dsn = dbtest.MigrateURL(t, dsn)
	if _, err := db.Migrate(t.Context(), dsn); err != nil {
		t.Fatal(err)
	}
	appDSN := os.Getenv("AURA_DB_URL")
	if appDSN == "" {
		t.Fatal("AURA_DB_URL required for runtime-role verification")
	}
	pool, err := pgxpool.New(t.Context(), dbtest.MigrateURL(t, appDSN))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testStorePersistence(t *testing.T, s *Store, pool *pgxpool.Pool, initial State) {
	t.Helper()
	d := Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}
	state, err := s.SaveDesired(t.Context(), initial.Generation, d, "admin")
	if err != nil || state.Generation != initial.Generation+1 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if _, err := s.SaveDesired(t.Context(), initial.Generation, d, "stale"); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale save: %v", err)
	}
	state.Resources.TunnelID = "tunnel"
	state.Phase = PhaseProvisioning
	if _, err := s.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(pool).Load(t.Context())
	if err != nil || reloaded.Resources.TunnelID != "tunnel" {
		t.Fatalf("resume=%+v err=%v", reloaded, err)
	}
	state.Generation--
	if _, err := s.Advance(t.Context(), state); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale advance: %v", err)
	}
	secrets, err := settings.NewStore(pool, strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"} {
		if _, err := secrets.Upsert(t.Context(), key, "fixture-credential", ""); err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := pool.QueryRow(t.Context(), "SELECT value FROM aura.settings WHERE key=$1", key).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(raw, "enc:v1:") || strings.Contains(raw, "fixture-credential") {
			t.Fatal("plaintext credential in database")
		}
		if got, err := secrets.Secret(t.Context(), key); err != nil || got != "fixture-credential" {
			t.Fatal("decrypt failed")
		}
		if err := secrets.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
}

// blankRemoteAccess returns the singleton with no resources, hostnames, reason or abandoned list.
func blankRemoteAccess(t *testing.T, s *Store) State {
	t.Helper()
	state, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state.Resources = Resources{AccountID: state.Resources.AccountID}
	state.TunnelName, state.OwnedHostname, state.AcceptedHostname = "", "", ""
	state.Reason, state.Phase = "", PhaseDisabled
	if err = s.DismissAbandoned(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = s.Advance(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSaveDesiredAcceptsAnAddressChangeWithResources(t *testing.T) {
	s := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, s)
	state, err := s.SaveDesired(t.Context(), state.Generation, Desired{Enabled: true, AccountID: "account", ZoneName: "old.it", PublicLabel: "aura"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	state.Resources.PublicDNSID = "d1"
	if state, err = s.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDesired(t.Context(), state.Generation, Desired{Enabled: true, AccountID: "account", ZoneName: "new.it", PublicLabel: "aura"}, "admin"); err != nil {
		t.Fatalf("address change refused: %v", err)
	}
	after, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Desired.ZoneName != "new.it" || after.Resources.PublicDNSID != "d1" {
		t.Fatalf("zone=%q public_dns_id=%q", after.Desired.ZoneName, after.Resources.PublicDNSID)
	}
}

func TestSaveDesiredKeepsAcceptanceOnlyForTheSameHostname(t *testing.T) {
	s := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, s)
	d := Desired{Enabled: true, AccountID: "account", ZoneName: "a.it", PublicLabel: "aura"}
	state, err := s.SaveDesired(t.Context(), state.Generation, d, "admin")
	if err != nil {
		t.Fatal(err)
	}
	state.AcceptedHostname = "aura.a.it"
	if state, err = s.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if state, err = s.SaveDesired(t.Context(), state.Generation, d, "admin"); err != nil || state.AcceptedHostname != "aura.a.it" {
		t.Fatalf("same hostname lost acceptance: %q err=%v", state.AcceptedHostname, err)
	}
	d.PublicLabel = "x"
	if state, err = s.SaveDesired(t.Context(), state.Generation, d, "admin"); err != nil || state.AcceptedHostname != "" {
		t.Fatalf("changed hostname kept acceptance: %q err=%v", state.AcceptedHostname, err)
	}
}

func TestAdvancePersistsReasonAndOwnedHostname(t *testing.T) {
	s := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, s)
	state.Reason, state.OwnedHostname = ReasonTunnelDown, "aura.a.it"
	if _, err := s.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Reason != ReasonTunnelDown || got.OwnedHostname != "aura.a.it" {
		t.Fatalf("reason=%q owned=%q", got.Reason, got.OwnedHostname)
	}
}

func TestReasonCheckRejectsAnUnknownCode(t *testing.T) {
	pool := remoteAccessPool(t)
	_, err := pool.Exec(t.Context(), "UPDATE aura.cloudflare_remote_access SET reason = 'boom' WHERE singleton = true")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("want check violation 23514, got %v", err)
	}
}

func TestAbandonClearsResourcesAndListsThem(t *testing.T) {
	s := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, s)
	state.Resources = Resources{AccountID: "account", ZoneID: "z", TunnelID: "t", PublicDNSID: "d", OTPProviderID: "o", PublicAppID: "a", PublicPolicyID: "p"}
	state.TunnelName, state.OwnedHostname, state.AcceptedHostname = "aura-1", "aura.a.it", "aura.a.it"
	state, err := s.Advance(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	list := []AbandonedResource{{Kind: "tunnel", Name: "aura-1"}}
	got, err := s.Abandon(t.Context(), state.Generation, list)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resources != (Resources{AccountID: "account"}) || got.TunnelName != "" || got.OwnedHostname != "" || got.AcceptedHostname != "" {
		t.Fatalf("not cleared: %+v", got)
	}
	if !slices.Equal(got.Abandoned, list) {
		t.Fatalf("abandoned=%v", got.Abandoned)
	}
	if _, err = s.Abandon(t.Context(), state.Generation+1, list); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale abandon: %v", err)
	}
	if err = s.DismissAbandoned(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Load(t.Context()); err != nil || len(got.Abandoned) != 0 {
		t.Fatalf("abandoned after dismiss=%v err=%v", got.Abandoned, err)
	}
}

func TestDismissIsNotUndoneByAPassInFlight(t *testing.T) {
	s := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, s)
	if _, err := s.Abandon(t.Context(), state.Generation, []AbandonedResource{{Kind: "tunnel", Name: "aura-1"}}); err != nil {
		t.Fatal(err)
	}
	inFlight, err := s.Load(t.Context())
	if err != nil || len(inFlight.Abandoned) != 1 {
		t.Fatalf("abandoned=%v err=%v", inFlight.Abandoned, err)
	}
	if err = s.DismissAbandoned(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Advance(t.Context(), inFlight); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(t.Context()); err != nil || len(got.Abandoned) != 0 {
		t.Fatalf("a pass in flight resurrected the list: %v err=%v", got.Abandoned, err)
	}
}

func TestAbandonKeepsWhatTheOperatorHasNotSeen(t *testing.T) {
	s := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, s)
	first := []AbandonedResource{{Kind: "tunnel", Name: "aura-1"}}
	second := []AbandonedResource{{Kind: "dns", Name: "aura.a.it"}}
	if _, err := s.Abandon(t.Context(), state.Generation, first); err != nil {
		t.Fatal(err)
	}
	got, err := s.Abandon(t.Context(), state.Generation, second)
	if err != nil || !slices.Equal(got.Abandoned, append(first, second...)) {
		t.Fatalf("abandoned=%v err=%v", got.Abandoned, err)
	}
	if err = s.AddAbandoned(t.Context(), []AbandonedResource{{Kind: "posture", Name: "aura-1-gateway"}}); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Load(t.Context()); err != nil || len(got.Abandoned) != 3 {
		t.Fatalf("abandoned=%v err=%v", got.Abandoned, err)
	}
}
