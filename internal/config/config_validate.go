// config_validate.go holds the boot-time configuration validation split out of
// config.go (refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS): the REQUIRED-secret
// fail-fast Validate lived inline in config.go until it pushed the file against the
// 600-LOC cap that the whole-tree file-size hook enforces. Moving it here frees the
// headroom Phase 33 needs and gives the per-profile validation surface (plans 02-05)
// a home beside the contract types it produces.
//
// Validate is behaviour-identical to its former config.go location — this is a pure
// relocation. The Violation/Severity types are the contract the runtime-profile
// re-parse pass (config_knobs.go) and the `aura config validate` CLI consume.
package config

import (
	"fmt"
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
type Violation struct {
	Knob string
	Sev  Severity
	Msg  string
}

// Validate fails fast on an empty REQUIRED infrastructure secret so a misconfigured
// deploy errors at boot with a named cause instead of a late, cryptic DB auth
// failure or a silently degraded graph (O-04). The LLM API key has its own
// fail-fast in llm.Load (D-22); this covers the composed DB DSN and the Neo4j
// password. The daemon/REPL boot wires it in; the DB-only commands (LoadDB) skip it.
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.DB.URL) == "" {
		missing = append(missing, "POSTGRES_PASSWORD (or AURA_DB_URL)")
	}
	if strings.TrimSpace(c.Neo4j.Password) == "" {
		missing = append(missing, "NEO4J_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: required secret(s) unset: %s", strings.Join(missing, ", "))
	}
	if c.RunDirErr != nil {
		return fmt.Errorf("config: %w", c.RunDirErr)
	}
	return nil
}
