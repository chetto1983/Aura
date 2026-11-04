package browserlogin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The sign-in runtime passes Options{} and so relies on this default guard; every
// other test injects fixtureOwner, which would leave the production check unexercised.
func TestDefaultOwnerCheck(t *testing.T) {
	canceled, cancel := context.WithCancel(ownerContext("alice"))
	cancel()
	cases := []struct {
		name     string
		ctx      context.Context
		owner    string
		want     error
		rejected bool
	}{
		{"scoped owner", ownerContext("alice"), "alice", nil, false},
		{"unscoped context", context.Background(), "alice", nil, false},
		{"empty owner", context.Background(), "", nil, true},
		{"padded owner", context.Background(), " alice", nil, true},
		{"other identity", ownerContext("bob"), "alice", nil, true},
		{"canceled request", canceled, "alice", context.Canceled, false},
	}
	check := defaultOptions(Options{}).CheckOwner
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := check(tc.ctx, tc.owner)
			if tc.rejected {
				if err == nil || err.Error() != "browser login: an Aura identity is required" {
					t.Fatalf("checkOwner(%q) = %v, want identity rejection", tc.owner, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("checkOwner(%q) = %v, want %v", tc.owner, err, tc.want)
			}
		})
	}
}

func TestDefaultOptionsFillOnlyUnsetDurations(t *testing.T) {
	defaults := defaultOptions(Options{Startup: -time.Second})
	if defaults.Startup != 45*time.Second || defaults.TTL != 10*time.Minute {
		t.Fatalf("defaults = startup %v, ttl %v", defaults.Startup, defaults.TTL)
	}
	if defaults.Prefix != "signin-" || defaults.Route != "/signing-in/" {
		t.Fatalf("defaults = prefix %q, route %q", defaults.Prefix, defaults.Route)
	}
	tight := defaultOptions(Options{Startup: time.Nanosecond, TTL: time.Nanosecond})
	if tight.Startup != time.Nanosecond || tight.TTL != time.Nanosecond {
		t.Fatalf("explicit durations replaced: startup %v, ttl %v", tight.Startup, tight.TTL)
	}
}
