package cloudflareapi

import (
	"context"
	"net/http"
	"net/mail"
	"sort"
	"strings"
)

// EmailPolicy refuses empty membership and normalizes explicit email addresses.
func EmailPolicy(name string, emails []string, otpID string) (AccessPolicy, error) {
	p := AccessPolicy{Name: name, Decision: "allow", Precedence: 1, Include: []AccessRule{}, Require: []AccessRule{}, Exclude: []AccessRule{}}
	if name == "" || otpID == "" || len(emails) == 0 {
		return p, failure("explicit emails and OTP provider required")
	}
	seen := map[string]bool{}
	for _, email := range emails {
		email = strings.ToLower(strings.TrimSpace(email))
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || strings.ContainsAny(email, "*\r\n") {
			return p, failure("invalid explicit email")
		}
		seen[email] = true
	}
	ordered := make([]string, 0, len(seen))
	for email := range seen {
		ordered = append(ordered, email)
	}
	sort.Strings(ordered)
	for _, email := range ordered {
		p.Include = append(p.Include, AccessRule{Email: &EmailRule{email}})
	}
	p.Require = append(p.Require, AccessRule{LoginMethod: &IDRule{otpID}})
	return p, nil
}

// GatewayEmailPolicy adds organization enrollment as a mandatory requirement.
func GatewayEmailPolicy(name string, emails []string, otpID, postureID string) (AccessPolicy, error) {
	p, err := EmailPolicy(name, emails, otpID)
	if err != nil {
		return p, err
	}
	if postureID == "" {
		return p, failure("Gateway posture required")
	}
	p.Require = append(p.Require, AccessRule{DevicePosture: &PostureRule{postureID}})
	return p, nil
}

// ListIdentityProviders reads every page before deciding whether OTP exists.
func (c *Client) ListIdentityProviders(ctx context.Context, accountID string) ([]IdentityProvider, error) {
	return listResource[IdentityProvider](ctx, c, nil, "accounts", accountID, "access", "identity_providers")
}

// EnsureOTPProvider reuses account-wide OTP and never modifies another provider.
func (c *Client) EnsureOTPProvider(ctx context.Context, accountID string) (IdentityProvider, error) {
	providers, err := c.ListIdentityProviders(ctx, accountID)
	if err != nil {
		return IdentityProvider{}, err
	}
	for _, p := range providers {
		if p.Type == "onetimepin" && p.ID != "" {
			return p, nil
		}
	}
	body := struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		Config struct{} `json:"config"`
	}{Name: "Aura one-time PIN", Type: "onetimepin"}
	return get[IdentityProvider](ctx, c, http.MethodPost, body, "accounts", accountID, "access", "identity_providers")
}

// ListApplications supports conflict detection without granting name-based ownership.
func (c *Client) ListApplications(ctx context.Context, accountID string) ([]AccessApplication, error) {
	return listResource[AccessApplication](ctx, c, nil, "accounts", accountID, "access", "apps")
}

// GetApplication lets the reconciler verify persisted ownership before mutation.
func (c *Client) GetApplication(ctx context.Context, accountID, id string) (AccessApplication, error) {
	return get[AccessApplication](ctx, c, http.MethodGet, nil, "accounts", accountID, "access", "apps", id)
}

// PutApplication creates when id is empty; callers must verify ownership before updates.
func (c *Client) PutApplication(ctx context.Context, accountID, id string, app AccessApplication) (AccessApplication, error) {
	if app.Name == "" || app.Domain == "" || app.Type != "self_hosted" || len(app.AllowedIDPs) != 1 || app.AllowedIDPs[0] == "" {
		return AccessApplication{}, failure("self-hosted application and OTP provider required")
	}
	app.ID = ""
	method := http.MethodPost
	parts := []string{"accounts", accountID, "access", "apps"}
	if id != "" {
		method = http.MethodPut
		parts = append(parts, id)
	}
	return get[AccessApplication](ctx, c, method, app, parts...)
}

// DeleteApplication requires the caller to have verified persisted ownership.
func (c *Client) DeleteApplication(ctx context.Context, accountID, id string) error {
	return c.remove(ctx, "accounts", accountID, "access", "apps", id)
}

// ListPolicies detects ambiguous creations and unrelated rules before policy writes.
func (c *Client) ListPolicies(ctx context.Context, accountID, appID string) ([]AccessPolicy, error) {
	return listResource[AccessPolicy](ctx, c, nil, "accounts", accountID, "access", "apps", appID, "policies")
}

// GetPolicy reads an application-specific policy, not a reusable account policy.
func (c *Client) GetPolicy(ctx context.Context, accountID, appID, id string) (AccessPolicy, error) {
	return get[AccessPolicy](ctx, c, http.MethodGet, nil, "accounts", accountID, "access", "apps", appID, "policies", id)
}

// PutPolicy refuses policies lacking explicit emails and an OTP requirement.
func (c *Client) PutPolicy(ctx context.Context, accountID, appID, id string, p AccessPolicy) (AccessPolicy, error) {
	if err := validatePolicy(p); err != nil {
		return AccessPolicy{}, err
	}
	p.ID = ""
	method := http.MethodPost
	parts := []string{"accounts", accountID, "access", "apps", appID, "policies"}
	if id != "" {
		method = http.MethodPut
		parts = append(parts, id)
	}
	return get[AccessPolicy](ctx, c, method, p, parts...)
}
func validatePolicy(p AccessPolicy) error {
	if p.Decision != "allow" || len(p.Include) == 0 || len(p.Require) == 0 || p.Name == "" {
		return failure("explicit allow policy required")
	}
	emails := []string{}
	for _, r := range p.Include {
		if r.Email == nil || r.LoginMethod != nil || r.DevicePosture != nil {
			return failure("only explicit email includes permitted")
		}
		emails = append(emails, r.Email.Email)
	}
	otp := ""
	for _, r := range p.Require {
		if r.Email != nil || (r.LoginMethod == nil) == (r.DevicePosture == nil) {
			return failure("invalid policy requirement")
		}
		if r.LoginMethod != nil {
			otp = r.LoginMethod.ID
			if otp == "" {
				return failure("OTP provider required")
			}
		}
		if r.DevicePosture != nil && r.DevicePosture.IntegrationUID == "" {
			return failure("posture identifier required")
		}
	}
	_, err := EmailPolicy(p.Name, emails, otp)
	return err
}

// DeletePolicy requires the caller to have verified persisted ownership.
func (c *Client) DeletePolicy(ctx context.Context, accountID, appID, id string) error {
	return c.remove(ctx, "accounts", accountID, "access", "apps", appID, "policies", id)
}

// ListPosture reads the non-paginated device posture collection.
func (c *Client) ListPosture(ctx context.Context, accountID string) ([]Posture, error) {
	return get[[]Posture](ctx, c, http.MethodGet, nil, "accounts", accountID, "devices", "posture")
}

// GetPosture retains the type and description needed for ownership checks.
func (c *Client) GetPosture(ctx context.Context, accountID, id string) (Posture, error) {
	return get[Posture](ctx, c, http.MethodGet, nil, "accounts", accountID, "devices", "posture", id)
}

// EnsureGatewayPosture requires organization enrollment; Require WARP also admits consumer
// clients: https://developers.cloudflare.com/cloudflare-one/reusable-components/posture-checks/client-checks/require-gateway/
//
// The name is the only owner-bearing field to check against: a description is accepted on
// create but is absent from every posture read, so a rule identified by one can never be
// recognised again (measured 2026-09-22 — a live gateway rule reads back as {id, type, name}).
func (c *Client) EnsureGatewayPosture(ctx context.Context, accountID, id, name string) (Posture, error) {
	if name == "" {
		return Posture{}, failure("posture ownership required")
	}
	if id != "" {
		p, err := c.GetPosture(ctx, accountID, id)
		if err != nil {
			return p, err
		}
		if p.ID != id || p.Name != name || p.Type != "gateway" {
			return Posture{}, failure("posture ownership conflict")
		}
		return p, nil
	}
	return get[Posture](ctx, c, http.MethodPost, Posture{Name: name, Type: "gateway"}, "accounts", accountID, "devices", "posture")
}

// DeletePosture refuses missing ownership and non-Gateway checks.
func (c *Client) DeletePosture(ctx context.Context, accountID, id, name string) error {
	if name == "" {
		return failure("posture ownership required")
	}
	p, err := c.GetPosture(ctx, accountID, id)
	if err != nil {
		return err
	}
	if p.ID != id || p.Name != name || p.Type != "gateway" {
		return failure("posture ownership conflict")
	}
	return c.remove(ctx, "accounts", accountID, "devices", "posture", id)
}
