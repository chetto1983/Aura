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

// KnobSpec is one row in the single-source-of-truth registry (D-08). Name is the
// env key, Kind drives the generic re-parse check, Default documents the in-code
// fallback (for .env.example / doc generation), Enum lists the valid set for a
// KindEnum row, and Secret marks a value that must be redacted in any rendered
// output (plan 05) — the re-parse pass itself never echoes a knob VALUE.
type KnobSpec struct {
	Name    string
	Kind    KnobKind
	Default string
	Enum    []string
	Secret  bool
}

// knobRegistry is the single source of truth (D-08): one KnobSpec per Tier A + Tier B
// hot-path knob (RESEARCH §"Knob Registry Catalogue"). Tier A is the security/reliability
// gate surface; Tier B is the int/bool reliability surface read in internal/config (the
// F-016 silent-fallback knobs). Tier C (agent-tools/loop/llm) is a documented follow-on
// and is deliberately OUT (D-16). Defaults mirror the in-code fallbacks in config.go so
// the registry stays the authoritative catalogue.
func knobRegistry() []KnobSpec {
	return []KnobSpec{
		// --- Tier A: profile selector + security/reliability gate knobs ---
		{Name: "AURA_PROFILE", Kind: KindEnum, Default: string(ProfileDev), Enum: []string{
			string(ProfileDev),
			string(ProfileLocalTrusted),
			string(ProfileSingleUserHardened),
			string(ProfileServerProduction),
		}},
		{Name: "AURA_OBJECTSTORE_ACCESS_KEY", Kind: KindString, Default: defaultObjectStoreAccessKey, Secret: true},
		{Name: "AURA_OBJECTSTORE_SECRET_KEY", Kind: KindString, Default: defaultObjectStoreSecretKey, Secret: true},
		{Name: "GARAGE_RPC_SECRET", Kind: KindString, Default: "", Secret: true},
		{Name: "AURA_GARAGE_ADMIN_TOKEN", Kind: KindString, Default: "", Secret: true},
		{Name: "AURA_GARAGE_ADMIN_ENDPOINT", Kind: KindString, Default: "http://garage:3903"},
		{Name: "AURA_AUTHULA_SECRET", Kind: KindString, Default: "", Secret: true},
		{Name: "AURA_OBJECTSTORE_BUCKET", Kind: KindString, Default: "aura-assets"},
		{Name: "AURA_OBJECTSTORE_ENDPOINT", Kind: KindString, Default: "http://127.0.0.1:3900"},
		{Name: "AURA_OBJECTSTORE_REPLICATION_FACTOR", Kind: KindInt, Default: "1"},
		{Name: "AURA_AGUI_BIND", Kind: KindString, Default: "127.0.0.1:9080"},
		{Name: "AURA_AGUI_CORS_PERMISSIVE", Kind: KindBool, Default: "false"},
		{Name: "AURA_SHELL_DESTRUCTIVE_PATTERNS", Kind: KindString, Default: ""},
		{Name: "AURA_WEB_TRUST_PROXY", Kind: KindBool, Default: "false"},

		// --- Tier B: int/bool reliability knobs read in internal/config (F-016 surface) ---
		{Name: "AURA_CONTEXT_PREVIEW_CAP_BYTES", Kind: KindInt, Default: "2048"},
		{Name: "AURA_CONVERSATION_TURN_CAP_BYTES", Kind: KindInt, Default: "65536"},
		{Name: "AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", Kind: KindInt, Default: "10"},
		{Name: "AURA_HISTORY_HARD_CAP_TURNS", Kind: KindInt, Default: "50"},
		{Name: "AURA_RUN_DIR_WARN_THRESHOLD_BYTES", Kind: KindInt, Default: "1073741824"},
		{Name: "AURA_RUN_DIR_SWEEP_INTERVAL_SEC", Kind: KindInt, Default: "3600"},
		{Name: "AURA_WEB_DNS_PIN_TTL_SEC", Kind: KindInt, Default: "60"},
		{Name: "AURA_WEB_FETCH_MAX_BODY_BYTES", Kind: KindInt, Default: "5000000"},
		{Name: "AURA_WEB_CACHE_PERSISTENT", Kind: KindBool, Default: "false"},
		{Name: "AURA_WEB_SEARCH_TIMEOUT_SEC", Kind: KindInt, Default: "20"},
		{Name: "AURA_WEB_FETCH_TIMEOUT_SEC", Kind: KindInt, Default: "30"},
		{Name: "AURA_SWARM_MAX_GOALS", Kind: KindInt, Default: "8"},
		{Name: "AURA_SWARM_CHILD_TIMEOUT_SEC", Kind: KindInt, Default: "120"},
		{Name: "AURA_SWARM_MAX_CONCURRENT", Kind: KindInt, Default: "4"},
		{Name: "AURA_AGENT_JOB_MAX_DURATION_SEC", Kind: KindInt, Default: "600"},
		{Name: "AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL", Kind: KindBool, Default: "true"},
		{Name: "AURA_SKILL_BODY_CAP_BYTES", Kind: KindInt, Default: "32768"},
		{Name: "AURA_SKILL_MANIFEST_CAP_BYTES", Kind: KindInt, Default: "8192"},
		{Name: "AURA_SKILL_SNIPPET_TTL_DAYS", Kind: KindInt, Default: "90"},
		{Name: "AURA_AGUI_BUFFER_CAP", Kind: KindInt, Default: "64"},
		{Name: "AURA_ASSET_MAX_DOCUMENT_BYTES", Kind: KindInt, Default: "104857600"},
		{Name: "AURA_ASSET_MAX_IMAGE_BYTES", Kind: KindInt, Default: "26214400"},
		{Name: "AURA_ASSET_MAX_AUDIO_BYTES", Kind: KindInt, Default: "104857600"},
		{Name: "AURA_ASSET_PRESIGN_TTL_SEC", Kind: KindInt, Default: "600"},
		{Name: "AURA_ASSET_PROCESSING_CONCURRENCY", Kind: KindInt, Default: "2"},
		{Name: "AURA_OBJECTSTORE_PATH_STYLE", Kind: KindBool, Default: "true"},
		{Name: "AURA_TELEGRAM_LOCAL_BOT_API", Kind: KindBool, Default: "false"},
		{Name: "AURA_AUTHULA_RATE_LIMIT_MAX", Kind: KindInt, Default: "30"},
		{Name: "AURA_SERVE_SHUTDOWN_GRACE_SEC", Kind: KindInt, Default: "25"},
		{Name: "AURA_VISION_CLOUD", Kind: KindBool, Default: "false"},
		{Name: "AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC", Kind: KindInt, Default: "10"},
		{Name: "AURA_EMBED_DIMENSIONS", Kind: KindInt, Default: "768"},
		{Name: "AURA_PROFILE_CERTAINTY_N", Kind: KindInt, Default: "3"},

		// Phase 36 identity-isolation rollout switch (D-13): the documents-retrieval
		// scoped-vs-unscoped path selector (plan 05 consumer, plan 12 flip). Catalogued so
		// `aura config validate` flags a malformed value under a strict tier; it is a
		// dedicated config field, NOT a mutable internal/settings OverlayEnv knob.
		{Name: "AURA_MUSR_ISOLATION", Kind: KindBool, Default: "false"},

		// Phase 37 per-identity sandbox operator surface (SBX foundation, config_sandbox.go).
		// The numeric caps + TTL are KindInt so the reparse pass FLAGS a malformed value Fatal
		// under a strict tier (T-37-01-CFG) — a KindString registration would get NO reparse
		// check and could silently fall back into an unsafe cap/TTL under server_production.
		// AURA_SANDBOX_IDLE_TTL_SEC is int-seconds, mirroring AURA_RUN_DIR_SWEEP_INTERVAL_SEC.
		// Image + egress-allowlist are free-form strings (no numeric parse to validate).
		{Name: "AURA_SANDBOX_IDLE_TTL_SEC", Kind: KindInt, Default: "1800"},
		{Name: "AURA_SANDBOX_CPU_LIMIT", Kind: KindInt, Default: "2"},
		{Name: "AURA_SANDBOX_MEMORY_LIMIT", Kind: KindInt, Default: "2147483648"},
		{Name: "AURA_SANDBOX_PIDS_LIMIT", Kind: KindInt, Default: "512"},
		{Name: "AURA_SANDBOX_EGRESS_ALLOWLIST", Kind: KindString, Default: ""},
		{Name: "AURA_SANDBOX_IMAGE", Kind: KindString, Default: "aura-sandbox:latest"},
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
// knob VALUE (T-33-03b: secrets are flagged via KnobSpec.Secret, never echoed here).
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
