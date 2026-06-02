//go:build !web_integration

package web

import (
	"net/netip"
	"testing"
	"time"
)

// TestDNSPin_TTL proves SC#4: a pin is a hit within TTL and a miss after it
// expires, and entries are keyed by BOTH conversation AND host so a different
// conversation never reuses another's pin. The clock is injected so expiry is
// deterministic (no real sleep, no flake).
func TestDNSPin_TTL(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	p := newDNSPin(60)
	p.now = func() time.Time { return now }

	ip := netip.MustParseAddr("1.2.3.4")
	p.Pin("conv-A", "example.test", ip)

	// Hit within TTL.
	if got, ok := p.Pinned("conv-A", "example.test"); !ok || got != ip {
		t.Fatalf("within TTL: got (%v,%v), want (%v,true)", got, ok, ip)
	}

	// Different conversation, same host → miss (no cross-conversation sharing).
	if got, ok := p.Pinned("conv-B", "example.test"); ok {
		t.Fatalf("conv-B must not see conv-A's pin, got %v", got)
	}
	// Different host, same conversation → miss.
	if _, ok := p.Pinned("conv-A", "other.test"); ok {
		t.Fatal("different host must miss")
	}

	// Advance past TTL → miss on expiry.
	now = now.Add(61 * time.Second)
	if got, ok := p.Pinned("conv-A", "example.test"); ok {
		t.Fatalf("after TTL: expected miss, got %v", got)
	}
}

// TestDNSPin_ExactExpiryIsMiss pins the boundary: at exactly expires the entry is
// a miss (Pinned uses now().Before(expires), so equality is not "live"). This
// kills the off-by-one mutation (<= vs <).
func TestDNSPin_ExactExpiryIsMiss(t *testing.T) {
	t.Parallel()
	now := time.Unix(2_000_000, 0)
	p := newDNSPin(10)
	p.now = func() time.Time { return now }
	p.Pin("c", "h", netip.MustParseAddr("9.9.9.9"))

	now = now.Add(10 * time.Second) // exactly at expiry
	if _, ok := p.Pinned("c", "h"); ok {
		t.Fatal("entry at exact expiry must be a miss")
	}
}
