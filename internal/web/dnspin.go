package web

import (
	"net/netip"
	"sync"
	"time"
)

// pinKey scopes a pin to BOTH the conversation and the host so two conversations
// fetching the same host never share a pinned IP (per-conversation isolation,
// D-25). A bare host key would let one conversation's resolution leak into
// another's dial.
type pinKey struct {
	conv string
	host string
}

type pinEntry struct {
	ip      netip.Addr
	expires time.Time
}

// dnsPin is the per-(conversation,host) TTL pin cache that closes the DNS
// rebinding TOCTOU window: the first resolution is reused for AURA_WEB_DNS_PIN_TTL_SEC
// so a second fetch within the window dials the SAME IP the classifier approved,
// never a freshly-rebound one (D-25, SC#4). A mutex guards the map; clock is
// injectable so the TTL test is deterministic.
type dnsPin struct {
	mu  sync.Mutex
	m   map[pinKey]pinEntry
	ttl time.Duration
	now func() time.Time
}

// newDNSPin builds a pin cache with the config-injected TTL (seconds). The TTL is
// injected — dnspin.go never reads AURA_WEB_DNS_PIN_TTL_SEC itself; config.LoadDB
// owns the env read and passes WebDNSPinTTLSec in.
func newDNSPin(ttlSec int) *dnsPin {
	return &dnsPin{
		m:   make(map[pinKey]pinEntry),
		ttl: time.Duration(ttlSec) * time.Second,
		now: time.Now,
	}
}

// Pinned returns the live pinned IP for (conv,host), or (zero,false) when the key
// is absent OR its entry has expired. An expired entry is a miss so the caller
// re-resolves and re-classifies — the pin reuses a VALIDATED IP, it does not
// extend trust past the TTL.
func (p *dnsPin) Pinned(conv, host string) (netip.Addr, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.m[pinKey{conv, host}]
	if !ok || !p.now().Before(e.expires) {
		return netip.Addr{}, false
	}
	return e.ip, true
}

// Pin stores ip for (conv,host) with a fresh TTL window. Called only after the
// classifier has approved the IP (validateAndPin) so the cache never holds a
// blocked address.
func (p *dnsPin) Pin(conv, host string, ip netip.Addr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[pinKey{conv, host}] = pinEntry{ip: ip, expires: p.now().Add(p.ttl)}
}
