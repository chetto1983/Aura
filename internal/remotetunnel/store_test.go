package remotetunnel

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubDB struct {
	err  error
	args []any
}
type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }
func (*stubDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}
func (*stubDB) Query(context.Context, string, ...any) (pgx.Rows, error) { panic("unexpected Query") }
func (d *stubDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if len(args) > 0 {
		d.args = args
	}
	return errorRow{d.err}
}

func TestStoreStaleGenerationAndReasonIsACode(t *testing.T) {
	db := &stubDB{err: pgx.ErrNoRows}
	s := NewStore(db)
	if _, err := s.Load(t.Context()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	if _, err := s.SaveDesired(t.Context(), 7, Desired{}, "admin"); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal(err)
	}
	if db.args[len(db.args)-1] != int64(7) {
		t.Fatal("save lost generation")
	}
	if _, err := s.Advance(t.Context(), State{Generation: 7, Reason: ReasonUnknown}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal(err)
	}
	if !slices.Contains(db.args, any(string(ReasonUnknown))) {
		t.Fatal("reason code not persisted as given")
	}
	if db.args[len(db.args)-1] != int64(7) {
		t.Fatal("advance lost generation")
	}
	db.err = errors.New("database unavailable")
	if _, err := s.Advance(t.Context(), State{}); err != db.err {
		t.Fatal("database error lost")
	}
}

func TestStateMappingPreservesResumableResources(t *testing.T) {
	now := time.Now().UTC()
	row := sqlc.AuraCloudflareRemoteAccess{
		Enabled: true, Generation: 9, Phase: "healthy", AccountID: "a", ZoneID: "z", ZoneName: "example.com", TunnelID: "t", TunnelName: "owned",
		PublicLabel: "public", WarpLabel: "warp", Reason: "tunnel_down", OwnedHostname: "public.example.com", AcceptedHostname: "public.example.com", Abandoned: []byte(`[{"kind":"tunnel","name":"aura-1"}]`), PublicDnsID: "pd", WarpDnsID: "wd", OtpIdpID: "otp", PublicAppID: "pa", PublicPolicyID: "pp",
		WarpAppID: "wa", WarpPolicyID: "wp", WarpPostureID: "gateway", ObservedHealthy: true, UpdatedBy: "admin",
		LastReconciledAt: pgtype.Timestamptz{Time: now, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}
	s, err := stateFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if s.Resources != (Resources{AccountID: "a", ZoneID: "z", TunnelID: "t", PublicDNSID: "pd", WARPDNSID: "wd", OTPProviderID: "otp", PublicAppID: "pa", PublicPolicyID: "pp", WARPAppID: "wa", WARPPolicyID: "wp", GatewayPostureID: "gateway", LegacyWARPHostname: "warp.example.com"}) {
		t.Fatalf("resources=%+v", s.Resources)
	}
	if s.Desired != (Desired{Enabled: true, AccountID: "a", ZoneName: "example.com", PublicLabel: "public"}) || s.Reason != ReasonTunnelDown || s.OwnedHostname != "public.example.com" || s.AcceptedHostname != "public.example.com" || !slices.Equal(s.Abandoned, []AbandonedResource{{Kind: "tunnel", Name: "aura-1"}}) || s.Generation != 9 || s.Phase != PhaseHealthy || !s.ObservedHealthy || s.TunnelName != "owned" || s.UpdatedBy != "admin" || s.LastReconciledAt == nil || !s.LastReconciledAt.Equal(now) {
		t.Fatalf("state=%+v", s)
	}
	if empty, err := stateFromRow(sqlc.AuraCloudflareRemoteAccess{}); err != nil || empty.LastReconciledAt != nil || len(empty.Abandoned) != 0 {
		t.Fatalf("empty row: %+v err=%v", empty, err)
	}
}

func TestStateMappingLeavesLegacyWARPHostnameEmptyWithoutWARPResources(t *testing.T) {
	s, err := stateFromRow(sqlc.AuraCloudflareRemoteAccess{WarpLabel: "aura-warp", ZoneName: "example.com", Abandoned: []byte("[]")})
	if err != nil || s.Resources.LegacyWARPHostname != "" || len(s.Abandoned) != 0 {
		t.Fatalf("legacy=%q abandoned=%v", s.Resources.LegacyWARPHostname, s.Abandoned)
	}
}

func TestStateMappingSurfacesAMisShapedAbandonedList(t *testing.T) {
	if _, err := stateFromRow(sqlc.AuraCloudflareRemoteAccess{Abandoned: []byte(`{"kind":1}`)}); err == nil {
		t.Fatal("a mis-shaped abandoned column was read as an empty list")
	}
}

func TestAbandonPassesTheListAndGenerationAndMapsNoRowToStale(t *testing.T) {
	db := &stubDB{err: pgx.ErrNoRows}
	s := NewStore(db)
	_, err := s.Abandon(t.Context(), 7, []AbandonedResource{{Kind: "tunnel", Name: "aura-1"}})
	if !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("no-row abandon: %v", err)
	}
	if !slices.Equal(db.args[0].([]byte), []byte(`[{"kind":"tunnel","name":"aura-1"}]`)) || db.args[1] != int64(7) {
		t.Fatalf("args=%v", db.args)
	}
	if _, err = s.Abandon(t.Context(), 7, nil); !errors.Is(err, ErrStaleGeneration) || string(db.args[0].([]byte)) != "[]" {
		t.Fatalf("nil list=%s err=%v", db.args[0], err)
	}
}

func TestDismissAbandonedPropagatesADatabaseError(t *testing.T) {
	want := errors.New("database unavailable")
	if err := NewStore(&stubDB{err: want}).DismissAbandoned(t.Context()); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
