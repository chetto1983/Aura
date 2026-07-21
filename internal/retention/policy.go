// Package retention owns Aura's bounded storage-lifecycle policy and executor.
package retention

import (
	"fmt"
	"time"
)

// Environment selects the locked full-trace lifetime.
type Environment string

// Class identifies a finite Aura-owned retention class.
type Class string

// Supported environments, data classes, and non-expiring lifetime sentinels.
const (
	EnvironmentProduction         Environment = "production"
	EnvironmentTrustedDevelopment Environment = "trusted_development"

	ClassTemporary          Class = "temporary"
	ClassCrashArtifact      Class = "crash_artifact"
	ClassFullTrace          Class = "full_trace"
	ClassMetadataTrace      Class = "metadata_trace"
	ClassReferencedArtifact Class = "referenced_artifact"
	ClassConversation       Class = "conversation"

	Unlimited          time.Duration = 0
	FollowConversation time.Duration = -1
)

// DiskThresholds defines the monotonic warn/urgent/stop-optional boundaries.
type DiskThresholds struct {
	Warn         int
	Urgent       int
	StopOptional int
}

// DiskState is a signal-only pressure decision; DeleteExtraData is always false.
type DiskState struct {
	Warn               bool
	Urgent             bool
	StopOptionalTraces bool
	DeleteExtraData    bool
}

// Validate enforces strictly increasing percentage thresholds.
func (d DiskThresholds) Validate() error {
	if d.Warn <= 0 || d.Warn >= d.Urgent || d.Urgent >= d.StopOptional || d.StopOptional > 100 {
		return fmt.Errorf("disk thresholds must satisfy 0 < warn < urgent < stop-optional <= 100")
	}
	return nil
}

// Evaluate only raises pressure signals. It never expands the deletion set.
func (d DiskThresholds) Evaluate(usedPercent int) DiskState {
	return DiskState{
		Warn:               usedPercent >= d.Warn,
		Urgent:             usedPercent >= d.Urgent,
		StopOptionalTraces: usedPercent >= d.StopOptional,
		DeleteExtraData:    false,
	}
}

// Policy is the complete versioned retention and execution policy.
type Policy struct {
	Version        string
	Environment    Environment
	TTL            map[Class]time.Duration
	Disk           DiskThresholds
	BatchSize      int
	LeaseDuration  time.Duration
	MaxRunDuration time.Duration
}

// DefaultPolicy returns the D-15 locked policy for environment.
func DefaultPolicy(environment Environment) Policy {
	fullTraceTTL := 24 * time.Hour
	if environment == EnvironmentTrustedDevelopment {
		fullTraceTTL = 7 * 24 * time.Hour
	}
	return Policy{
		Version:     "retention-v1",
		Environment: environment,
		TTL: map[Class]time.Duration{
			ClassTemporary:          24 * time.Hour,
			ClassCrashArtifact:      24 * time.Hour,
			ClassFullTrace:          fullTraceTTL,
			ClassMetadataTrace:      14 * 24 * time.Hour,
			ClassReferencedArtifact: FollowConversation,
			ClassConversation:       Unlimited,
		},
		Disk:           DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85},
		BatchSize:      100,
		LeaseDuration:  5 * time.Minute,
		MaxRunDuration: 5 * time.Minute,
	}
}

// Validate rejects incomplete or unsafe policy values.
func (p Policy) Validate() error {
	if p.Version == "" {
		return fmt.Errorf("policy version is required")
	}
	if p.Environment != EnvironmentProduction && p.Environment != EnvironmentTrustedDevelopment {
		return fmt.Errorf("unsupported environment")
	}
	for _, class := range []Class{ClassTemporary, ClassCrashArtifact, ClassFullTrace, ClassMetadataTrace} {
		if p.TTL[class] <= 0 {
			return fmt.Errorf("%s TTL must be positive", class)
		}
	}
	if p.TTL[ClassConversation] < 0 || p.TTL[ClassReferencedArtifact] != FollowConversation {
		return fmt.Errorf("conversation and referenced-artifact lifetime is invalid")
	}
	if p.BatchSize <= 0 || p.LeaseDuration <= 0 || p.MaxRunDuration <= 0 {
		return fmt.Errorf("retention execution bounds must be positive")
	}
	return p.Disk.Validate()
}
