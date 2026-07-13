package config

import (
	"errors"
	"hash/fnv"
	"strconv"
	"strings"
)

// ErrInvalidCompactionConfig rejects unsafe rollout combinations.
var ErrInvalidCompactionConfig = errors.New("invalid compaction configuration")

// CompactionMode is the closed rollout-stage vocabulary.
type CompactionMode string

const (
	// CompactionDisabled is the default and performs no semantic work.
	CompactionDisabled CompactionMode = "disabled"
	// CompactionShadow evaluates without activation.
	CompactionShadow CompactionMode = "shadow"
	// CompactionCanary activates a deterministic percentage cohort.
	CompactionCanary CompactionMode = "canary"
	// CompactionEnabled activates every eligible conversation after promotion.
	CompactionEnabled CompactionMode = "enabled"
)

// CompactionConfig controls deterministic rollout and recovery gating.
type CompactionConfig struct {
	Mode                CompactionMode
	Percent             int
	RecoveryDrillPassed bool
}

// ParseCompactionConfig applies disabled defaults and strict integer parsing.
func ParseCompactionConfig(mode, percent string) (CompactionConfig, error) {
	mode = strings.TrimSpace(mode)
	percent = strings.TrimSpace(percent)
	if mode == "" {
		mode = string(CompactionDisabled)
	}
	p := 0
	if percent != "" {
		var err error
		p, err = strconv.Atoi(percent)
		if err != nil {
			return CompactionConfig{}, ErrInvalidCompactionConfig
		}
	}
	cfg := CompactionConfig{Mode: CompactionMode(mode), Percent: p}
	if !cfg.validStage() {
		return CompactionConfig{}, ErrInvalidCompactionConfig
	}
	return cfg, nil
}

func (c CompactionConfig) validStage() bool {
	switch c.Mode {
	case CompactionDisabled, CompactionShadow:
		return c.Percent == 0
	case CompactionCanary:
		return c.Percent == 1 || c.Percent == 5 || c.Percent == 20 || c.Percent == 50
	case CompactionEnabled:
		return c.Percent == 100
	default:
		return false
	}
}

// Validate additionally requires a successful recovery drill before activation.
func (c CompactionConfig) Validate() error {
	if !c.validStage() || (c.Mode == CompactionCanary || c.Mode == CompactionEnabled) && !c.RecoveryDrillPassed {
		return ErrInvalidCompactionConfig
	}
	return nil
}

// Selected deterministically hashes tenant and conversation into a stable cohort.
func (c CompactionConfig) Selected(tenantID, conversationID string) bool {
	if c.Mode == CompactionEnabled && c.Percent == 100 && c.RecoveryDrillPassed {
		return true
	}
	if c.Mode != CompactionCanary || !c.RecoveryDrillPassed || tenantID == "" || conversationID == "" {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(tenantID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(conversationID))
	return int(h.Sum32()%100) < c.Percent
}
