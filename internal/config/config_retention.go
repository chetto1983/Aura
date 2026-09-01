package config

import (
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
)

const retentionPolicyVersion = "retention-v1"

// RetentionConfig is the typed, versioned storage lifecycle policy loaded at boot.
// ConversationTTL == 0 means unlimited; referenced artifacts follow that lifetime.
type RetentionConfig struct {
	PolicyVersion                  string
	TemporaryTTL                   time.Duration
	ProductionFullTraceTTL         time.Duration
	TrustedDevelopmentFullTraceTTL time.Duration
	MetadataTraceTTL               time.Duration
	ConversationTTL                time.Duration
	ReasoningSuccessTTL            time.Duration
	ReasoningFailedTTL             time.Duration
	BatchSize                      int
	LeaseDuration                  time.Duration
	MaxDuration                    time.Duration
	DiskWarnPercent                int
	DiskUrgentPercent              int
	DiskStopOptionalPercent        int
}

// ReasoningTTL returns the terminal retention class for a persisted status.
func (c RetentionConfig) ReasoningTTL(string) (time.Duration, error) {
	return 0, fmt.Errorf("reasoning retention policy not implemented")
}

func loadRetentionConfig(_ RuntimeProfile) RetentionConfig {
	return RetentionConfig{
		PolicyVersion:                  retentionPolicyVersion,
		TemporaryTTL:                   hours("AURA_RETENTION_TEMP_HOURS", 24),
		ProductionFullTraceTTL:         hours("AURA_RETENTION_PROD_TRACE_HOURS", 24),
		TrustedDevelopmentFullTraceTTL: hours("AURA_RETENTION_DEV_TRACE_HOURS", 7*24),
		MetadataTraceTTL:               hours("AURA_RETENTION_METADATA_TRACE_HOURS", 14*24),
		ConversationTTL:                days("AURA_RETENTION_CONVERSATION_DAYS", 0),
		BatchSize:                      envutil.IntDefault("AURA_RETENTION_BATCH_SIZE", 100),
		LeaseDuration:                  seconds("AURA_RETENTION_LEASE_SEC", 300),
		MaxDuration:                    seconds("AURA_RETENTION_MAX_DURATION_SEC", 300),
		DiskWarnPercent:                envutil.IntDefault("AURA_RETENTION_DISK_WARN_PERCENT", 70),
		DiskUrgentPercent:              envutil.IntDefault("AURA_RETENTION_DISK_URGENT_PERCENT", 80),
		DiskStopOptionalPercent:        envutil.IntDefault("AURA_RETENTION_DISK_STOP_PERCENT", 85),
	}
}

func hours(name string, fallback int) time.Duration {
	return time.Duration(envutil.IntDefault(name, fallback)) * time.Hour
}

func days(name string, fallback int) time.Duration {
	return time.Duration(envutil.IntDefault(name, fallback)) * 24 * time.Hour
}

func seconds(name string, fallback int) time.Duration {
	return time.Duration(envutil.IntDefault(name, fallback)) * time.Second
}

// ActiveFullTraceTTL resolves the only environment-dependent retention default.
func (c RetentionConfig) ActiveFullTraceTTL(profile RuntimeProfile) time.Duration {
	if profile == ProfileDev || profile == ProfileLocalTrusted || profile == "" {
		return c.TrustedDevelopmentFullTraceTTL
	}
	return c.ProductionFullTraceTTL
}

// Validate rejects unsafe or internally contradictory lifecycle settings.
func (c RetentionConfig) Validate() error {
	switch {
	case c.PolicyVersion == "":
		return fmt.Errorf("policy version is required")
	case c.TemporaryTTL <= 0:
		return fmt.Errorf("temporary TTL must be positive")
	case c.ProductionFullTraceTTL <= 0:
		return fmt.Errorf("production trace TTL must be positive")
	case c.TrustedDevelopmentFullTraceTTL <= 0:
		return fmt.Errorf("development trace TTL must be positive")
	case c.MetadataTraceTTL <= 0:
		return fmt.Errorf("metadata trace TTL must be positive")
	case c.ConversationTTL < 0:
		return fmt.Errorf("conversation TTL cannot be negative")
	case c.BatchSize <= 0:
		return fmt.Errorf("batch size must be positive")
	case c.LeaseDuration <= 0:
		return fmt.Errorf("lease duration must be positive")
	case c.MaxDuration <= 0:
		return fmt.Errorf("max duration must be positive")
	case c.DiskWarnPercent <= 0 || c.DiskWarnPercent >= c.DiskUrgentPercent ||
		c.DiskUrgentPercent >= c.DiskStopOptionalPercent || c.DiskStopOptionalPercent > 100:
		return fmt.Errorf("disk thresholds must satisfy 0 < warn < urgent < stop-optional <= 100")
	default:
		return nil
	}
}
