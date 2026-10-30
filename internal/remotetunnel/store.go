package remotetunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	if err != nil {
		return State{}, err
	}
	return stateFromRow(row)
}

// SaveDesired atomically advances generation and rejects stale operator writes.
// Persisted deletion intent overrides re-enable requests until cleanup completes;
// its diagnostic survives credential replacement and explicit retries. Disabled +
// error is reserved for failed deletion; disabled membership sync is refused locally.
func (s *Store) SaveDesired(ctx context.Context, expectedGeneration int64, d Desired, by string) (State, error) {
	row, err := s.q.SaveCloudflareRemoteAccessDesired(ctx, sqlc.SaveCloudflareRemoteAccessDesiredParams{
		ExpectedGeneration: expectedGeneration, Enabled: d.Enabled, AccountID: d.AccountID,
		ZoneName: d.ZoneName, PublicLabel: d.PublicLabel, UpdatedBy: by,
	})
	return changedState(row, err)
}

// Advance writes Reason as is: it is a code, never upstream text, which may carry credentials.
func (s *Store) Advance(ctx context.Context, state State) (State, error) {
	r := state.Resources
	row, err := s.q.AdvanceCloudflareRemoteAccess(ctx, sqlc.AdvanceCloudflareRemoteAccessParams{
		ExpectedGeneration: state.Generation, Phase: string(state.Phase), ZoneID: r.ZoneID,
		TunnelID: r.TunnelID, TunnelName: state.TunnelName, PublicDnsID: r.PublicDNSID, WarpDnsID: r.WARPDNSID,
		OtpIdpID: r.OTPProviderID, PublicAppID: r.PublicAppID, PublicPolicyID: r.PublicPolicyID,
		WarpAppID: r.WARPAppID, WarpPolicyID: r.WARPPolicyID, WarpPostureID: r.GatewayPostureID,
		OwnedHostname: state.OwnedHostname, AcceptedHostname: state.AcceptedHostname,
		ResourceAccountID: r.AccountID, OwnedZoneName: state.OwnedZoneName, LegacyWarpHostname: r.LegacyWARPHostname,
		ObservedHealthy: state.ObservedHealthy, Reason: string(state.Reason),
	})
	return changedState(row, err)
}

// Abandon forgets every resource ID and hostname, and appends what was forgotten to the list
// the operator has not seen yet, when a replaced token can no longer reach the account those
// resources live in. It keeps the generation, so the SaveDesired of the same transaction still
// matches.
func (s *Store) Abandon(ctx context.Context, generation int64, list []AbandonedResource) (State, error) {
	abandoned, err := marshalAbandoned(list)
	if err != nil {
		return State{}, err
	}
	row, err := s.q.AbandonCloudflareRemoteAccess(ctx, sqlc.AbandonCloudflareRemoteAccessParams{
		ExpectedGeneration: generation, Abandoned: abandoned,
	})
	return changedState(row, err)
}

// AddAbandoned appends in SQL, so a pass that loaded the row before a dismissal can neither
// resurrect the old list nor erase an entry written meanwhile.
func (s *Store) AddAbandoned(ctx context.Context, list []AbandonedResource) error {
	abandoned, err := marshalAbandoned(list)
	if err != nil {
		return err
	}
	_, err = s.q.AddCloudflareRemoteAccessAbandoned(ctx, abandoned)
	return err
}

// DismissAbandoned clears the list once the operator has seen it.
func (s *Store) DismissAbandoned(ctx context.Context) error {
	_, err := s.q.DismissCloudflareRemoteAccessAbandoned(ctx)
	return err
}

func marshalAbandoned(list []AbandonedResource) ([]byte, error) {
	if list == nil {
		list = []AbandonedResource{}
	}
	return json.Marshal(list)
}

func changedState(row sqlc.AuraCloudflareRemoteAccess, err error) (State, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, ErrStaleGeneration
	}
	if err != nil {
		return State{}, err
	}
	return stateFromRow(row)
}

func stateFromRow(r sqlc.AuraCloudflareRemoteAccess) (State, error) {
	s := State{
		Desired:    Desired{Enabled: r.Enabled, AccountID: r.AccountID, ZoneName: r.ZoneName, PublicLabel: r.PublicLabel},
		Generation: r.Generation, Phase: Phase(r.Phase), TunnelName: r.TunnelName,
		Resources: Resources{AccountID: r.ResourceAccountID, ZoneID: r.ZoneID, TunnelID: r.TunnelID, PublicDNSID: r.PublicDnsID, WARPDNSID: r.WarpDnsID,
			OTPProviderID: r.OtpIdpID, PublicAppID: r.PublicAppID, PublicPolicyID: r.PublicPolicyID, WARPAppID: r.WarpAppID,
			WARPPolicyID: r.WarpPolicyID, GatewayPostureID: r.WarpPostureID, LegacyWARPHostname: r.LegacyWarpHostname},
		OwnedHostname: r.OwnedHostname, OwnedZoneName: r.OwnedZoneName, AcceptedHostname: r.AcceptedHostname,
		ObservedHealthy: r.ObservedHealthy, Reason: Reason(r.Reason), UpdatedAt: r.UpdatedAt.Time, UpdatedBy: r.UpdatedBy,
	}
	if len(r.Abandoned) > 0 {
		if err := json.Unmarshal(r.Abandoned, &s.Abandoned); err != nil {
			return State{}, fmt.Errorf("remote access: read abandoned list: %w", err)
		}
	}
	if r.LastReconciledAt.Valid {
		s.LastReconciledAt = &r.LastReconciledAt.Time
	}
	return s, nil
}
