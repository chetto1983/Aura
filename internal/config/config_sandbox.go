// config_sandbox.go defines the AURA_SANDBOX_* operator config surface (Phase 37, SBX
// foundation): the primitive per-identity-box tuning knobs 37-05/37-06 read into
// usersandbox.specFor / buildEgressSidecar. Split out of config.go so the root stays
// under the 600-LOC cap (CLAUDE.md NO GOD CLASS), mirroring the db.Config /
// knowledge.Config sub-struct composition.
//
// SandboxConfig is a LEAF: it holds only primitives and imports nothing from
// internal/sandbox — config is a leaf, and 37-05's specFor maps these primitives into
// usersandbox.Resources / EgressPolicy. Every field is a non-fatal envutil-style fallback
// (a malformed ad-hoc tweak falls back at load, never boots fatal); the per-profile
// re-parse pass (config_knobs.go) is what FLAGS a malformed numeric knob Fatal under a
// strict tier — so a bad cap/TTL can never silently degrade into an unsafe value.
package config

import (
	"os"
	"strconv"

	"github.com/chetto1983/aura/internal/envutil"
)

// defaultSandboxImage is the box image ref default (D-13). Operators SHOULD override
// AURA_SANDBOX_IMAGE with a digest-pinned registry ref in production; the local build tag
// (docker/aura-sandbox/Dockerfile builds `aura-sandbox`) is the dev default.
const defaultSandboxImage = "aura-sandbox:latest"

// defaultSandboxEgressImage is the aura-egress sidecar image ref default (SBX-04, D-06). It is
// deliberately NON-EMPTY so the always-on tenancy floor is ON BY DEFAULT under the strict
// profiles (ROADMAP Success Criterion #4): buildSandboxRouter passes it to usersandbox.WithEgress,
// so every strict-profile box gets the DROP-RFC1918/metadata/bridge floor without an opt-in. An
// EMPTY value would leave the floor OFF (WithEgress("") is a guarded no-op), which is NOT the
// default — a strict host must fail-CLOSED (refuse box creation) when this image is unavailable,
// never run un-floored. Operators SHOULD override with a digest-pinned registry ref in production
// (docker/aura-egress/Dockerfile builds `aura-egress`).
const defaultSandboxEgressImage = "aura-egress:latest"

// Sandbox lifecycle + cgroup-cap defaults (D-08 idle-TTL, D-14 caps — tunable starting
// points, not a scale SLA).
const (
	defaultSandboxIdleTTLSec  = 1800           // 30 min idle-suspend TTL (D-08)
	defaultSandboxCPULimit    = 2              // 2 CPUs per box (D-14 start)
	defaultSandboxMemoryLimit = int64(2) << 30 // 2 GiB per box (D-14 start)
	defaultSandboxPidsLimit   = int64(512)     // 512 pids per box (D-14 start)
)

// SandboxConfig is the AURA_SANDBOX_* operator surface for the Phase-37 per-identity
// full-capability box. It is DEFINED + cataloged + parse-tested by plan 37-01 only —
// 37-05/37-06 wire these primitives into usersandbox.specFor / buildEgressSidecar. All
// fields follow the AURA_<DOMAIN>_<UNIT> convention.
type SandboxConfig struct {
	// IdleTTLSec is the idle-suspend TTL in SECONDS (D-08). A plain int-seconds knob (NOT a
	// time.Duration) so it registers KindInt like AURA_RUN_DIR_SWEEP_INTERVAL_SEC and a
	// malformed value is caught by the reparse pass, never silently defaulted. 37-05's
	// reaper reads it as time.Duration(IdleTTLSec)*time.Second.
	IdleTTLSec int // AURA_SANDBOX_IDLE_TTL_SEC — idle-suspend TTL seconds, default 1800 (30m)

	// CPULimit is the per-box CPU-count cap (D-14). 37-05 maps it into
	// container.Resources.NanoCPUs (= int64(CPULimit) * 1e9).
	CPULimit int // AURA_SANDBOX_CPU_LIMIT — per-box CPU count, default 2

	// MemoryLimit is the per-box memory cap in BYTES (D-14) → container.Resources.Memory.
	MemoryLimit int64 // AURA_SANDBOX_MEMORY_LIMIT — per-box memory bytes, default 2 GiB

	// PidsLimit is the per-box PID cap (D-14) → container.Resources.PidsLimit.
	PidsLimit int64 // AURA_SANDBOX_PIDS_LIMIT — per-box pids cap, default 512

	// EgressAllowlist is the operator-tightened egress allowlist (D-06). Empty (the default)
	// = floor-only (full public internet minus the tenancy boundary); a non-empty list is
	// enforced via nftables by 37-06's buildEgressSidecar.
	EgressAllowlist []string // AURA_SANDBOX_EGRESS_ALLOWLIST — CSV allowlist, empty default = floor-only

	// Image is the box image ref (D-13). Default is the local build tag; production SHOULD
	// override with a digest-pinned registry ref.
	Image string // AURA_SANDBOX_IMAGE — box image reference, default aura-sandbox:latest

	// EgressImage is the aura-egress sidecar image ref buildSandboxRouter passes to
	// usersandbox.WithEgress (SBX-04, D-06). An EMPTY value disables the sidecar (WithEgress("")
	// is a guarded no-op), so the default is NON-EMPTY — the tenancy floor is ON by default under
	// the strict profiles (SC#4). A strict host without this image built/available fails-CLOSED
	// (box creation refuses), never runs un-floored.
	EgressImage string // AURA_SANDBOX_EGRESS_IMAGE — egress sidecar image, default aura-egress:latest (non-empty = floor-on)
}

// loadSandboxConfig reads the AURA_SANDBOX_* surface with non-fatal fallbacks, wired into
// loadBase() as cfg.Sandbox. The numeric knobs are cataloged KindInt in config_knobs.go so
// the per-profile reparse pass flags a malformed value Fatal under a strict tier (never a
// silent fallback into an unsafe cap/TTL — the W-cfg bug this KindInt registration fixes).
func loadSandboxConfig() SandboxConfig {
	return SandboxConfig{
		IdleTTLSec:      envutil.IntDefault("AURA_SANDBOX_IDLE_TTL_SEC", defaultSandboxIdleTTLSec),
		CPULimit:        envutil.IntDefault("AURA_SANDBOX_CPU_LIMIT", defaultSandboxCPULimit),
		MemoryLimit:     int64Default("AURA_SANDBOX_MEMORY_LIMIT", defaultSandboxMemoryLimit),
		PidsLimit:       int64Default("AURA_SANDBOX_PIDS_LIMIT", defaultSandboxPidsLimit),
		EgressAllowlist: envSliceDefault("AURA_SANDBOX_EGRESS_ALLOWLIST", nil),
		Image:           envDefault("AURA_SANDBOX_IMAGE", defaultSandboxImage),
		EgressImage:     envDefault("AURA_SANDBOX_EGRESS_IMAGE", defaultSandboxEgressImage),
	}
}

// int64Default returns the int64 value of key, falling back to fallback when the variable
// is unset, empty, or fails to parse — the int64 analog of envutil.IntDefault for the byte
// and pid caps whose defaults exceed the int32 range (a 2 GiB default overflows int on a
// 32-bit build). envutil's leaf is deliberately int-only (its documented scope), so this
// stays a config-package helper. A malformed value is silently absorbed to the fallback
// (non-fatal boot); the reparse pass is what surfaces it under a strict tier.
func int64Default(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
