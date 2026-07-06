package config

import (
	"slices"
	"testing"
)

// TestLoad_SandboxConfig asserts the Phase 37 AURA_SANDBOX_* operator surface (SBX
// foundation): config.Load() parses each knob into the typed cfg.Sandbox.* field, an unset
// var falls back to the documented default, and — critically — a malformed numeric cap/TTL
// is FLAGGED Fatal by the per-profile reparse pass under a strict tier (never a silent
// fallback into an unsafe value). Lives in its own file so config_test.go stays under the
// 600-LOC cap (CLAUDE.md NO GOD CLASS).
func TestLoad_SandboxConfig(t *testing.T) {
	// The six AURA_SANDBOX_* knobs; the default subtest neutralizes any ambient host value so
	// the fallback assertions are deterministic regardless of the shell that runs the suite.
	sandboxEnvKeys := []string{
		"AURA_SANDBOX_IDLE_TTL_SEC", "AURA_SANDBOX_CPU_LIMIT", "AURA_SANDBOX_MEMORY_LIMIT",
		"AURA_SANDBOX_PIDS_LIMIT", "AURA_SANDBOX_EGRESS_ALLOWLIST", "AURA_SANDBOX_IMAGE",
	}

	t.Run("defaults on unset", func(t *testing.T) {
		clearPostgresEnv(t)
		for _, k := range sandboxEnvKeys {
			t.Setenv(k, "")
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		s := cfg.Sandbox
		if s.IdleTTLSec != 1800 {
			t.Errorf("IdleTTLSec default: want 1800, got %d", s.IdleTTLSec)
		}
		if s.CPULimit != 2 {
			t.Errorf("CPULimit default: want 2, got %d", s.CPULimit)
		}
		if s.MemoryLimit != int64(2)<<30 {
			t.Errorf("MemoryLimit default: want %d (2 GiB), got %d", int64(2)<<30, s.MemoryLimit)
		}
		if s.PidsLimit != 512 {
			t.Errorf("PidsLimit default: want 512, got %d", s.PidsLimit)
		}
		if len(s.EgressAllowlist) != 0 {
			t.Errorf("EgressAllowlist default: want empty (floor-only), got %v", s.EgressAllowlist)
		}
		if s.Image != "aura-sandbox:latest" {
			t.Errorf("Image default: want aura-sandbox:latest, got %q", s.Image)
		}
	})

	t.Run("env overrides parse into typed fields", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_SANDBOX_IDLE_TTL_SEC", "600")
		t.Setenv("AURA_SANDBOX_CPU_LIMIT", "4")
		t.Setenv("AURA_SANDBOX_MEMORY_LIMIT", "4294967296") // 4 GiB — exceeds int32, proves int64 parse
		t.Setenv("AURA_SANDBOX_PIDS_LIMIT", "1024")
		t.Setenv("AURA_SANDBOX_EGRESS_ALLOWLIST", "pypi.org, *.githubusercontent.com ,")
		t.Setenv("AURA_SANDBOX_IMAGE", "registry.example.test/aura-sandbox@sha256:abc")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		s := cfg.Sandbox
		if s.IdleTTLSec != 600 {
			t.Errorf("IdleTTLSec override: want 600, got %d", s.IdleTTLSec)
		}
		if s.CPULimit != 4 {
			t.Errorf("CPULimit override: want 4, got %d", s.CPULimit)
		}
		if s.MemoryLimit != 4294967296 {
			t.Errorf("MemoryLimit override: want 4294967296 (4 GiB), got %d", s.MemoryLimit)
		}
		if s.PidsLimit != 1024 {
			t.Errorf("PidsLimit override: want 1024, got %d", s.PidsLimit)
		}
		// envSliceDefault trims each entry and drops empties: the trailing "," is dropped.
		wantAllow := []string{"pypi.org", "*.githubusercontent.com"}
		if !slices.Equal(s.EgressAllowlist, wantAllow) {
			t.Errorf("EgressAllowlist override: want %v, got %v", wantAllow, s.EgressAllowlist)
		}
		if s.Image != "registry.example.test/aura-sandbox@sha256:abc" {
			t.Errorf("Image override: want the digest-pinned registry ref, got %q", s.Image)
		}
	})

	// The numeric caps + TTL are KindInt precisely so a malformed value can never silently
	// degrade into an unsafe cap/TTL (T-37-01-CFG): the load leaf falls back (non-fatal boot),
	// but the per-profile reparse pass FLAGS it Fatal under a strict tier. A KindString
	// registration would get NO reparse check — the exact bug the KindInt choice fixes.
	t.Run("malformed IDLE_TTL_SEC is Fatal under server_production but the leaf still boots", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_SANDBOX_IDLE_TTL_SEC", "abc")
		v, found := findViolation(reparsePass(ProfileServerProduction), "AURA_SANDBOX_IDLE_TTL_SEC")
		if !found {
			t.Fatal("reparsePass(server_production) produced no violation for a malformed AURA_SANDBOX_IDLE_TTL_SEC")
		}
		if v.Sev != Fatal {
			t.Errorf("malformed AURA_SANDBOX_IDLE_TTL_SEC under server_production: sev=%v, want Fatal", v.Sev)
		}
		// The load leaf silently absorbs the bad value to the default — the reparse pass, not
		// the leaf, is the gate. Load must not boot-fail on a malformed non-required knob.
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load must not boot-fail on a malformed AURA_SANDBOX_IDLE_TTL_SEC (leaf falls back): %v", err)
		}
		if cfg.Sandbox.IdleTTLSec != 1800 {
			t.Errorf("malformed IDLE_TTL_SEC should fall back to default 1800 at the leaf, got %d", cfg.Sandbox.IdleTTLSec)
		}
	})

	// The SAME malformed value is only a Warn under a lenient tier (dev boots full-host, D-09).
	t.Run("malformed IDLE_TTL_SEC is only Warn under dev", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_SANDBOX_IDLE_TTL_SEC", "abc")
		v, found := findViolation(reparsePass(ProfileDev), "AURA_SANDBOX_IDLE_TTL_SEC")
		if !found || v.Sev != Warn {
			t.Errorf("malformed AURA_SANDBOX_IDLE_TTL_SEC under dev: found=%v sev=%v, want found + Warn", found, v.Sev)
		}
	})
}
