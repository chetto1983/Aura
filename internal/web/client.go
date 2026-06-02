package web

import (
	"net"

	"github.com/chetto1983/aura/internal/config"
)

// Client is the Phase 7 shared engine behind web_search and web_fetch. It owns the
// single SSRF-hardened transport (dial-only-the-pinned-IP), the per-conversation
// DNS pin cache, the response cache, and the web config. The two thin tool adapters
// (Wave 4) hold one *Client and call Search / Fetch — no tool re-implements the
// security boundary (D-01).
type Client struct {
	cfg       *config.Config
	transport *hardenedTransport
	pin       *dnsPin
	cache     *cache
}

// NewClient wires the hardened transport (real net.Dialer with the Control
// recheck), the DNS pin, and the response cache from config. The pin TTL and the
// User-Agent come from config; the transport owns the egress identity so every
// search and fetch request carries the same Aura UA (D-34/D-35).
func NewClient(cfg *config.Config) *Client {
	pin := newDNSPin(cfg.WebDNSPinTTLSec)
	g := newGuard(net.DefaultResolver, pin, nil)
	tr := newHardenedTransport(g, nil, cfg.WebUserAgent)
	return &Client{
		cfg:       cfg,
		transport: tr,
		pin:       pin,
		cache:     newCache(cfg.WebCachePersistent, defaultCacheTTL),
	}
}

// userAgent is the egress identity stamped on every outbound request.
func (c *Client) userAgent() string { return c.transport.ua }
