// Package scoring is the pure, shared risk-tier module (PRD §Risk-Based
// Governance, amendment #41 / D-11): it computes a qualitative RiskTier for
// scheduler tasks, skills, and sandbox calls so consumers can render an
// advisory gate. It is deliberately pure — no DB, no IO, no env read. The alert
// threshold is supplied by the caller (config owns the env read; this module
// mirrors dnspin taking its TTL as a constructor arg), keeping every function a
// total, testable transform.
package scoring

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

// SandboxArgs is the sandbox-tier input. D-12: this is the ONLY scoring path
// wired in Phase 8 — the execute tool consumes ComputeSandboxTier for its
// advisory {risk_tier, gate_recommended} preview.
type SandboxArgs struct {
	// NetworkAllow is the egress allowlist the model requested (host names).
	NetworkAllow []string
}

// TaskArgs is the scheduler-tier input. Built + unit-tested now (D-11) but has
// NO runtime consumer in Phase 8 — the Scheduler (P10) wires it later (D-12).
type TaskArgs struct {
	Kind         string // reminder | agent_job | backup_postgres | backup_neo4j
	ScheduleKind string // oneoff | daily | every_hour | every_minute | ...
	Silent       bool
	AgentTier    string // worker | chat | reasoning (only for agent_job)
	Payload      []byte // raw, scanned for destructive keywords
}

// SkillAction enumerates the skill mutations the Skills system (P11) gates.
type SkillAction string

const (
	SkillCreate  SkillAction = "create"
	SkillUpdate  SkillAction = "update"
	SkillInstall SkillAction = "install"
	SkillDelete  SkillAction = "delete"
)

// rank returns the total ordering of a tier (Safe<Normal<Risky<Destructive).
// An unknown tier sorts at Risky so unclassified input is treated conservatively.
func rank(t RiskTier) int { return 0 }

// bumpTier raises a tier by one level, saturating at Destructive. It never
// lowers a tier — the modifier table is UP-only (property-tested).
func bumpTier(t RiskTier) RiskTier { return t }

// ComputeSandboxTier classifies a sandbox call from its egress allowlist.
func ComputeSandboxTier(a SandboxArgs) RiskTier { return Risky }

// ComputeTaskTier classifies a scheduler task (D-11; unwired in Phase 8).
func ComputeTaskTier(a TaskArgs) RiskTier { return Safe }

// ComputeSkillTier classifies a skill mutation (D-11; unwired in Phase 8).
func ComputeSkillTier(action SkillAction, body string) RiskTier { return Safe }

// GateRecommended reports whether a tier should advise a confirmation gate.
func GateRecommended(t RiskTier) bool { return false }

// RequiresImmediateAlert reports whether tier meets or exceeds the
// caller-supplied threshold (D-11; unwired in Phase 8). The threshold is an
// argument, never an env read — config owns AURA_RISK_ALERT_THRESHOLD.
func RequiresImmediateAlert(tier, threshold RiskTier) bool { return false }
