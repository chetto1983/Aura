// config_knobs.go is the QUAL-04 deliverable: a data registry of the hot-path
// AURA_* knobs (Tier A + Tier B, D-16) that doubles as the validation ENGINE
// (D-08 — "the registry IS the engine"). It is the single source of truth the
// per-profile validator (config_validate.go, plan 04) and the `aura config validate`
// renderer (cmd/aura, plan 05) read; nothing here enforces a runtime capability.
//
// The registry feeds the generic, kind-driven re-parse pass (plan 03 task 2):
// every cataloged int/bool/enum knob is re-read straight from the environment and
// re-parsed with the SAME stdlib strconv mechanics the envutil leaf uses — but for
// DIAGNOSTICS instead of silent fallback (D-06). The leaf (internal/envutil) stays
// the dumb silent-fallback surface, UNTOUCHED; this pass never couples it to profile
// state, it only re-reads raw env independently to flag misconfiguration.
package config

import (
	"os"
	"slices"
	"strconv"
	"strings"
)

// KnobKind classifies a KnobSpec so the generic re-parse pass can pick the right
// stdlib check (strconv.Atoi / strconv.ParseBool / slices.Contains) with zero
// per-knob code. KindString carries no parse check — secret/path string knobs are
// validated (when at all) by the bespoke gates in config_validate.go (plan 04).
type KnobKind int

const (
	// KindString is a free-form string knob (no re-parse check here).
	KindString KnobKind = iota
	// KindInt is re-parsed with strconv.Atoi.
	KindInt
	// KindBool is re-parsed with strconv.ParseBool.
	KindBool
	// KindEnum is checked against the Enum set with slices.Contains.
	KindEnum
)

// KnobSpec is one row in the registry (D-08). Name is the env key, Kind drives the
// generic re-parse check, and Enum lists the valid set for a KindEnum row. The re-parse
// pass never echoes a knob VALUE, so no row needs a redaction flag. Defaults live in the
// loaders alone.
type KnobSpec struct {
	Name string
	Kind KnobKind
	Enum []string
}

// knobRegistry is the single source of truth (D-08): one KnobSpec per Tier A + Tier B
// hot-path knob (RESEARCH §"Knob Registry Catalogue"). Tier A is the security/reliability
// gate surface; Tier B is the int/bool reliability surface read in internal/config (the
// F-016 silent-fallback knobs). Tier C (agent-tools/loop/llm) is a documented follow-on
// and is deliberately OUT (D-16).
func knobRegistry() []KnobSpec {
	return []KnobSpec{
		// --- Tier A: profile selector + security/reliability gate knobs ---
		{Name: "AURA_MCP_ENV_DIR", Kind: KindString},
		{Name: "AURA_PLUGINS_DIR", Kind: KindString},
		{Name: "AURA_PROFILE", Kind: KindEnum, Enum: []string{
			string(ProfileDev),
			string(ProfileLocalTrusted),
			string(ProfileSingleUserHardened),
			string(ProfileServerProduction),
		}},
		{Name: "AURA_OBJECTSTORE_ACCESS_KEY", Kind: KindString},
		{Name: "AURA_OBJECTSTORE_SECRET_KEY", Kind: KindString},
		{Name: "GARAGE_RPC_SECRET", Kind: KindString},
		{Name: "AURA_GARAGE_ADMIN_TOKEN", Kind: KindString},
		{Name: "AURA_GARAGE_ADMIN_ENDPOINT", Kind: KindString},
		{Name: "AURA_AUTHULA_SECRET", Kind: KindString},
		{Name: "AURA_WEB_PUBLIC_URL", Kind: KindString},
		{Name: "AURA_OBJECTSTORE_BUCKET", Kind: KindString},
		{Name: "AURA_OBJECTSTORE_ENDPOINT", Kind: KindString},
		{Name: "AURA_OBJECTSTORE_REPLICATION_FACTOR", Kind: KindInt},
		{Name: "AURA_AGUI_BIND", Kind: KindString},
		{Name: "AURA_SHELL_DESTRUCTIVE_PATTERNS", Kind: KindString},
		{Name: "AURA_WEB_TRUST_PROXY", Kind: KindBool},
		// Reasoning trace accepts legacy truthy spellings as well as summary/full,
		// so it remains a string knob; the strict-profile full-mode policy is
		// enforced by gateReasoningTraceFull.
		{Name: "AURA_REASONING_TRACE", Kind: KindString},
		{Name: "AURA_TRACE_FULL_ACK", Kind: KindBool},
		{Name: "AURA_TRACE_ENCRYPT_KEY", Kind: KindString},

		// --- Tier B: int/bool reliability knobs read in internal/config (F-016 surface) ---
		{Name: "AURA_CONTEXT_PREVIEW_CAP_BYTES", Kind: KindInt},
		{Name: "AURA_CONVERSATION_TURN_CAP_BYTES", Kind: KindInt},
		{Name: "AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", Kind: KindInt},
		{Name: "AURA_HISTORY_HARD_CAP_TURNS", Kind: KindInt},
		{Name: "AURA_CONTEXT_COMPACTION_ENABLED", Kind: KindBool},
		{Name: "AURA_CONTEXT_COMPACTION_MODEL", Kind: KindString},
		{Name: "AURA_MEMORY_PRELOAD_ENABLED", Kind: KindBool},
		{Name: "AURA_MEMORY_PRELOAD_TOP_K", Kind: KindInt},
		{Name: "AURA_MEMORY_PRELOAD_TIMEOUT_MS", Kind: KindInt},
		{Name: "AURA_RUN_DIR_WARN_THRESHOLD_BYTES", Kind: KindInt},
		{Name: "AURA_RUN_DIR_SWEEP_INTERVAL_SEC", Kind: KindInt},
		{Name: "AURA_WEB_DNS_PIN_TTL_SEC", Kind: KindInt},
		{Name: "AURA_WEB_FETCH_MAX_BODY_BYTES", Kind: KindInt},
		{Name: "AURA_WEB_CACHE_PERSISTENT", Kind: KindBool},
		{Name: "AURA_WEB_SEARCH_TIMEOUT_SEC", Kind: KindInt},
		{Name: "AURA_WEB_FETCH_TIMEOUT_SEC", Kind: KindInt},
		{Name: "AURA_SWARM_MAX_GOALS", Kind: KindInt},
		{Name: "AURA_SWARM_CHILD_IDLE_SEC", Kind: KindInt},
		{Name: "AURA_SWARM_MAX_CONCURRENT", Kind: KindInt},
		{Name: "AURA_SWARM_DELEGATION_NUDGE_SEC", Kind: KindInt},
		{Name: "AURA_AGENT_JOB_MAX_DURATION_SEC", Kind: KindInt},
		{Name: "AURA_SKILL_BODY_CAP_BYTES", Kind: KindInt},
		{Name: "AURA_SKILL_MANIFEST_CAP_BYTES", Kind: KindInt},
		{Name: "AURA_SKILL_SNIPPET_TTL_DAYS", Kind: KindInt},
		{Name: "AURA_AGUI_BUFFER_CAP", Kind: KindInt},
		{Name: "AURA_AGUI_SSE_HEARTBEAT_SEC", Kind: KindInt},
		{Name: "AURA_AGUI_RUN_DETACH", Kind: KindBool}, // D-11: catalogue corrected to match the loader's real default, never the other way
		{Name: "AURA_AGUI_RUN_BUFFER_EVENTS", Kind: KindInt},
		{Name: "AURA_AGUI_RUN_LINGER_SEC", Kind: KindInt},
		{Name: "AURA_AGUI_RUN_MAX_WALLCLOCK_SEC", Kind: KindInt},
		{Name: "AURA_AGUI_RUN_MAX_LIVE", Kind: KindInt},
		{Name: "AURA_AGUI_RUN_STEER", Kind: KindBool}, // D-12: off is dark code
		{Name: "AURA_AGUI_RUN_STEER_MAX", Kind: KindInt},
		{Name: "AURA_AGUI_RUN_STEER_MAX_BYTES", Kind: KindInt},
		{Name: "AURA_ASKUSER_PAUSE_TTL_SEC", Kind: KindInt},
		{Name: "AURA_ASSET_MAX_DOCUMENT_BYTES", Kind: KindInt},
		{Name: "AURA_ASSET_MAX_IMAGE_BYTES", Kind: KindInt},
		{Name: "AURA_ASSET_MAX_AUDIO_BYTES", Kind: KindInt},
		{Name: "AURA_ASSET_PRESIGN_TTL_SEC", Kind: KindInt},
		{Name: "AURA_ASSET_PROCESSING_CONCURRENCY", Kind: KindInt},
		{Name: "AURA_ASSET_PROCESSING_LEASE_SEC", Kind: KindInt},
		{Name: "AURA_EMBED_REVISION", Kind: KindString},
		{Name: "AURA_EMBED_FINGERPRINT", Kind: KindString},
		{Name: "AURA_OBJECTSTORE_PATH_STYLE", Kind: KindBool},
		{Name: "AURA_TELEGRAM_LOCAL_BOT_API", Kind: KindBool},
		{Name: "AURA_AUTHULA_RATE_LIMIT_MAX", Kind: KindInt},
		{Name: "AURA_SERVE_SHUTDOWN_GRACE_SEC", Kind: KindInt},
		{Name: "AURA_RETENTION_TEMP_HOURS", Kind: KindInt},
		{Name: "AURA_RETENTION_PROD_TRACE_HOURS", Kind: KindInt},
		{Name: "AURA_RETENTION_DEV_TRACE_HOURS", Kind: KindInt},
		{Name: "AURA_RETENTION_METADATA_TRACE_HOURS", Kind: KindInt},
		{Name: "AURA_RETENTION_CONVERSATION_DAYS", Kind: KindInt},
		{Name: "AURA_RETENTION_REASONING_SUCCESS_DAYS", Kind: KindInt},
		{Name: "AURA_RETENTION_REASONING_FAILED_DAYS", Kind: KindInt},
		{Name: "AURA_RETENTION_BATCH_SIZE", Kind: KindInt},
		{Name: "AURA_RETENTION_LEASE_SEC", Kind: KindInt},
		{Name: "AURA_RETENTION_MAX_DURATION_SEC", Kind: KindInt},
		{Name: "AURA_RETENTION_DISK_WARN_PERCENT", Kind: KindInt},
		{Name: "AURA_RETENTION_DISK_URGENT_PERCENT", Kind: KindInt},
		{Name: "AURA_RETENTION_DISK_STOP_PERCENT", Kind: KindInt},
		{Name: "AURA_EMBED_DIMENSIONS", Kind: KindInt},
		// The optional non-OpenRouter cloud endpoint. Kept apart from AURA_EMBED_BASE_URL,
		// which names the LOCAL sidecar and nothing else. The switch itself is
		// AURA_EMBED_MODEL, which the cockpit writes into aura.settings.
		{Name: "AURA_EMBED_CLOUD_BASE_URL", Kind: KindString},
		{Name: "AURA_TTS_MAX_CHARS", Kind: KindInt},

		// The multi-identity provisioning switch. Catalogued so `aura config validate`
		// flags a malformed value; it is a dedicated config field, NOT a mutable
		// internal/settings OverlayEnv knob.
		{Name: "AURA_MUSR_ISOLATION", Kind: KindBool},

		// Phase 37 per-identity sandbox operator surface (SBX foundation, config_sandbox.go).
		// The numeric caps + TTL are KindInt so the reparse pass FLAGS a malformed value Fatal
		// under a strict tier (T-37-01-CFG) — a KindString registration would get NO reparse
		// check and could silently fall back into an unsafe cap/TTL under server_production.
		// AURA_SANDBOX_IDLE_TTL_SEC is int-seconds, mirroring AURA_RUN_DIR_SWEEP_INTERVAL_SEC.
		// Image + egress-image + egress-allowlist are free-form strings (no numeric parse to
		// validate). AURA_SANDBOX_EGRESS_IMAGE defaults NON-EMPTY (aura-egress:latest) so the
		// SBX-04 tenancy floor is on-by-default under a strict tier (SC#4).
		{Name: "AURA_SANDBOX_IDLE_TTL_SEC", Kind: KindInt},
		{Name: "AURA_SANDBOX_BROWSER_CPU_LIMIT", Kind: KindInt},
		{Name: "AURA_SANDBOX_BROWSER_MEMORY_LIMIT", Kind: KindInt},
		{Name: "AURA_SANDBOX_BROWSER_PIDS_LIMIT", Kind: KindInt},
		{Name: "AURA_SANDBOX_BROWSER_MAX_ACTIVE", Kind: KindInt},
		{Name: "AURA_SANDBOX_CPU_LIMIT", Kind: KindInt},
		{Name: "AURA_SANDBOX_MEMORY_LIMIT", Kind: KindInt},
		{Name: "AURA_SANDBOX_PIDS_LIMIT", Kind: KindInt},
		{Name: "AURA_SANDBOX_EGRESS_ALLOWLIST", Kind: KindString},
		{Name: "AURA_SANDBOX_IMAGE", Kind: KindString},
		{Name: "AURA_SANDBOX_EGRESS_IMAGE", Kind: KindString},
	}
}

// reparsePass is the generic, kind-driven F-016 re-parse pass (PROF-04 / D-07): it
// re-reads every cataloged knob straight from the environment and re-parses it with
// the same stdlib mechanics envutil uses — but emits a Violation instead of silently
// falling back. Severity is the ONLY profile coupling: Fatal under a strict tier
// (hardened/production), Warn under a lenient one (dev/local_trusted), keyed on the
// plan-01 RuntimeProfile.Strict() helper. Unset or whitespace-only values are skipped
// (the runtime read uses its default via the untouched leaf); KindString rows carry no
// parse check. It aggregates ALL violations — one named row per bad knob, never the
// knob VALUE (T-33-03b), so a secret knob is never echoed here.
func reparsePass(p RuntimeProfile) []Violation {
	sev := Warn
	if p.Strict() {
		sev = Fatal
	}
	var vs []Violation
	for _, k := range knobRegistry() {
		raw, set := os.LookupEnv(k.Name)
		if !set {
			continue
		}
		if strings.TrimSpace(raw) == "" {
			continue // whitespace-only ⇒ uses the default, no violation
		}
		switch k.Kind {
		case KindInt:
			// Parse the RAW (untrimmed) value: envutil.IntDefault does NOT trim, so a
			// whitespace-padded value like " 128" (common from YAML quoting) silently
			// falls back to the default at runtime — surfacing it here is the whole point
			// of the re-parse pass (F-016). Trimming would mask exactly that silent fallback.
			if _, err := strconv.Atoi(raw); err != nil {
				vs = append(vs, Violation{Knob: k.Name, Sev: sev, Msg: "not a valid integer"})
			}
		case KindBool:
			// Raw value, same reason as KindInt — envutil.BoolDefault does not trim.
			if _, err := strconv.ParseBool(raw); err != nil {
				vs = append(vs, Violation{Knob: k.Name, Sev: sev, Msg: "not a valid boolean"})
			}
		case KindEnum:
			// Enum knobs (AURA_PROFILE) are read at runtime via ParseProfile, which DOES
			// trim — so mirror that and match against the trimmed value.
			if !slices.Contains(k.Enum, strings.TrimSpace(raw)) {
				vs = append(vs, Violation{Knob: k.Name, Sev: sev, Msg: "not one of " + strings.Join(k.Enum, ", ")})
			}
		case KindString:
			// no parse check — string/secret/path knobs are validated (if at all)
			// by the bespoke gates in config_validate.go (plan 04).
		}
	}
	return vs
}
