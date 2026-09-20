package remotetunnel

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/identity"
)

// IdentitySource reuses Aura's canonical identity and capability store.
type IdentitySource interface {
	ListIdentities(context.Context) ([]identity.Identity, error)
	ListCapabilities(context.Context, string) ([]string, error)
}

// IdentityMembers excludes deactivated, system, service, and channel identities.
type IdentityMembers struct{ Source IdentitySource }

// ActiveEmails derives email addresses from human identity names.
func (m IdentityMembers) ActiveEmails(ctx context.Context) ([]string, error) {
	return m.emails(ctx, false)
}

// ActiveAdminEmails requires the explicit identity.create grant.
func (m IdentityMembers) ActiveAdminEmails(ctx context.Context) ([]string, error) {
	return m.emails(ctx, true)
}
func (m IdentityMembers) emails(ctx context.Context, admins bool) ([]string, error) {
	identities, err := m.Source.ListIdentities(ctx)
	if err != nil {
		return nil, err
	}
	emails := []string{}
	for _, person := range identities {
		if person.Deactivated || person.Kind != "user" {
			continue
		}
		if admins {
			caps, e := m.Source.ListCapabilities(ctx, person.ID)
			if e != nil {
				return nil, e
			}
			if !slices.Contains(caps, identity.CapIdentityCreate) {
				continue
			}
		}
		emails = append(emails, person.Name)
	}
	return emails, nil
}

func normalizedEmails(emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, ErrMemberLockout
	}
	p, err := cloudflareapi.EmailPolicy("membership-validation", emails, "otp-validation")
	if err != nil {
		return nil, ErrMemberLockout
	}
	out := make([]string, 0, len(p.Include))
	for _, rule := range p.Include {
		out = append(out, rule.Email.Email)
	}
	return out, nil
}
func sameEmails(a, b []string) bool { return slices.Equal(a, b) }

func (r *Reconciler) activeMembers(ctx context.Context) ([]string, []string, error) {
	if r.members == nil {
		return nil, nil, ErrMemberLockout
	}
	raw, err := r.members.ActiveEmails(ctx)
	if err != nil {
		return nil, nil, err
	}
	emails, err := normalizedEmails(raw)
	if err != nil {
		return nil, nil, err
	}
	source, ok := r.members.(Administrators)
	if !ok {
		return nil, nil, ErrMemberLockout
	}
	raw, err = source.ActiveAdminEmails(ctx)
	if err != nil {
		return nil, nil, err
	}
	admins, err := normalizedEmails(raw)
	if err != nil {
		return nil, nil, err
	}
	for _, admin := range admins {
		if !slices.Contains(emails, admin) {
			return nil, nil, ErrMemberLockout
		}
	}
	return emails, admins, nil
}

func (r *Reconciler) checkAdministrators(ctx context.Context, s *State, admins []string) error {
	confirmed := slices.Clone(admins)
	for _, route := range routes(s) {
		if *route.policy == "" {
			continue
		}
		p, err := r.cloud.GetPolicy(ctx, s.Resources.AccountID, *route.app, *route.policy)
		if err != nil {
			return err
		}
		if p.ID != *route.policy || p.Name != resourceName(s, route.role+"-members") {
			return ErrOwnershipConflict
		}
		confirmed = slices.DeleteFunc(confirmed, func(admin string) bool {
			for _, rule := range p.Include {
				if rule.Email != nil && strings.EqualFold(strings.TrimSpace(rule.Email.Email), admin) {
					return false
				}
			}
			return true
		})
	}
	// A replacement administrator must already be granted by both remote policies.
	if len(confirmed) == 0 {
		return ErrMemberLockout
	}
	return nil
}

// SyncMembers changes only the two email policies after checking administrator continuity.
func (r *Reconciler) SyncMembers(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	if s.Phase == PhaseDeleting {
		return ErrConfiguration
	}
	if s.Phase == PhaseError {
		return ErrTerminal
	}
	if s.Generation == r.retryGeneration && r.now().Before(r.retryAt) {
		return ErrBackoff
	}
	emails, admins, err := r.activeMembers(ctx)
	if err == nil {
		err = r.checkAdministrators(ctx, &s, admins)
	}
	if err == nil {
		err = r.verifyOwned(ctx, &s)
	}
	if err == nil {
		for _, route := range routes(&s) {
			if *route.app == "" || *route.policy == "" {
				err = ErrConfiguration
				break
			}
			if err = r.policy(ctx, &s, route, emails); err != nil {
				break
			}
		}
	}
	if err != nil {
		return r.failed(ctx, &s, err)
	}
	s.LastError = ""
	if s.Phase == PhaseDegraded {
		s.Phase = PhaseDisabled
		if s.Desired.Enabled {
			s.Phase = PhaseConnecting
		}
	}
	r.attempts = 0
	r.retryAt = time.Time{}
	return r.persist(ctx, &s)
}
