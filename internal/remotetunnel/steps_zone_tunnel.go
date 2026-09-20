package remotetunnel

import (
	"context"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/google/uuid"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateDesired(d Desired) error {
	if d.AccountID == "" || !dnsLabel.MatchString(d.PublicLabel) || !dnsLabel.MatchString(d.WARPLabel) || d.PublicLabel == d.WARPLabel || len(d.ZoneName) > 253 {
		return ErrConfiguration
	}
	parts := strings.Split(d.ZoneName, ".")
	if len(parts) < 2 {
		return ErrConfiguration
	}
	for _, part := range parts {
		if !dnsLabel.MatchString(part) {
			return ErrConfiguration
		}
	}
	if len(d.PublicLabel)+1+len(d.ZoneName) > 253 || len(d.WARPLabel)+1+len(d.ZoneName) > 253 {
		return ErrConfiguration
	}
	return nil
}

func (r *Reconciler) account(ctx context.Context, s *State) error {
	if s.Resources.AccountID != "" && s.Resources.AccountID != s.Desired.AccountID {
		return ErrOwnershipConflict
	}
	accounts, err := r.cloud.ListAccounts(ctx)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if a.ID == s.Desired.AccountID {
			s.Resources.AccountID = a.ID
			return nil
		}
	}
	return ErrConfiguration
}

func (r *Reconciler) zone(ctx context.Context, s *State) (bool, error) {
	var z cloudflareapi.Zone
	var err error
	if s.Resources.ZoneID != "" {
		z, err = r.cloud.GetZone(ctx, s.Resources.ZoneID)
	} else {
		var zones []cloudflareapi.Zone
		zones, err = r.cloud.ListZones(ctx, s.Resources.AccountID, s.Desired.ZoneName)
		if err != nil {
			return false, err
		}
		if len(zones) > 1 {
			return false, ErrOwnershipConflict
		}
		if len(zones) == 1 {
			z = zones[0]
		} else {
			z, err = r.cloud.CreateZone(ctx, s.Resources.AccountID, s.Desired.ZoneName)
		}
		if err == nil && z.ID != "" {
			s.Resources.ZoneID = z.ID
			err = r.persist(ctx, s)
		}
	}
	if err != nil {
		return false, err
	}
	if z.ID == "" || z.ID != s.Resources.ZoneID || z.Account.ID != s.Resources.AccountID || z.Name != s.Desired.ZoneName {
		return false, ErrOwnershipConflict
	}
	if z.Status != "active" {
		s.Phase = PhaseWaitingNameservers
		s.ObservedHealthy = false
		s.LastError = ""
		return false, r.persist(ctx, s)
	}
	return true, nil
}

func validOwner(name string) bool {
	if !strings.HasPrefix(name, "aura-") {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(name, "aura-"))
	return err == nil
}
func ownedTunnel(s State, t cloudflareapi.Tunnel) bool {
	// config_src supersedes deprecated remote_config in Cloudflare's Get Tunnel contract:
	// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/methods/get/
	remote := t.ConfigSource == "cloudflare" || t.ConfigSource == "" && t.RemoteConfig
	return validOwner(s.TunnelName) && t.ID == s.Resources.TunnelID && t.Name == s.TunnelName && remote
}

func (r *Reconciler) tunnel(ctx context.Context, s *State) error {
	if s.Resources.TunnelID != "" {
		t, err := r.cloud.GetTunnel(ctx, s.Resources.AccountID, s.Resources.TunnelID)
		if err != nil {
			return err
		}
		if !ownedTunnel(*s, t) || t.DeletedAt != nil {
			return ErrOwnershipConflict
		}
		return nil
	}
	if s.TunnelName == "" {
		s.TunnelName = "aura-" + uuid.NewString()
		if err := r.persist(ctx, s); err != nil {
			return err
		}
	}
	if !validOwner(s.TunnelName) {
		return ErrOwnershipConflict
	}
	tunnels, err := r.cloud.ListTunnels(ctx, s.Resources.AccountID)
	if err != nil {
		return err
	}
	for _, t := range tunnels {
		if t.Name == s.TunnelName {
			return ErrOwnershipConflict
		}
	}
	t, err := r.cloud.CreateTunnel(ctx, s.Resources.AccountID, s.TunnelName)
	if err != nil {
		return err
	}
	if t.ID == "" {
		return ErrConfiguration
	}
	s.Resources.TunnelID = t.ID
	if err = r.persist(ctx, s); err != nil {
		return err
	}
	if !ownedTunnel(*s, t) {
		return ErrOwnershipConflict
	}
	return nil
}

func (r *Reconciler) otp(ctx context.Context, s *State) error {
	if s.Resources.OTPProviderID != "" {
		providers, err := r.cloud.ListIdentityProviders(ctx, s.Resources.AccountID)
		if err != nil {
			return err
		}
		for _, p := range providers {
			if p.ID == s.Resources.OTPProviderID && p.Type == "onetimepin" {
				return nil
			}
		}
		return ErrOwnershipConflict
	}
	p, err := r.cloud.EnsureOTPProvider(ctx, s.Resources.AccountID)
	if err != nil {
		return err
	}
	if p.ID == "" {
		return ErrConfiguration
	}
	s.Resources.OTPProviderID = p.ID
	if err = r.persist(ctx, s); err != nil {
		return err
	}
	if p.Type != "onetimepin" {
		return ErrOwnershipConflict
	}
	return nil
}
