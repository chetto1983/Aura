package netguard

import (
	"net/netip"
	"testing"

	"pgregory.net/rapid"
)

// TestClassify has one row per class and per IANA block, plus the public neighbours just
// outside a block, so a widened or narrowed prefix fails a row.
func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		ip     string // empty: the zero Addr
		reason string // empty: may be dialed
	}{
		{"invalid zero addr", "", "invalid_target"},

		{"public v4", "8.8.8.8", ""},
		{"public v4 example", "93.184.216.34", ""},
		{"public v6", "2606:4700:4700::1111", ""},
		{"public v6 first block after 2001::/23", "2001:200::1", ""},

		{"loopback v4", "127.0.0.1", "loopback"},
		{"loopback v6", "::1", "loopback"},
		{"link-local metadata v4", "169.254.169.254", "link_local"},
		{"link-local v6", "fe80::1", "link_local"},
		{"link-local v6 with zone", "fe80::1%eth0", "link_local"},
		{"link-local multicast v4", "224.0.0.1", "link_local"},
		{"link-local multicast v6", "ff02::1", "link_local"},
		{"private 10/8", "10.0.0.5", "private"},
		{"private 172.16/12", "172.16.3.4", "private"},
		{"private 192.168/16", "192.168.1.10", "private"},
		{"unique-local v6", "fc00::1", "private"},
		// fd00:ec2::/32 is AWS's IPv6 metadata range inside fc00::/7: Classify says private,
		// and IsMetadata is what keeps it out of every profile.
		{"v6 metadata range", "fd00:ec2::254", "private"},
		{"multicast v4", "239.0.0.1", "multicast"},
		{"multicast v6", "ff0e::1", "multicast"},
		{"unspecified v4", "0.0.0.0", "unspecified"},
		{"unspecified v6", "::", "unspecified"},
		{"cgnat", "100.64.0.1", "cgnat"},
		{"this network", "0.1.2.3", "this_network"},

		{"mapped metadata", "::ffff:169.254.169.254", "link_local"},
		{"mapped loopback", "::ffff:127.0.0.1", "loopback"},
		{"mapped private", "::ffff:10.0.0.1", "private"},
		{"mapped cgnat", "::ffff:100.64.0.1", "cgnat"},
		{"mapped this network", "::ffff:0.1.2.3", "this_network"},
		{"mapped reserved", "::ffff:198.18.0.1", "reserved"},
		{"mapped public", "::ffff:8.8.8.8", ""},

		{"nat64 metadata", "64:ff9b::a9fe:a9fe", "link_local"},
		{"nat64 loopback", "64:ff9b::7f00:1", "loopback"},
		{"nat64 private", "64:ff9b::a00:1", "private"},
		{"nat64 cgnat", "64:ff9b::6440:1", "cgnat"},
		{"nat64 this network", "64:ff9b::1:203", "this_network"},
		{"nat64 public stays dialable for DNS64", "64:ff9b::808:808", ""},
		{"nat64 local-use prefix", "64:ff9b:1::a9fe:a9fe", "reserved"},

		{"6to4 metadata", "2002:a9fe:a9fe::1", "link_local"},
		{"6to4 loopback", "2002:7f00:1::1", "loopback"},
		{"6to4 private", "2002:c0a8:101::1", "private"},
		{"6to4 public", "2002:808:808::1", ""},

		{"ietf protocol assignments", "192.0.0.1", "reserved"},
		{"nat64 discovery", "192.0.0.170", "reserved"},
		{"test-net-1", "192.0.2.1", "reserved"},
		{"6a44 relay", "192.88.99.2", "reserved"},
		{"deprecated 6to4 relay anycast is not in the table", "192.88.99.1", ""},
		{"benchmarking low", "198.18.0.1", "reserved"},
		{"benchmarking high", "198.19.255.255", "reserved"},
		{"below benchmarking", "198.17.255.255", ""},
		{"above benchmarking", "198.20.0.1", ""},
		{"test-net-2", "198.51.100.1", "reserved"},
		{"test-net-3", "203.0.113.1", "reserved"},
		{"class e", "240.0.0.1", "reserved"},
		{"limited broadcast", "255.255.255.255", "reserved"},

		{"teredo", "2001:0:4136:e378:8000:63bf:3fff:fdd2", "reserved"},
		{"v6 benchmarking", "2001:2::1", "reserved"},
		{"v6 documentation", "2001:db8::1", "reserved"},
		{"v6 documentation 3fff", "3fff::1", "reserved"},

		{"discard-only", "100::1", "reserved"},
		{"dummy prefix", "100:0:0:1::1", "reserved"},
		{"srv6 sids", "5f00::1", "reserved"},
		{"deprecated site-local", "fec0::1", "reserved"},
		{"ipv4-compatible loopback", "::7f00:1", "reserved"},
		{"unallocated 4000::/3", "4000::1", "reserved"},
		{"global v6 with zone", "2606:4700:4700::1111%eth0", "reserved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ip netip.Addr
			if tc.ip != "" {
				ip = netip.MustParseAddr(tc.ip)
			}
			reason, blocked := Classify(ip)
			if reason != tc.reason || blocked != (tc.reason != "") {
				t.Fatalf("Classify(%s) = (%q, %v), want (%q, %v)", tc.ip, reason, blocked, tc.reason, tc.reason != "")
			}
		})
	}
}

// TestCarriedIPv4IsJudgedAsItself is the property behind the transition rows: an IPv4
// address reached through ::ffff:0:0/96, 64:ff9b::/96 or 2002::/16 gets exactly the verdict
// of the address itself, whatever it is and whatever the 6to4 site bits hold.
func TestCarriedIPv4IsJudgedAsItself(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		var v4 [4]byte
		for i := range v4 {
			v4[i] = rapid.Byte().Draw(rt, "v4")
		}
		addr := netip.AddrFrom4(v4)
		var site [10]byte
		for i := range site {
			site[i] = rapid.Byte().Draw(rt, "site")
		}
		mapped := netip.AddrFrom16([16]byte{10: 0xff, 11: 0xff, 12: v4[0], 13: v4[1], 14: v4[2], 15: v4[3]})
		nat64 := netip.AddrFrom16([16]byte{1: 0x64, 2: 0xff, 3: 0x9b, 12: v4[0], 13: v4[1], 14: v4[2], 15: v4[3]})
		var b [16]byte
		b[0], b[1] = 0x20, 0x02
		copy(b[2:6], v4[:])
		copy(b[6:], site[:])
		sixToFour := netip.AddrFrom16(b)

		wantReason, wantBlocked := Classify(addr)
		wantMetadata := IsMetadata(addr)
		for _, carried := range []netip.Addr{mapped, nat64, sixToFour} {
			reason, blocked := Classify(carried)
			if reason != wantReason || blocked != wantBlocked {
				rt.Fatalf("Classify(%s) = (%q, %v), but %s is (%q, %v)", carried, reason, blocked, addr, wantReason, wantBlocked)
			}
			if IsMetadata(carried) != wantMetadata {
				rt.Fatalf("IsMetadata(%s) = %v, but %s is %v", carried, !wantMetadata, addr, wantMetadata)
			}
		}
	})
}

func TestIsMetadata(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ip   string
		want bool
	}{
		{"", false},
		{"169.254.169.254", true},
		{"169.254.0.1", true},
		{"224.0.0.251", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"fd00:ec2::254", true},
		{"::ffff:169.254.169.254", true},
		{"64:ff9b::a9fe:a9fe", true},
		{"2002:a9fe:a9fe::1", true},
		{"10.0.0.1", false},
		{"127.0.0.1", false},
		{"fd00::1", false},
		{"fd00:ec3::1", false},
		{"8.8.8.8", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			t.Parallel()
			var ip netip.Addr
			if tc.ip != "" {
				ip = netip.MustParseAddr(tc.ip)
			}
			if got := IsMetadata(ip); got != tc.want {
				t.Fatalf("IsMetadata(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

func TestIsMetadataHostname(t *testing.T) {
	t.Parallel()
	cases := []struct {
		host string
		want bool
	}{
		{"metadata.google.internal", true},
		{"METADATA.AMAZONAWS.COM", true},
		{"metadata.azure.com", true},
		{"kubernetes.default.svc", true},
		{"Host.Docker.Internal", true},
		{"metadata.google.internal.", true},
		{"metadata.google.internal.evil.test", false},
		{"example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			if got := IsMetadataHostname(tc.host); got != tc.want {
				t.Fatalf("IsMetadataHostname(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}
