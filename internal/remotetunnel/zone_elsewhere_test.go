package remotetunnel

import (
	"context"
	"errors"
	"net"
	"testing"
)

// noZoneYet is the harness with no zone in the operator's account, so zone() reaches the create path.
func noZoneYet(t *testing.T, hosts ...string) *harness {
	h := newHarness(t)
	h.ns.hosts = hosts
	h.cloud.zone.Status = "pending"
	return h
}

func TestZoneActiveElsewhereIsRefusedBeforeCreation(t *testing.T) {
	for _, host := range []string{"jo.ns.cloudflare.com.", "yahir.NS.Cloudflare.com", "JO.NS.CLOUDFLARE.COM."} {
		t.Run(host, func(t *testing.T) {
			h := noZoneYet(t, host, "yahir.ns.cloudflare.com.")
			err := h.r.Reconcile(t.Context())
			if !errors.Is(err, ErrDomainElsewhere) {
				t.Fatalf("err=%v, want ErrDomainElsewhere", err)
			}
			st := h.store.state
			if st.Phase != PhaseError || st.Reason != ReasonDomainElsewhere {
				t.Fatalf("phase=%q reason=%q, want error/domain_elsewhere", st.Phase, st.Reason)
			}
			if h.cloud.zoneCreates != 0 {
				t.Fatalf("CreateZone called %d times for a domain held elsewhere", h.cloud.zoneCreates)
			}
			if len(h.ns.asked) != 1 || h.ns.asked[0] != "example.com" {
				t.Fatalf("looked up %v, want the zone name once", h.ns.asked)
			}
		})
	}
}

func TestZoneOnAnotherProviderIsCreated(t *testing.T) {
	h := noZoneYet(t, "dns.register.it.", "dns2.register.it.")
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.cloud.zoneCreates != 1 || h.store.state.Phase != PhaseWaitingNameservers {
		t.Fatalf("creates=%d phase=%q, want one zone and waiting_nameservers", h.cloud.zoneCreates, h.store.state.Phase)
	}
}

func TestZoneLookupFailureCreatesTheZone(t *testing.T) {
	for name, cause := range map[string]error{
		"nxdomain": &net.DNSError{IsNotFound: true},
		"servfail": &net.DNSError{Err: "server misbehaving", IsTemporary: true},
		"timeout":  &net.DNSError{Err: "i/o timeout", IsTimeout: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := noZoneYet(t)
			h.ns.err = cause
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if h.cloud.zoneCreates != 1 || h.store.state.Phase != PhaseWaitingNameservers {
				t.Fatalf("creates=%d phase=%q, want one zone and waiting_nameservers", h.cloud.zoneCreates, h.store.state.Phase)
			}
		})
	}
}

func TestZoneAlreadyInThisAccountSkipsTheLookup(t *testing.T) {
	h := newHarness(t)
	h.ns.hosts = []string{"jo.ns.cloudflare.com."}
	h.cloud.zone.ID = "zone"
	h.cloud.allowUnpersisted = true
	h.cloud.zone.Status = "pending"
	h.store.state.Resources.ZoneID = ""
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.ns.asked) != 0 || h.cloud.zoneCreates != 0 {
		t.Fatalf("looked up %v, created %d; a zone already here needs neither", h.ns.asked, h.cloud.zoneCreates)
	}
}

func TestCancelledLookupDecidesNothing(t *testing.T) {
	h := noZoneYet(t)
	ctx, cancel := context.WithCancel(t.Context())
	h.ns.err = context.Canceled
	cancel()
	if err := h.r.Reconcile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if h.cloud.zoneCreates != 0 {
		t.Fatal("a cancelled pass created a zone")
	}
}

func TestZoneNeedingALookupWithoutAResolverIsAConfigurationError(t *testing.T) {
	h := noZoneYet(t)
	h.r = New(h.store, h.cloud, h.members, h.projection, nil, WithCredentials(h.credentials))
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("err=%v, want ErrConfiguration", err)
	}
	if h.cloud.zoneCreates != 0 {
		t.Fatal("a zone was created without checking where the domain lives")
	}
}
