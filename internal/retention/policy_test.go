package retention

import (
	"testing"
	"time"
)

func TestDefaultPolicyLockedLifetimes(t *testing.T) {
	production := DefaultPolicy(EnvironmentProduction)
	trusted := DefaultPolicy(EnvironmentTrustedDevelopment)

	for _, policy := range []Policy{production, trusted} {
		if policy.TTL[ClassTemporary] != 24*time.Hour || policy.TTL[ClassCrashArtifact] != 24*time.Hour {
			t.Fatalf("temporary/crash TTLs = %+v", policy.TTL)
		}
		if policy.TTL[ClassMetadataTrace] != 14*24*time.Hour {
			t.Fatalf("metadata trace TTL = %s", policy.TTL[ClassMetadataTrace])
		}
		if policy.TTL[ClassConversation] != Unlimited || policy.TTL[ClassReferencedArtifact] != FollowConversation {
			t.Fatalf("conversation/artifact policy = %+v", policy.TTL)
		}
		if err := policy.Validate(); err != nil {
			t.Fatalf("default policy: %v", err)
		}
	}
	if production.TTL[ClassFullTrace] != 24*time.Hour {
		t.Fatalf("production full trace TTL = %s", production.TTL[ClassFullTrace])
	}
	if trusted.TTL[ClassFullTrace] != 7*24*time.Hour {
		t.Fatalf("trusted full trace TTL = %s", trusted.TTL[ClassFullTrace])
	}
}

func TestDiskStateExactBoundaries(t *testing.T) {
	thresholds := DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85}
	for _, tc := range []struct {
		used                  int
		warn, urgent, stopOpt bool
	}{
		{69, false, false, false},
		{70, true, false, false},
		{80, true, true, false},
		{85, true, true, true},
		{99, true, true, true},
	} {
		got := thresholds.Evaluate(tc.used)
		if got.Warn != tc.warn || got.Urgent != tc.urgent || got.StopOptionalTraces != tc.stopOpt {
			t.Errorf("Evaluate(%d) = %+v", tc.used, got)
		}
		if got.DeleteExtraData {
			t.Errorf("Evaluate(%d) selected emergency deletion", tc.used)
		}
	}
}

func TestPolicyValidationRejectsUnsafeValues(t *testing.T) {
	base := DefaultPolicy(EnvironmentProduction)
	cases := []struct {
		name   string
		mutate func(*Policy)
	}{
		{"missing version", func(p *Policy) { p.Version = "" }},
		{"unknown environment", func(p *Policy) { p.Environment = "staging" }},
		{"missing temporary TTL", func(p *Policy) { p.TTL[ClassTemporary] = 0 }},
		{"negative conversation TTL", func(p *Policy) { p.TTL[ClassConversation] = -time.Hour }},
		{"referenced artifact mismatch", func(p *Policy) { p.TTL[ClassReferencedArtifact] = time.Hour }},
		{"zero batch", func(p *Policy) { p.BatchSize = 0 }},
		{"zero lease", func(p *Policy) { p.LeaseDuration = 0 }},
		{"zero run duration", func(p *Policy) { p.MaxRunDuration = 0 }},
		{"invalid disk order", func(p *Policy) { p.Disk.Urgent = p.Disk.Warn }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := base
			policy.TTL = map[Class]time.Duration{}
			for class, ttl := range base.TTL {
				policy.TTL[class] = ttl
			}
			tc.mutate(&policy)
			if err := policy.Validate(); err == nil {
				t.Fatal("unsafe policy accepted")
			}
		})
	}
	for _, thresholds := range []DiskThresholds{{}, {Warn: 70, Urgent: 80, StopOptional: 101}} {
		if err := thresholds.Validate(); err == nil {
			t.Fatalf("unsafe thresholds accepted: %+v", thresholds)
		}
	}
}
