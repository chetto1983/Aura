package remotetunnel

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// errConnectorDraining is Cloudflare refusing to delete a tunnel that still has connections: the
// connector was just stopped, so the refusal clears by itself ("The tunnel must have no active
// connections", developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/
// subresources/cloudflared/methods/delete/). The reference names no status or code for it.
var errConnectorDraining = errors.New("remote access: tunnel still has connections")

func hasResources(r Resources) bool {
	for _, id := range []string{r.ZoneID, r.TunnelID, r.PublicDNSID, r.WARPDNSID, r.OTPProviderID,
		r.PublicAppID, r.PublicPolicyID, r.WARPAppID, r.WARPPolicyID, r.GatewayPostureID} {
		if id != "" {
			return true
		}
	}
	return false
}

// addressChanged is true when what exists in Cloudflare was made for another account, zone or
// hostname than the one now desired. A row that holds nothing has nothing to move.
func addressChanged(s State) bool {
	return hasResources(s.Resources) && (s.Resources.AccountID != s.Desired.AccountID ||
		s.OwnedZoneName != s.Desired.ZoneName || s.OwnedHostname != publicHostname(s.Desired))
}

// recordOwned writes the address the resources are about to be created for, so that the first
// persisted ID already carries it.
func recordOwned(s *State) {
	s.Resources.AccountID = s.Desired.AccountID
	s.OwnedZoneName = s.Desired.ZoneName
	s.OwnedHostname = publicHostname(s.Desired)
}

// teardown stops the connector, then deletes every owned resource and forgets the owned set.
// The zone survives when account and zone name are unchanged; the account-wide OTP provider
// survives an unchanged account. Neither is ever deleted. Each step persists, so a pass cut
// short resumes with the connector still off.
func (r *Reconciler) teardown(ctx context.Context, s *State) error {
	// An empty credential leaves rebuild nothing to project after a reboot.
	if r.credentials != nil {
		if err := r.credentials.Save(ctx, ""); err != nil {
			return err
		}
	}
	if err := r.projection.Apply(ctx, ProjectionState{Generation: s.Generation}); err != nil {
		return err
	}
	if err := r.retireWARP(ctx, s); err != nil {
		return err
	}
	accountChanged := s.Resources.AccountID != s.Desired.AccountID
	if accountChanged && s.Resources.LegacyWARPHostname != "" {
		return ErrRemotePresent
	}
	if err := r.runDeletions(ctx, s, r.deletions(ctx, s)); err != nil {
		return err
	}
	if accountChanged || s.OwnedZoneName != s.Desired.ZoneName {
		s.Resources.ZoneID, s.OwnedZoneName = "", ""
	}
	if accountChanged {
		s.Resources.OTPProviderID, s.Resources.AccountID = "", ""
	}
	s.TunnelName, s.OwnedHostname = "", ""
	s.ObservedHealthy = false
	return r.persist(ctx, s)
}

// tunnelRefusal turns Cloudflare's refusal to delete a tunnel that still serves into a retryable one.
func tunnelRefusal(err error, last cloudflareapi.Tunnel) error {
	var api *cloudflareapi.APIError
	if errors.As(err, &api) && api.Status >= 400 && api.Status < 500 && !denied(err) && !notFound(err) && api.Status != 429 && tunnelServes(last) {
		return errConnectorDraining
	}
	return err
}
