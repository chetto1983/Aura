package cloudflareapi

import (
	"context"
	"net/http"
	"net/url"
)

// VerifyToken rejects revoked and expired user API tokens.
func (c *Client) VerifyToken(ctx context.Context) (TokenVerification, error) {
	token, err := get[TokenVerification](ctx, c, http.MethodGet, nil, "user", "tokens", "verify")
	if err == nil && token.Status != "active" {
		err = failure("token is not active")
	}
	return token, err
}

// ListAccounts consumes account pagination before onboarding selects an account.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	return listResource[Account](ctx, c, nil, "accounts")
}

// ListZones scopes discovery to the selected account and registered domain.
func (c *Client) ListZones(ctx context.Context, accountID, name string) ([]Zone, error) {
	return listResource[Zone](ctx, c, url.Values{"account.id": {accountID}, "name": {name}}, "zones")
}

// GetZone preserves pending activation rather than recreating the zone.
func (c *Client) GetZone(ctx context.Context, id string) (Zone, error) {
	return get[Zone](ctx, c, http.MethodGet, nil, "zones", id)
}

// CreateZone requests authoritative DNS hosting, not a domain purchase.
func (c *Client) CreateZone(ctx context.Context, accountID, name string) (Zone, error) {
	if accountID == "" || name == "" {
		return Zone{}, failure("account and zone name required")
	}
	body := struct {
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
		Name string `json:"name"`
		Type string `json:"type"`
	}{Name: name, Type: "full"}
	body.Account.ID = accountID
	return get[Zone](ctx, c, http.MethodPost, body, "zones")
}
