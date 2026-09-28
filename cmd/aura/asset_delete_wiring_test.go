package main

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
)

// The nightly retention job drains the deletes an asset delete left unfinished only if the
// daemon hands it the sweep. Without an asset service or an identity store there is nothing to
// sweep, and the slot gets a bare nil the handler skips, never a sweep that panics at 02:30.
// Refused and failed rows live as long as the deployment keeps its metadata traces.
func TestBuildAssetDeleteSweep(t *testing.T) {
	if sweep := buildAssetDeleteSweep(&chatEnv{}); sweep != nil {
		t.Fatalf("an unwired daemon registered %T", sweep)
	}
	pool := newLazyPool(t)
	svc := &assets.Service{}
	cfg := &config.Config{Retention: config.RetentionConfig{MetadataTraceTTL: 72 * time.Hour}}
	sweep, ok := buildAssetDeleteSweep(&chatEnv{cfg: cfg, assets: svc, identity: identity.New(pool)}).(assets.DeleteSweep)
	if !ok || sweep.Assets != svc || sweep.Identities == nil {
		t.Fatalf("sweep = %#v, want the daemon's asset service over every identity", sweep)
	}
	if sweep.FailedLifetime != 72*time.Hour {
		t.Fatalf("FailedLifetime = %v, want the configured metadata-trace lifetime", sweep.FailedLifetime)
	}
}
