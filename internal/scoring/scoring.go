// Package scoring is the pure, shared risk-tier module (PRD §Risk-Based
// Governance, amendment #41 / D-11): it computes a qualitative RiskTier for
// scheduler tasks and skills so consumers can render an
// advisory gate. It is deliberately pure — no DB, no IO, no env read. The alert
// threshold is supplied by the caller (config owns the env read; this module
// mirrors dnspin taking its TTL as a constructor arg), keeping every function a
// total, testable transform.
package scoring

import (
	"regexp"
)

// RiskTier is the qualitative, non-numeric risk classification an action
// carries. Order matters for thresholds and monotone modifiers: see rank.
type RiskTier string

const (
	// Safe is reversible, local, ephemeral, no side effect.
	Safe RiskTier = "safe"
	// Normal is reversible or easily recoverable.
	Normal RiskTier = "normal"
	// Risky is irreversible OR broad blast radius OR persistent self-modification.
	Risky RiskTier = "risky"
	// Destructive is rm -rf, drop table, force push, send-to-third-party, etc.
	Destructive RiskTier = "destructive"
)

// TaskArgs is the scheduler-tier input. Built + unit-tested now (D-11) but has
// NO runtime consumer in Phase 8 — the Scheduler (P10) wires it later (D-12).
type TaskArgs struct {
	Kind         string // reminder | agent_job | backup_postgres
	ScheduleKind string // oneoff | daily | every_hour | every_minute | ...
	Silent       bool
	Payload      []byte // raw, scanned for destructive keywords
}

// SkillAction enumerates the skill mutations the Skills system (P11) gates.
type SkillAction string

// The four gated skill mutations; delete is the only irreversible one.
const (
	SkillCreate  SkillAction = "create"
	SkillUpdate  SkillAction = "update"
	SkillInstall SkillAction = "install"
	SkillDelete  SkillAction = "delete"
)

// tierOrder is the canonical Safe<Normal<Risky<Destructive sequence; its index
// is each tier's rank. It is the single source of ordering for both monotone
// modifiers and threshold comparison.
var tierOrder = []RiskTier{Safe, Normal, Risky, Destructive}

// rank returns the total ordering of a tier (Safe<Normal<Risky<Destructive).
// An unknown tier sorts at Risky so unclassified input is treated conservatively
// (it gates and saturates upward rather than slipping past as Safe).
func rank(t RiskTier) int {
	for i, known := range tierOrder {
		if known == t {
			return i
		}
	}
	return rank(Risky)
}

// bumpTier raises a tier by exactly one level, saturating at Destructive. It is
// the only modifier primitive, so by construction a modifier can never lower a
// tier (the UP-only invariant, property-tested in TestModifierMonotone).
func bumpTier(t RiskTier) RiskTier {
	next := rank(t) + 1
	if next >= len(tierOrder) {
		return Destructive
	}
	return tierOrder[next]
}

var destructiveKeyword = regexp.MustCompile(`(?i)\b(rm|delete|drop|purge|truncate)\b`)

// ComputeTaskTier classifies a scheduler task from its base kind, then applies
// the UP-only modifier table (D-11; unwired in Phase 8 per D-12). An agent_job
// whose payload matches a destructive keyword jumps straight to Destructive.
func ComputeTaskTier(a TaskArgs) RiskTier {
	tier := baseTaskTier(a)
	for range taskModifierBumps(a) {
		tier = bumpTier(tier)
	}
	return tier
}

// baseTaskTier maps a task kind to its un-modified tier.
func baseTaskTier(a TaskArgs) RiskTier {
	if a.Kind == "agent_job" && destructiveKeyword.Match(a.Payload) {
		return Destructive
	}
	switch a.Kind {
	case "reminder", "backup_postgres":
		return Safe
	default:
		// agent_job and any unrecognised kind default to Normal (parent layer
		// is only a spawn; unknown kinds are treated conservatively, not Safe).
		return Normal
	}
}

// taskModifierBumps counts the UP-only modifier bumps a task accrues. Modifiers
// only raise the tier and saturate at Destructive; none ever lowers it.
func taskModifierBumps(a TaskArgs) int {
	bumps := 0
	switch a.ScheduleKind {
	case "every_minute", "every_hour":
		bumps++
	}
	if a.Silent {
		bumps++
	}
	return bumps
}

// ComputeSkillTier classifies a skill mutation (D-11; unwired in Phase 8). The
// body is reserved for future content-based escalation; today the action alone
// decides the tier, with delete the only Destructive (irreversible) action.
func ComputeSkillTier(action SkillAction, body string) RiskTier {
	_ = body
	switch action {
	case SkillDelete:
		return Destructive
	case SkillCreate, SkillUpdate, SkillInstall:
		return Risky
	default:
		return Risky
	}
}

// GateRecommended reports whether a tier should advise a confirmation gate
// (Risky or Destructive — the structurally vetoable tiers).
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }

// RequiresImmediateAlert reports whether tier meets or exceeds the
// caller-supplied threshold (D-11; unwired in Phase 8). The threshold is an
// argument, never an env read — config owns AURA_RISK_ALERT_THRESHOLD and an
// unknown threshold value falls back to Risky via rank.
func RequiresImmediateAlert(tier, threshold RiskTier) bool {
	return rank(tier) >= rank(threshold)
}
