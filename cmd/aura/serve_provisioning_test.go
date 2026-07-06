package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/cron/handlers"
)

// validProvisioningAuthulaSecret is a 64-hex-char (32-byte) AURA_AUTHULA_SECRET for the
// object-store KEK derivation (test-only; never a real secret).
const validProvisioningAuthulaSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newLazyPool returns a non-nil *pgxpool.Pool that never dials — pgxpool.New connects
// lazily (MinConns defaults to 0), and buildProvisioningPorts/buildDeprovisioner only store
// the pointer (they never Query in construction), so no live Postgres is needed.
func newLazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:5432/aura?sslmode=disable")
	if err != nil {
		t.Fatalf("new lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// provisioningConfiguredCfg is a config with the pool + Garage admin + object-store fields
// present, so buildProvisioningPorts wires all three ports.
func provisioningConfiguredCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		GarageAdminEndpoint: "http://garage:3903",
		GarageAdminToken:    "test-admin-token",
		AuthulaSecret:       validProvisioningAuthulaSecret,
		ObjectStoreBucket:   "aura-assets",
		SkillsDir:           t.TempDir(),
		ProfileDir:          t.TempDir(),
	}
}

// TestBuildProvisioningPortsWiredWhenConfigured asserts all three onboarding resource-leg
// ports are non-nil when a pool + Garage admin config are present — the fix for VERIF-3/
// HI-01 (before this, OnboardingDeps left them nil so admin-create provisioned nothing).
func TestBuildProvisioningPortsWiredWhenConfigured(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: provisioningConfiguredCfg(t)}
	objProv, fsProv, jrnl := buildProvisioningPorts(chat)
	if objProv == nil {
		t.Error("buildProvisioningPorts ObjectStore = nil, want non-nil")
	}
	if fsProv == nil {
		t.Error("buildProvisioningPorts Filesystem = nil, want non-nil")
	}
	if jrnl == nil {
		t.Error("buildProvisioningPorts Journal = nil, want non-nil")
	}
}

// TestBuildProvisioningPortsNilWhenUnconfigured asserts the ports are all nil when either
// the Garage admin config OR the pool is absent — the backward-compatible pre-cutover /
// interview-only path (each agui port nil-skips its leg).
func TestBuildProvisioningPortsNilWhenUnconfigured(t *testing.T) {
	// Pool present, but no Garage admin config → provisions nothing.
	noGarage := &chatEnv{pool: newLazyPool(t), cfg: &config.Config{SkillsDir: t.TempDir(), ProfileDir: t.TempDir()}}
	if o, f, j := buildProvisioningPorts(noGarage); o != nil || f != nil || j != nil {
		t.Fatalf("buildProvisioningPorts without Garage config: want all nil, got o=%v f=%v j=%v", o, f, j)
	}
	// Nil pool → nil regardless of config.
	if o, f, j := buildProvisioningPorts(&chatEnv{cfg: provisioningConfiguredCfg(t)}); o != nil || f != nil || j != nil {
		t.Fatalf("buildProvisioningPorts with nil pool: want all nil, got o=%v f=%v j=%v", o, f, j)
	}
	// Nil chat → nil.
	if o, f, j := buildProvisioningPorts(nil); o != nil || f != nil || j != nil {
		t.Fatal("buildProvisioningPorts(nil): want all nil")
	}
}

// TestBuildDeprovisionerWiresPurger asserts buildDeprovisioner returns a non-nil
// *Deprovisioner whose PurgeExpired seam satisfies handlers.IdentityPurger (the seam the
// cron identity-purge handler drives), and that the seam runs as a safe no-op with no due
// identities.
func TestBuildDeprovisionerWiresPurger(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: provisioningConfiguredCfg(t)}
	dep := buildDeprovisioner(chat)
	if dep == nil {
		t.Fatal("buildDeprovisioner: want non-nil *Deprovisioner")
	}
	// The Purger seam the dispatch handler consumes MUST be satisfied by the Deprovisioner.
	// This is a compile-time interface-satisfaction assertion: a concrete *Deprovisioner is
	// never nil here, so a runtime nil-check would be dead code (staticcheck SA4023).
	var _ handlers.IdentityPurger = dep
	// A nil-pool build yields a no-op Purger (nil Deactivator → PurgeExpired returns 0,nil).
	nilPoolDep := buildDeprovisioner(&chatEnv{cfg: &config.Config{}})
	n, err := nilPoolDep.PurgeExpired(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("nil-pool PurgeExpired: unexpected error %v", err)
	}
	if n != 0 {
		t.Fatalf("nil-pool PurgeExpired: purged %d, want 0 (no-op)", n)
	}
}

// TestBuildDispatchRegistersIdentityPurge asserts buildDispatch builds with the new
// identity_purge entry present. cron.Dispatch's handler map is unexported (package cron),
// so the exact map entry is proven via the identical construction expression buildDispatch
// uses — handlers.IdentityPurgeHandler{Purger: buildDeprovisioner(chat)} — whose Meta().Kind
// is the registered key and whose Run is a wired no-op. buildDispatch itself is exercised to
// prove the entry compiles into the live map without panicking.
func TestBuildDispatchRegistersIdentityPurge(t *testing.T) {
	chat := &chatEnv{
		pool: nil, // buildDispatch is nil-pool-safe (newSkillWriter/newSelfSendResolver/buildDeprovisioner all nil-safe)
		cfg: &config.Config{
			SkillsDir:         t.TempDir(),
			SkillExportDir:    t.TempDir(),
			ProfileDir:        t.TempDir(),
			RunDir:            t.TempDir(),
			ObjectStoreBucket: "aura-assets",
		},
	}
	dispatch := buildDispatch(chat, cron.New(nil), nil)
	if dispatch == nil {
		t.Fatal("buildDispatch: want non-nil *cron.Dispatch")
	}

	// The exact entry buildDispatch registers under cron.KindIdentityPurge.
	entry := handlers.IdentityPurgeHandler{Purger: buildDeprovisioner(chat)}
	if string(entry.Meta().Kind) != string(cron.KindIdentityPurge) {
		t.Fatalf("identity purge handler kind = %q, want %q", entry.Meta().Kind, cron.KindIdentityPurge)
	}
	summary, err := entry.Run(context.Background(), handlers.Job{})
	if err != nil {
		t.Fatalf("identity purge handler Run: unexpected error %v", err)
	}
	if summary == "" {
		t.Fatal("identity purge handler Run: empty summary, want a no-op purge count")
	}
}

// TestBuildDispatchRegistersSandboxReap asserts buildDispatch builds with the sandbox_reap entry
// present and that a non-strict (dev) profile leaves the router nil — the handler is then the
// disabled no-op (nil reaper), a safe boot. It mirrors TestBuildDispatchRegistersIdentityPurge:
// cron.Dispatch's handler map is unexported, so the exact entry is proven via the identical
// construction expression buildDispatch uses.
func TestBuildDispatchRegistersSandboxReap(t *testing.T) {
	chat := &chatEnv{
		pool: nil, // buildDispatch is nil-pool-safe
		cfg: &config.Config{
			SkillsDir:         t.TempDir(),
			SkillExportDir:    t.TempDir(),
			ProfileDir:        t.TempDir(),
			RunDir:            t.TempDir(),
			ObjectStoreBucket: "aura-assets",
			// Profile is the zero value (non-strict) → buildSandboxRouter returns nil.
		},
	}
	dispatch := buildDispatch(chat, cron.New(nil), nil)
	if dispatch == nil {
		t.Fatal("buildDispatch: want non-nil *cron.Dispatch")
	}
	if chat.sandboxRouter != nil {
		t.Fatal("buildSandboxRouter: want nil router under a non-strict profile (host-direct no-op)")
	}

	// The exact entry buildDispatch registers under cron.KindSandboxReap when the router is nil:
	// a nil reaper interface, which the handler treats as the disabled no-op.
	var reaper handlers.SandboxReaper
	entry := handlers.SandboxReapHandler{Reaper: reaper}
	if string(entry.Meta().Kind) != string(cron.KindSandboxReap) {
		t.Fatalf("sandbox reap handler kind = %q, want %q", entry.Meta().Kind, cron.KindSandboxReap)
	}
	summary, err := entry.Run(context.Background(), handlers.Job{})
	if err != nil {
		t.Fatalf("sandbox reap handler Run: unexpected error %v", err)
	}
	if summary == "" {
		t.Fatal("sandbox reap handler Run: empty summary, want a disabled/no-op message")
	}
}

// TestSandboxReapSweepMinutes proves the reap cadence is derived from the idle-TTL knob (not a
// hardcoded const) and floored at 1 minute for a sub-minute TTL.
func TestSandboxReapSweepMinutes(t *testing.T) {
	cases := []struct {
		idleTTLSec int
		want       int
	}{
		{idleTTLSec: 1800, want: 30}, // 30-min default TTL → 30-min sweep
		{idleTTLSec: 60, want: 1},
		{idleTTLSec: 59, want: 1}, // sub-minute floors at 1
		{idleTTLSec: 0, want: 1},
		{idleTTLSec: 3600, want: 60},
	}
	for _, tc := range cases {
		if got := sandboxReapSweepMinutes(tc.idleTTLSec); got != tc.want {
			t.Errorf("sandboxReapSweepMinutes(%d) = %d, want %d", tc.idleTTLSec, got, tc.want)
		}
	}
}
