package remotetunnel

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// ErrStaleGeneration means newer operator intent won the compare-and-swap.
var ErrStaleGeneration = errors.New("remote access: desired generation changed")

// Store persists non-secret state through the generated SQL authority.
type Store struct{ q *sqlc.Queries }

// NewStore also accepts a pgx transaction for coordinated state changes.
func NewStore(db sqlc.DBTX) *Store { return &Store{q: sqlc.New(db)} }

// Load resumes the singleton without deriving state from runtime files.
func (s *Store) Load(ctx context.Context) (State, error) {
	row, err := s.q.GetCloudflareRemoteAccess(ctx)
	return stateFromRow(row), err
}

// SaveDesired atomically advances generation and rejects stale operator writes.
func (s *Store) SaveDesired(ctx context.Context, expectedGeneration int64, d Desired, by string) (State, error) {
	row, err := s.q.SaveCloudflareRemoteAccessDesired(ctx, sqlc.SaveCloudflareRemoteAccessDesiredParams{
		ExpectedGeneration: expectedGeneration, Enabled: d.Enabled, AccountID: d.AccountID,
		ZoneName: d.ZoneName, PublicLabel: d.PublicLabel, WarpLabel: d.WARPLabel, UpdatedBy: by,
	})
	return changedState(row, err)
}

// Advance collapses arbitrary error text to a safe diagnostic because response bodies
// and transport errors may contain credentials.
func (s *Store) Advance(ctx context.Context, state State) (State, error) {
	diagnostic := ""
	if state.LastError != "" {
		diagnostic = "Remote access reconciliation failed; retry or check credentials."
	}
	r := state.Resources
	row, err := s.q.AdvanceCloudflareRemoteAccess(ctx, sqlc.AdvanceCloudflareRemoteAccessParams{
		ExpectedGeneration: state.Generation, Phase: string(state.Phase), ZoneID: r.ZoneID,
		TunnelID: r.TunnelID, TunnelName: state.TunnelName, PublicDnsID: r.PublicDNSID, WarpDnsID: r.WARPDNSID,
		OtpIdpID: r.OTPProviderID, PublicAppID: r.PublicAppID, PublicPolicyID: r.PublicPolicyID,
		WarpAppID: r.WARPAppID, WarpPolicyID: r.WARPPolicyID, WarpPostureID: r.GatewayPostureID,
		ObservedHealthy: state.ObservedHealthy, LastError: diagnostic,
	})
	return changedState(row, err)
}

func changedState(row sqlc.AuraCloudflareRemoteAccess, err error) (State, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, ErrStaleGeneration
	}
	return stateFromRow(row), err
}

func stateFromRow(r sqlc.AuraCloudflareRemoteAccess) State {
	s := State{
		Desired:    Desired{Enabled: r.Enabled, AccountID: r.AccountID, ZoneName: r.ZoneName, PublicLabel: r.PublicLabel, WARPLabel: r.WarpLabel},
		Generation: r.Generation, Phase: Phase(r.Phase), TunnelName: r.TunnelName,
		Resources: Resources{AccountID: r.AccountID, ZoneID: r.ZoneID, TunnelID: r.TunnelID, PublicDNSID: r.PublicDnsID, WARPDNSID: r.WarpDnsID,
			OTPProviderID: r.OtpIdpID, PublicAppID: r.PublicAppID, PublicPolicyID: r.PublicPolicyID, WARPAppID: r.WarpAppID,
			WARPPolicyID: r.WarpPolicyID, GatewayPostureID: r.WarpPostureID},
		ObservedHealthy: r.ObservedHealthy, LastError: r.LastError, UpdatedAt: r.UpdatedAt.Time, UpdatedBy: r.UpdatedBy,
	}
	if r.LastReconciledAt.Valid {
		s.LastReconciledAt = &r.LastReconciledAt.Time
	}
	return s
}
