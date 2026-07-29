package scoring

import (
	"testing"

	"pgregory.net/rapid"
)

// TestScoring is the exhaustive table for the pure risk-tier module: one block
// per Compute*Tier plus GateRecommended / RequiresImmediateAlert, mirroring the
// internal/web/ssrf_test.go TestBlocked_Classification shape. The module is
// pure (no DB/IO/env) so the whole unit tier runs here with no build tag.
func TestScoring(t *testing.T) {
	t.Parallel()

	t.Run("ComputeTaskTier", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			args TaskArgs
			want RiskTier
		}{
			{"reminder = Safe", TaskArgs{Kind: "reminder", ScheduleKind: "oneoff"}, Safe},
			{"backup_postgres = Safe", TaskArgs{Kind: "backup_postgres", ScheduleKind: "daily"}, Safe},
			{"backup_neo4j = Safe", TaskArgs{Kind: "backup_neo4j", ScheduleKind: "daily"}, Safe},
			{"agent_job = Normal", TaskArgs{Kind: "agent_job", ScheduleKind: "daily"}, Normal},
			{"unknown kind = Normal", TaskArgs{Kind: "mystery", ScheduleKind: "daily"}, Normal},
			{"agent_job destructive payload = Destructive", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", Payload: []byte("please rm -rf /workspace")}, Destructive},
			{"agent_job drop keyword = Destructive", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", Payload: []byte("drop table users")}, Destructive},
			{"agent_job truncate keyword = Destructive", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", Payload: []byte("TRUNCATE logs")}, Destructive},
			{"reminder destructive payload not agent_job = Safe", TaskArgs{Kind: "reminder", ScheduleKind: "oneoff", Payload: []byte("rm everything")}, Safe},
			// modifier rows (UP-only, saturate at Destructive)
			{"reminder every_minute = Normal (+1)", TaskArgs{Kind: "reminder", ScheduleKind: "every_minute"}, Normal},
			{"reminder every_hour = Normal (+1)", TaskArgs{Kind: "reminder", ScheduleKind: "every_hour"}, Normal},
			{"agent_job every_minute = Risky (+1)", TaskArgs{Kind: "agent_job", ScheduleKind: "every_minute"}, Risky},
			{"agent_job silent = Risky (+1)", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", Silent: true}, Risky},
			{"agent_job reasoning tier = Risky (+1)", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", AgentTier: "reasoning"}, Risky},
			{"agent_job worker tier = Normal (no bump)", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", AgentTier: "worker"}, Normal},
			{"agent_job chat tier = Normal (no bump)", TaskArgs{Kind: "agent_job", ScheduleKind: "daily", AgentTier: "chat"}, Normal},
			{"agent_job silent + every_minute saturates to Destructive", TaskArgs{Kind: "agent_job", ScheduleKind: "every_minute", Silent: true, AgentTier: "reasoning"}, Destructive},
			{"agent_job destructive payload + modifiers stays Destructive", TaskArgs{Kind: "agent_job", ScheduleKind: "every_minute", Silent: true, Payload: []byte("rm -rf")}, Destructive},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := ComputeTaskTier(tc.args); got != tc.want {
					t.Fatalf("ComputeTaskTier(%+v) = %q, want %q", tc.args, got, tc.want)
				}
			})
		}
	})

	t.Run("ComputeSkillTier", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name   string
			action SkillAction
			body   string
			want   RiskTier
		}{
			{"create = Risky", SkillCreate, "echo hi", Risky},
			{"update = Risky", SkillUpdate, "echo hi", Risky},
			{"install = Risky", SkillInstall, "pip install x", Risky},
			{"delete = Destructive", SkillDelete, "", Destructive},
			{"unknown action = Risky (conservative)", SkillAction("mystery"), "", Risky},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := ComputeSkillTier(tc.action, tc.body); got != tc.want {
					t.Fatalf("ComputeSkillTier(%q, %q) = %q, want %q", tc.action, tc.body, got, tc.want)
				}
			})
		}
	})

	t.Run("GateRecommended", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			tier RiskTier
			want bool
		}{
			{Safe, false},
			{Normal, false},
			{Risky, true},
			{Destructive, true},
		}
		for _, tc := range cases {
			t.Run(string(tc.tier), func(t *testing.T) {
				t.Parallel()
				if got := GateRecommended(tc.tier); got != tc.want {
					t.Fatalf("GateRecommended(%q) = %v, want %v", tc.tier, got, tc.want)
				}
			})
		}
	})

	t.Run("RequiresImmediateAlert", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name      string
			tier      RiskTier
			threshold RiskTier
			want      bool
		}{
			{"safe threshold alerts on safe", Safe, Safe, true},
			{"safe threshold alerts on destructive", Destructive, Safe, true},
			{"risky threshold no alert on normal", Normal, Risky, false},
			{"risky threshold alerts on risky", Risky, Risky, true},
			{"risky threshold alerts on destructive", Destructive, Risky, true},
			{"destructive threshold no alert on risky", Risky, Destructive, false},
			{"destructive threshold alerts on destructive", Destructive, Destructive, true},
			{"normal threshold no alert on safe", Safe, Normal, false},
			{"unknown threshold treated as risky default - no alert on normal", Normal, RiskTier("garbage"), false},
			{"unknown threshold treated as risky default - alerts on risky", Risky, RiskTier("garbage"), true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := RequiresImmediateAlert(tc.tier, tc.threshold); got != tc.want {
					t.Fatalf("RequiresImmediateAlert(%q, %q) = %v, want %v", tc.tier, tc.threshold, got, tc.want)
				}
			})
		}
	})
}

// TestModifierMonotone is the property test: applying any modifier bump to any
// base tier yields a tier >= the base — the modifier table is UP-only and never
// lowers a tier (RESEARCH Validation map "modifiers monotone (never down)"). It
// also asserts saturation never exceeds Destructive.
func TestModifierMonotone(t *testing.T) {
	t.Parallel()
	tiers := []RiskTier{Safe, Normal, Risky, Destructive}
	rapid.Check(t, func(rt *rapid.T) {
		base := tiers[rapid.IntRange(0, len(tiers)-1).Draw(rt, "base")]
		bumps := rapid.IntRange(0, 5).Draw(rt, "bumps")
		got := applyBumps(base, bumps)
		if rank(got) < rank(base) {
			rt.Fatalf("applyBumps(%q, %d) = %q lowered the tier (rank %d < %d)", base, bumps, got, rank(got), rank(base))
		}
		if rank(got) > rank(Destructive) {
			rt.Fatalf("applyBumps(%q, %d) = %q exceeded Destructive saturation", base, bumps, got)
		}
		if bumps > 0 && rank(base) < rank(Destructive) && rank(got) <= rank(base) {
			rt.Fatalf("applyBumps(%q, %d) = %q did not raise a non-saturated tier", base, bumps, got)
		}
	})
}

// applyBumps is a test-only helper exercising the UP-only modifier seam N times.
func applyBumps(t RiskTier, n int) RiskTier {
	for range n {
		t = bumpTier(t)
	}
	return t
}
