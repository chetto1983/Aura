package cloudflareapi

import (
	"context"
	"net/http"
	"net/url"
)

// CreateTunnel creates a remotely managed connector without private routes.
func (c *Client) CreateTunnel(ctx context.Context, accountID, name string) (Tunnel, error) {
	if name == "" {
		return Tunnel{}, failure("tunnel name required")
	}
	body := struct {
		Name         string `json:"name"`
		ConfigSource string `json:"config_src"`
	}{name, "cloudflare"}
	return get[Tunnel](ctx, c, http.MethodPost, body, "accounts", accountID, "cfd_tunnel")
}

// GetTunnel exposes connector health without returning its credential.
func (c *Client) GetTunnel(ctx context.Context, accountID, id string) (Tunnel, error) {
	return get[Tunnel](ctx, c, http.MethodGet, nil, "accounts", accountID, "cfd_tunnel", id)
}

// ListTunnels excludes deleted tunnels and supports ownership conflict detection.
func (c *Client) ListTunnels(ctx context.Context, accountID string) ([]Tunnel, error) {
	return listResource[Tunnel](ctx, c, url.Values{"is_deleted": {"false"}}, "accounts", accountID, "cfd_tunnel")
}

// DeleteTunnel requires the caller to have verified persisted ownership.
func (c *Client) DeleteTunnel(ctx context.Context, accountID, id string) error {
	return c.remove(ctx, "accounts", accountID, "cfd_tunnel", id)
}

// TunnelToken retrieves the current credential; it does not rotate it.
func (c *Client) TunnelToken(ctx context.Context, accountID, id string) (Secret, error) {
	value, err := get[string](ctx, c, http.MethodGet, nil, "accounts", accountID, "cfd_tunnel", id, "token")
	if err == nil && value == "" {
		err = failure("empty tunnel token")
	}
	return Secret(value), err
}

// PutTunnelConfig refuses ingress without a final HTTP 404 catch-all.
func (c *Client) PutTunnelConfig(ctx context.Context, accountID, id string, config TunnelConfig) error {
	if len(config.Ingress) < 2 || config.Ingress[len(config.Ingress)-1].Hostname != "" || config.Ingress[len(config.Ingress)-1].Service != "http_status:404" {
		return failure("ingress requires final 404 catch-all")
	}
	for _, rule := range config.Ingress[:len(config.Ingress)-1] {
		if rule.Hostname == "" || rule.Service == "" {
			return failure("ingress requires hostname and service")
		}
	}
	body := struct {
		Config TunnelConfig `json:"config"`
	}{config}
	_, err := get[struct {
		Config TunnelConfig `json:"config"`
	}](ctx, c, http.MethodPut, body, "accounts", accountID, "cfd_tunnel", id, "configurations")
	return err
}

// ListDNS checks all record types for a hostname conflict.
func (c *Client) ListDNS(ctx context.Context, zoneID, hostname string) ([]DNSRecord, error) {
	return listResource[DNSRecord](ctx, c, url.Values{"name": {hostname}}, "zones", zoneID, "dns_records")
}

// GetDNS retains the ownership marker required before replacement.
func (c *Client) GetDNS(ctx context.Context, zoneID, id string) (DNSRecord, error) {
	return get[DNSRecord](ctx, c, http.MethodGet, nil, "zones", zoneID, "dns_records", id)
}

// EnsureCNAME requires the persisted ID and a matching comment. A hostname match
// alone never authorizes mutation of an existing record.
func (c *Client) EnsureCNAME(ctx context.Context, zoneID, id, hostname, tunnelID, owner string) (DNSRecord, error) {
	if hostname == "" || tunnelID == "" || owner == "" {
		return DNSRecord{}, failure("CNAME ownership and target required")
	}
	method := http.MethodPost
	parts := []string{"zones", zoneID, "dns_records"}
	if id == "" {
		records, err := c.ListDNS(ctx, zoneID, hostname)
		if err != nil {
			return DNSRecord{}, err
		}
		if len(records) != 0 {
			return DNSRecord{}, failure("DNS ownership conflict")
		}
	} else {
		record, err := c.GetDNS(ctx, zoneID, id)
		if err != nil {
			return DNSRecord{}, err
		}
		if record.ID != id || record.Comment != owner || record.Name != hostname || record.Type != "CNAME" {
			return DNSRecord{}, failure("DNS ownership conflict")
		}
		method = http.MethodPut
		parts = append(parts, id)
	}
	want := DNSRecord{Type: "CNAME", Name: hostname, Content: tunnelID + ".cfargotunnel.com", Proxied: true, TTL: 1, Comment: owner}
	return get[DNSRecord](ctx, c, method, want, parts...)
}

// DeleteDNS rechecks the persisted ID and ownership comment before removal.
func (c *Client) DeleteDNS(ctx context.Context, zoneID, id, owner string) error {
	if owner == "" {
		return failure("DNS ownership required")
	}
	record, err := c.GetDNS(ctx, zoneID, id)
	if err != nil {
		return err
	}
	if record.ID != id || record.Comment != owner || record.Type != "CNAME" {
		return failure("DNS ownership conflict")
	}
	return c.remove(ctx, "zones", zoneID, "dns_records", id)
}
