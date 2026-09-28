package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/identity"
)

// The nightly retention job drains the deletes an asset delete left unfinished only if the
// daemon hands it the sweep. Without an asset service or an identity store there is nothing to
// sweep, and the slot gets a bare nil the handler skips, never a sweep that panics at 02:30.
func TestBuildAssetDeleteSweep(t *testing.T) {
	if sweep := buildAssetDeleteSweep(&chatEnv{}); sweep != nil {
		t.Fatalf("an unwired daemon registered %T", sweep)
	}
	pool := newLazyPool(t)
	svc := &assets.Service{}
	sweep, ok := buildAssetDeleteSweep(&chatEnv{assets: svc, identity: identity.New(pool)}).(assets.DeleteSweep)
	if !ok || sweep.Assets != svc || sweep.Identities == nil {
		t.Fatalf("sweep = %#v, want the daemon's asset service over every identity", sweep)
	}
}
