// config_validate.go holds the boot-time configuration validation split out of
// config.go (refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS): the REQUIRED-secret
// fail-fast Validate lived inline in config.go until it pushed the file against the
// 600-LOC cap that the whole-tree file-size hook enforces. Moving it here frees the
// headroom Phase 33 needs and gives the per-profile validation surface (plans 02-05)
// a home beside the contract types it produces.
//
// The Violation/Severity types are the contract the runtime-profile re-parse pass
// (config_knobs.go) and the `aura config validate` CLI consume. Beside the moved
// Validate this file holds the small set of pure, total, table-testable bespoke
// gates (each mirroring GuardWebBind — `config:`-prefixed, NAMING the offending
// knob, never echoing its VALUE) that encode the D-09..D-16 profile rule matrix:
// the strict set (single_user_hardened + server_production, via RuntimeProfile.Strict)
// rejects sample object-store creds, an empty Garage RPC secret and an absent
// web-auth secret; server_production additionally rejects a single-replica
// object store and a disabled destructive-shell gate (the hardened↔prod differentiator,
// D-11/D-15). The gates REFUSE and NAME — they never silently coerce an operator value
// to a "safe" one (D-05).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Severity ranks a configuration Violation. Warn is advisory (the deploy boots but
// the operator is told); Fatal fails closed under a strict runtime profile. The
// zero value is Warn so an unset severity never silently escalates to Fatal.
type Severity int

const (
	// Warn is an advisory violation: surfaced to the operator, non-blocking.
	Warn Severity = iota
	// Fatal blocks boot/validation under a strict runtime profile.
	Fatal
)

// Violation is one unmet configuration requirement. Knob names the offending env
// var (so operator output is actionable), Sev ranks it, and Msg explains the fix.
// The validation pass aggregates a []Violation — it lists EVERY unmet requirement
// rather than failing on the first, mirroring Validate's missing-secrets aggregation.
// Msg never echoes a secret VALUE (T-33-08): it names the knob and states the rule.
type Violation struct {
	Knob string
	Sev  Severity
	Msg  string
}

// Validate is the profile-aware boot fail-fast: it resolves the active runtime
// profile (c.Profile, default dev — D-03), runs the full ValidateProfile aggregation,
// and joins every Fatal violation into a single config:-prefixed error so a
// misconfigured deploy errors at boot NAMING every unmet requirement at once instead of
// a late, cryptic DB auth failure or a silently degraded graph (O-04, criterion #1).
// Under the lenient tiers (dev/local_trusted) a misconfigured cataloged knob is Warn —
// not Fatal — so Validate stays nil and today's full-host boot is unchanged
// (criterion #4); under the strict tiers any Fatal fails closed. The LLM API key has
// its own fail-fast in llm.Load (D-22). The daemon/REPL boot wires it in; the DB-only
// commands (LoadDB) skip it.
func (c *Config) Validate() error {
	p := c.Profile
	if p == "" {
		p = ProfileDev
	}
	var msgs []string
	for _, v := range c.ValidateProfile(p) {
		if v.Sev == Fatal {
			msgs = append(msgs, v.Knob+": "+v.Msg)
		}
	}
	if len(msgs) > 0 {
		return fmt.Errorf("config: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// ValidateProfile is the aggregating config-contract (D-04/D-08): it returns the UNION
// of the all-tier gates (required secrets, RunDir, web-bind), the generic kind-driven
// re-parse pass (reparsePass — Fatal under strict, Warn under lenient, PROF-04/F-016),
// and every bespoke security gate, passing the resolved profile p. It NEVER first-fails
// — the operator sees EVERY unmet requirement in one pass (criterion #1). The only env
// reads are reparsePass (cataloged knobs), gateDestructiveShell (the raw
// destructive-patterns knob); it performs no other I/O and never echoes a secret VALUE.
func (c *Config) ValidateProfile(p RuntimeProfile) []Violation {
	var vs []Violation
	vs = append(vs, c.gateRequiredSecrets()...)
	vs = append(vs, c.gateRunDir()...)
	vs = append(vs, c.gateWebBind()...)
	vs = append(vs, reparsePass(p)...)
	vs = append(vs, gateHistoryHardCap()...)
	vs = append(vs, c.gateObjectStoreCreds(p)...)
	vs = append(vs, c.gateGarageRPCSecret(p)...)
	vs = append(vs, c.gateReplication(p)...)
	vs = append(vs, c.gateDestructiveShell(p)...)
	vs = append(vs, c.gateReasoningTraceFull(p)...)
	vs = append(vs, c.gateWebAuth(p)...)
	vs = append(vs, c.gateObjectStoreEndpoint(p)...)
	vs = append(vs, c.gateDocumentRetrieval(p)...)
	vs = append(vs, c.gateAssetProcessingLease()...)
	vs = append(vs, c.gateRetention()...)
	vs = append(vs, c.gateMultiUserRequiresStrictProfile(p)...)
	return vs
}

// gateMultiUserRequiresStrictProfile keeps multi-user provisioning an explicit hardened-
// deployment posture. Tenant data and tool sandboxes are identity-scoped; the remaining
// deployment-global settings, MCP, scheduler-governance and skills catalogs are intentional
// administrator control planes protected by governance.read/write. The strict-profile
// requirement therefore controls the runtime posture, not tenant selection.
func (c *Config) gateMultiUserRequiresStrictProfile(p RuntimeProfile) []Violation {
	if c == nil || !c.MUSRIsolation || p.Strict() {
		return nil
	}
	return []Violation{{
		Knob: "AURA_MUSR_ISOLATION",
		Sev:  Fatal,
		// AURA_PROFILE, not AURA_RUNTIME_PROFILE. config.go:400 reads AURA_PROFILE and
		// nothing anywhere reads the other name — a fail-closed gate that tells the
		// operator to set a variable with no effect leaves them stuck at a boot loop
		// following its own instructions. Found on the live box, 2026-08-03.
		Msg: "a second identity needs a hardened profile: set AURA_PROFILE to " +
			"single_user_hardened or server_production, or leave AURA_MUSR_ISOLATION off — " +
			"multi-user provisioning is supported only under a strict runtime posture; " +
			"deployment-global catalogs remain administrator-only via governance.read/write",
	}}
}

func gateHistoryHardCap() []Violation {
	raw, configured := os.LookupEnv("AURA_HISTORY_HARD_CAP_TURNS")
	if !configured {
		return nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return nil // reparsePass reports the malformed integer.
	}
	if value < 4 || value > 1000 {
		return []Violation{{
			Knob: "AURA_HISTORY_HARD_CAP_TURNS",
			Sev:  Fatal,
			Msg:  "must be between 4 and 1000 rows per managed-history page",
		}}
	}
	return nil
}

func (c *Config) gateRetention() []Violation {
	// Programmatic legacy Config literals omit newly added optional sub-configs.
	// Load/LoadDB always populate Retention; preserve compatibility for the zero value.
	if c.Retention == (RetentionConfig{}) {
		return nil
	}
	if err := c.Retention.Validate(); err != nil {
		return []Violation{{Knob: "AURA_RETENTION_*", Sev: Fatal, Msg: err.Error()}}
	}
	return nil
}

// gateRequiredSecrets flags an empty REQUIRED infrastructure secret (the DB DSN)
// as Fatal in EVERY tier (O-04): it is required for any daemon/REPL boot
// regardless of profile, so the profile validator never relaxes it. It names the
// offending knob and never echoes the (empty) secret VALUE.
func (c *Config) gateRequiredSecrets() []Violation {
	if strings.TrimSpace(c.DB.URL) == "" {
		return []Violation{{Knob: "POSTGRES_PASSWORD (or AURA_DB_URL)", Sev: Fatal, Msg: "required DB secret is unset"}}
	}
	return nil
}

// gateRunDir flags a non-absolute AURA_RUN_DIR as Fatal in EVERY tier (F-041/PROF-05):
// sidecars must resolve against a stable root, not the process cwd. RunDirErr is the
// load-time failure (cwd unobtainable so filepath.Abs could not normalize); the
// IsAbs guard is the defensive backstop for a Config that somehow carries a relative
// RunDir that bypassed absRunDir. Either surfaces as one named Fatal.
func (c *Config) gateRunDir() []Violation {
	if c.RunDirErr != nil {
		return []Violation{{Knob: "AURA_RUN_DIR", Sev: Fatal, Msg: c.RunDirErr.Error()}}
	}
	if c.RunDir != "" && !filepath.IsAbs(c.RunDir) {
		return []Violation{{Knob: "AURA_RUN_DIR", Sev: Fatal, Msg: fmt.Sprintf("must be an absolute path, got %q", c.RunDir)}}
	}
	return nil
}

// gateWebBind reuses the existing WEB-02 GuardWebBind policy (loopback always boots;
// a non-loopback bind requires web-auth OR a trusted proxy) as a Violation in EVERY
// tier, naming AURA_AGUI_BIND. Loopback (the default) yields no violation.
func (c *Config) gateWebBind() []Violation {
	if err := GuardWebBind(c.AGUIBind, strings.TrimSpace(c.AuthulaSecret) != "", c.WebTrustProxy); err != nil {
		return []Violation{{Knob: "AURA_AGUI_BIND", Sev: Fatal, Msg: err.Error()}}
	}
	return nil
}

// gateObjectStoreCreds rejects the in-tree SAMPLE object-store credentials under the
// strict tiers (single_user_hardened + server_production, D-10/D-11/PROF-03/F-007):
// a deploy that ships the default sentinel keys is a credential-reuse EoP. Lenient
// tiers (dev/local_trusted) return no violation — full-host parity (D-09). The
// compare targets are PUBLIC constants (config.go), so == is correct: no secret is
// involved and constant-time comparison is not required (RESEARCH §Security V6).
func (c *Config) gateObjectStoreCreds(p RuntimeProfile) []Violation {
	if !p.Strict() {
		return nil
	}
	var vs []Violation
	if c.ObjectStoreAccessKey == defaultObjectStoreAccessKey {
		vs = append(vs, Violation{Knob: "AURA_OBJECTSTORE_ACCESS_KEY", Sev: Fatal, Msg: "sample object-store access key rejected under " + string(p)})
	}
	if c.ObjectStoreSecretKey == defaultObjectStoreSecretKey {
		vs = append(vs, Violation{Knob: "AURA_OBJECTSTORE_SECRET_KEY", Sev: Fatal, Msg: "sample object-store secret key rejected under " + string(p)})
	}
	return vs
}

// gateGarageRPCSecret requires a non-empty GARAGE_RPC_SECRET under the strict tiers
// (PROF-03/F-007/A5): an empty inter-node RPC secret leaves the object-store cluster
// unauthenticated. No in-tree sample literal exists (install.sh generates it), so the
// reject baseline is "empty". Lenient tiers return no violation.
func (c *Config) gateGarageRPCSecret(p RuntimeProfile) []Violation {
	if !p.Strict() {
		return nil
	}
	if strings.TrimSpace(c.GarageRPCSecret) == "" {
		return []Violation{{Knob: "GARAGE_RPC_SECRET", Sev: Fatal, Msg: "inter-node RPC secret must be set under " + string(p)}}
	}
	return nil
}

// gateReplication requires an object-store replication factor of at least 2 under
// server_production ONLY (PROF-06/F-018): a single replica is non-durable. This is the
// clean hardened↔prod differentiator (D-11) — single_user_hardened (the single-node
// appliance tier) ALLOWS replication = 1, so this gate is a no-op outside production.
func (c *Config) gateReplication(p RuntimeProfile) []Violation {
	if p != ProfileServerProduction {
		return nil
	}
	if c.ObjectStoreReplicationFactor < 2 {
		return []Violation{{Knob: "AURA_OBJECTSTORE_REPLICATION_FACTOR", Sev: Fatal, Msg: fmt.Sprintf("must be >= 2 for durability under server_production, got %d", c.ObjectStoreReplicationFactor)}}
	}
	return nil
}

// gateDestructiveShell forbids an explicitly DISABLED destructive-shell gate under
// server_production ONLY (D-11/D-15/F-002): single_user_hardened ALLOWS `off`
// (appliance-operator flexibility, A3). It reads the RAW env value directly — it does
// NOT import the agent tools package (scope fence): the runtime leaf stays
// profile-agnostic and this validator only inspects the operator's declared knob. Only
// the literal `off` (case-insensitive) disables the gate; every other value keeps it active.
func (c *Config) gateDestructiveShell(p RuntimeProfile) []Violation {
	if p != ProfileServerProduction {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("AURA_SHELL_DESTRUCTIVE_PATTERNS"))
	if strings.EqualFold(raw, "off") {
		return []Violation{{Knob: "AURA_SHELL_DESTRUCTIVE_PATTERNS", Sev: Fatal, Msg: "destructive-shell gate must not be disabled (off) under server_production"}}
	}
	return nil
}

// gateReasoningTraceFull requires an explicit acknowledgement before a strict
// deployment enables detailed reasoning traces. Encryption is independently
// fail-closed at the sink, so this gate never coerces the requested mode.
func (c *Config) gateReasoningTraceFull(p RuntimeProfile) []Violation {
	if !p.Strict() {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AURA_REASONING_TRACE")), "full") {
		return nil
	}
	if strings.TrimSpace(os.Getenv("AURA_TRACE_FULL_ACK")) != "1" {
		return []Violation{{
			Knob: "AURA_REASONING_TRACE",
			Sev:  Fatal,
			Msg:  "full trace requires AURA_TRACE_FULL_ACK=1 under " + string(p),
		}}
	}
	return nil
}

// gateWebAuth requires a web-auth secret (AURA_AUTHULA_SECRET) under the strict tiers
// (D-10/D-11): a hardened/production cockpit must authenticate. Lenient tiers return
// no violation (a loopback dev cockpit boots without auth, the compensating control).
func (c *Config) gateWebAuth(p RuntimeProfile) []Violation {
	if !p.Strict() {
		return nil
	}
	if strings.TrimSpace(c.AuthulaSecret) == "" {
		return []Violation{{Knob: "AURA_AUTHULA_SECRET", Sev: Fatal, Msg: "web-auth secret is required under " + string(p)}}
	}
	return nil
}

// gateObjectStoreEndpoint emits non-fatal WARNs under server_production (A6) when the
// object store still uses the default bucket name or a loopback endpoint: both can be
// legitimate behind compose DNS, so they are advisory, never Fatal — the operator is
// told to confirm intent, but a valid prod deploy is not blocked.
func (c *Config) gateObjectStoreEndpoint(p RuntimeProfile) []Violation {
	if p != ProfileServerProduction {
		return nil
	}
	var vs []Violation
	if c.ObjectStoreBucket == "aura-assets" {
		vs = append(vs, Violation{Knob: "AURA_OBJECTSTORE_BUCKET", Sev: Warn, Msg: "default bucket name in production — confirm it is intentional"})
	}
	if isLoopbackEndpoint(c.ObjectStoreEndpoint) {
		vs = append(vs, Violation{Knob: "AURA_OBJECTSTORE_ENDPOINT", Sev: Warn, Msg: "loopback object-store endpoint in production — confirm it is reachable"})
	}
	return vs
}

// isLoopbackEndpoint reports whether an object-store endpoint URL targets a loopback
// host. It is a lightweight substring heuristic (the WARN is advisory, not a gate), so
// it deliberately avoids a full URL parse: any of the loopback literals suffices.
func isLoopbackEndpoint(endpoint string) bool {
	e := strings.ToLower(endpoint)
	return strings.Contains(e, "127.0.0.1") || strings.Contains(e, "localhost") || strings.Contains(e, "::1")
}
