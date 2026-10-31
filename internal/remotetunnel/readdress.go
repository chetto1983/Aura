package remotetunnel

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// errConnectorDraining is Cloudflare refusing to delete a tunnel that still has connections: the
// connector was just stopped, so the refusal clears by itself. The delete reference only says
// "The tunnel must have no active connections"
// (developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/
// cloudflared/methods/delete/); the answer itself is HTTP 400 with code 1022, "Cannot delete
// tunnel because it has active connections", and a second attempt usually succeeds
// (github.com/cloudflare/terraform-provider-cloudflare/issues/5255).
var errConnectorDraining = errors.New("remote access: tunnel still has connections")

const codeTunnelHasConnections = 1022

// HasResources is true when any recorded ID says something exists in Cloudflare.
func HasResources(r Resources) bool {
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
	return HasResources(s.Resources) && (s.Resources.AccountID != s.Desired.AccountID ||
		s.OwnedZoneName != s.Desired.ZoneName || s.OwnedHostname != PublicHostname(s.Desired))
}

// moveCanFinish runs before the teardown, which cannot be undone: a zone that cannot be had in the
// desired account would leave the old address dead and the new one never started. A new label
// stays in the zone the row holds, so it has nothing to ask.
func (r *Reconciler) moveCanFinish(ctx context.Context, s *State) error {
	if s.Resources.AccountID == s.Desired.AccountID && s.OwnedZoneName == s.Desired.ZoneName {
		return nil
	}
	_, _, err := r.obtainableZone(ctx, s.Desired.AccountID, s.Desired.ZoneName)
	return err
}

// recordOwned writes the address the resources are about to be created for, so that the first
// persisted ID already carries it.
func recordOwned(s *State) {
	s.Resources.AccountID = s.Desired.AccountID
	s.OwnedZoneName = s.Desired.ZoneName
	s.OwnedHostname = PublicHostname(s.Desired)
}

// teardown proves it can finish before it stops the connector: the token must still see the
// account the resources live in, and every public resource must pass its ownership check. Past
// that point it stops the connector, then deletes every owned resource and forgets the owned set.
// The zone survives when account and zone name are unchanged; the account-wide OTP provider
// survives an unchanged account. Neither is ever deleted. Each step persists, so a pass cut
// short resumes with the connector still off.
func (r *Reconciler) teardown(ctx context.Context, s *State) error {
	if r.credentials == nil || r.projection == nil {
		return ErrConfiguration
	}
	if HasResources(s.Resources) {
		visible, err := r.accountVisible(ctx, s)
		if err != nil {
			return err
		}
		if !visible {
			return ErrAccountNotVisible
		}
		if err = r.verifyDeletions(s, r.deletions(ctx, s), func(deletion, error) (bool, error) { return false, nil }); err != nil {
			return err
		}
	}
	// An empty credential leaves rebuild nothing to project after a reboot.
	if err := r.credentials.Save(ctx, ""); err != nil {
		return err
	}
	if err := r.project(ctx, ProjectionState{Generation: s.Generation}); err != nil {
		return err
	}
	if err := r.retireWARP(ctx, s); err != nil {
		return err
	}
	// retireWARP leaves its hostname only for a token that lost the account; the WARP IDs still
	// need the tunnel name and the account, so nothing is forgotten under them.
	if s.Resources.LegacyWARPHostname != "" {
		return ErrAccountNotVisible
	}
	accountChanged := s.Resources.AccountID != s.Desired.AccountID
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
	return r.persist(ctx, s)
}

// tunnelRefusal turns Cloudflare's refusal to delete a tunnel that still has connections into a retryable one.
func tunnelRefusal(err error) error {
	var api *cloudflareapi.APIError
	if errors.As(err, &api) && api.Status == 400 && api.Code == codeTunnelHasConnections {
		return errConnectorDraining
	}
	return err
}
