package remotetunnel

import (
	"context"
	"errors"
	"strings"
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

func TestStoreStaleGenerationAndErrorSanitization(t *testing.T) {
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
	if _, err := s.Advance(t.Context(), State{Generation: 7, LastError: "secret-token"}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal(err)
	}
	for _, arg := range db.args {
		if text, ok := arg.(string); ok && strings.Contains(text, "secret-token") {
			t.Fatal("raw error persisted")
		}
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
		PublicLabel: "public", WarpLabel: "warp", PublicDnsID: "pd", WarpDnsID: "wd", OtpIdpID: "otp", PublicAppID: "pa", PublicPolicyID: "pp",
		WarpAppID: "wa", WarpPolicyID: "wp", WarpPostureID: "gateway", ObservedHealthy: true, UpdatedBy: "admin",
		LastReconciledAt: pgtype.Timestamptz{Time: now, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}
	s := stateFromRow(row)
	if s.Resources != (Resources{AccountID: "a", ZoneID: "z", TunnelID: "t", PublicDNSID: "pd", WARPDNSID: "wd", OTPProviderID: "otp", PublicAppID: "pa", PublicPolicyID: "pp", WARPAppID: "wa", WARPPolicyID: "wp", GatewayPostureID: "gateway"}) {
		t.Fatalf("resources=%+v", s.Resources)
	}
	if s.Desired != (Desired{Enabled: true, AccountID: "a", ZoneName: "example.com", PublicLabel: "public", WARPLabel: "warp"}) || s.Generation != 9 || s.Phase != PhaseHealthy || !s.ObservedHealthy || s.TunnelName != "owned" || s.UpdatedBy != "admin" || s.LastReconciledAt == nil || !s.LastReconciledAt.Equal(now) {
		t.Fatalf("state=%+v", s)
	}
	if stateFromRow(sqlc.AuraCloudflareRemoteAccess{}).LastReconciledAt != nil {
		t.Fatal("absent timestamp became present")
	}
}
