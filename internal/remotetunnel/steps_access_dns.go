package remotetunnel

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

type route struct {
	role, hostname   string
	app, policy, dns *string
}

func routes(s *State) []route {
	return []route{
		{"public", s.Desired.PublicLabel + "." + s.Desired.ZoneName, &s.Resources.PublicAppID, &s.Resources.PublicPolicyID, &s.Resources.PublicDNSID},
		{"warp", s.Desired.WARPLabel + "." + s.Desired.ZoneName, &s.Resources.WARPAppID, &s.Resources.WARPPolicyID, &s.Resources.WARPDNSID},
	}
}
func resourceName(s *State, role string) string { return s.TunnelName + "-" + role }

func (r *Reconciler) access(ctx context.Context, s *State, emails []string) error {
	for _, route := range routes(s) {
		if route.role == "warp" {
			if err := r.posture(ctx, s); err != nil {
				return err
			}
		}
		if err := r.application(ctx, s, route); err != nil {
			return err
		}
		if err := r.policy(ctx, s, route, emails); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) application(ctx context.Context, s *State, route route) error {
	name := resourceName(s, route.role)
	if *route.app != "" {
		a, err := r.cloud.GetApplication(ctx, s.Resources.AccountID, *route.app)
		if err != nil {
			return err
		}
		if a.ID != *route.app || a.Name != name || a.Type != "self_hosted" {
			return ErrOwnershipConflict
		}
	}
	// Matching domains on another application can alter Access evaluation even if its name differs.
	apps, err := r.cloud.ListApplications(ctx, s.Resources.AccountID)
	if err != nil {
		return err
	}
	for _, a := range apps {
		host, _, _ := strings.Cut(strings.ToLower(a.Domain), "/")
		if a.ID != *route.app && (a.Name == name || host == route.hostname || host == "*."+s.Desired.ZoneName) {
			return ErrOwnershipConflict
		}
	}
	a, err := r.cloud.PutApplication(ctx, s.Resources.AccountID, *route.app, cloudflareapi.AccessApplication{
		Name: name, Domain: route.hostname, Type: "self_hosted", SessionDuration: "24h", AllowedIDPs: []string{s.Resources.OTPProviderID}, AutoRedirectToIdentity: true,
	})
	if err != nil {
		return err
	}
	if a.ID == "" || *route.app != "" && a.ID != *route.app {
		return ErrOwnershipConflict
	}
	*route.app = a.ID
	if err = r.persist(ctx, s); err != nil {
		return err
	}
	if a.Name != name || a.Type != "self_hosted" || a.Domain != route.hostname {
		return ErrOwnershipConflict
	}
	return nil
}

func (r *Reconciler) policy(ctx context.Context, s *State, route route, emails []string) error {
	name := resourceName(s, route.role+"-members")
	if *route.policy != "" {
		p, err := r.cloud.GetPolicy(ctx, s.Resources.AccountID, *route.app, *route.policy)
		if err != nil {
			return err
		}
		if p.ID != *route.policy || p.Name != name {
			return ErrOwnershipConflict
		}
	}
	policies, err := r.cloud.ListPolicies(ctx, s.Resources.AccountID, *route.app)
	if err != nil {
		return err
	}
	// An extra policy could bypass the restrictive allowlist, regardless of its name.
	for _, p := range policies {
		if p.ID != *route.policy {
			return ErrOwnershipConflict
		}
	}
	var p cloudflareapi.AccessPolicy
	if route.role == "warp" {
		p, err = cloudflareapi.GatewayEmailPolicy(name, emails, s.Resources.OTPProviderID, s.Resources.GatewayPostureID)
	} else {
		p, err = cloudflareapi.EmailPolicy(name, emails, s.Resources.OTPProviderID)
	}
	if err != nil {
		return err
	}
	p, err = r.cloud.PutPolicy(ctx, s.Resources.AccountID, *route.app, *route.policy, p)
	if err != nil {
		return err
	}
	if p.ID == "" || *route.policy != "" && p.ID != *route.policy {
		return ErrOwnershipConflict
	}
	*route.policy = p.ID
	if err = r.persist(ctx, s); err != nil {
		return err
	}
	if p.Name != name {
		return ErrOwnershipConflict
	}
	return nil
}

func (r *Reconciler) posture(ctx context.Context, s *State) error {
	name := resourceName(s, "gateway")
	if s.Resources.GatewayPostureID != "" {
		p, err := r.cloud.GetPosture(ctx, s.Resources.AccountID, s.Resources.GatewayPostureID)
		if err != nil {
			return err
		}
		if !ownedPosture(s, p) {
			return ErrOwnershipConflict
		}
		return nil
	}
	checks, err := r.cloud.ListPosture(ctx, s.Resources.AccountID)
	if err != nil {
		return err
	}
	for _, p := range checks {
		if p.Name == name {
			return ErrOwnershipConflict
		}
	}
	p, err := r.cloud.EnsureGatewayPosture(ctx, s.Resources.AccountID, "", name)
	if err != nil {
		return err
	}
	if p.ID == "" {
		return ErrConfiguration
	}
	s.Resources.GatewayPostureID = p.ID
	if err = r.persist(ctx, s); err != nil {
		return err
	}
	if !ownedPosture(s, p) {
		return ErrOwnershipConflict
	}
	return nil
}

func ownedPosture(s *State, p cloudflareapi.Posture) bool {
	return validOwner(s.TunnelName) && p.ID == s.Resources.GatewayPostureID && p.Name == resourceName(s, "gateway") && p.Type == "gateway"
}

func (r *Reconciler) publish(ctx context.Context, s *State) error {
	if err := r.current(ctx, s); err != nil {
		return err
	}
	// Ownership and all Access dependencies are rechecked before changing reachable ingress.
	if err := r.verifyOwned(ctx, s); err != nil {
		return err
	}
	ingress := []cloudflareapi.Ingress{}
	for _, route := range routes(s) {
		ingress = append(ingress, cloudflareapi.Ingress{Hostname: route.hostname, Service: "http://caddy:8080"})
	}
	ingress = append(ingress, cloudflareapi.Ingress{Service: "http_status:404"})
	if err := r.cloud.PutTunnelConfig(ctx, s.Resources.AccountID, s.Resources.TunnelID, cloudflareapi.TunnelConfig{Ingress: ingress}); err != nil {
		return err
	}
	for _, route := range routes(s) {
		if *route.dns != "" {
			d, err := r.cloud.GetDNS(ctx, s.Resources.ZoneID, *route.dns)
			if err != nil {
				return err
			}
			if !ownedDNS(s, route, d) {
				return ErrOwnershipConflict
			}
		} else {
			records, err := r.cloud.ListDNS(ctx, s.Resources.ZoneID, route.hostname)
			if err != nil {
				return err
			}
			if len(records) > 0 {
				return ErrOwnershipConflict
			}
		}
		d, err := r.cloud.EnsureCNAME(ctx, s.Resources.ZoneID, *route.dns, route.hostname, s.Resources.TunnelID, s.TunnelName)
		if err != nil {
			return err
		}
		if d.ID == "" || *route.dns != "" && d.ID != *route.dns {
			return ErrOwnershipConflict
		}
		*route.dns = d.ID
		if err = r.persist(ctx, s); err != nil {
			return err
		}
		if !ownedDNS(s, route, d) {
			return ErrOwnershipConflict
		}
	}
	return nil
}

func ownedDNS(s *State, route route, d cloudflareapi.DNSRecord) bool {
	return validOwner(s.TunnelName) && d.ID == *route.dns && d.Name == route.hostname && d.Comment == s.TunnelName && d.Type == "CNAME"
}
