package config

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"strconv"
	"strings"
)

// PersistedCompactionState is the locale-neutral durable state projection consumed at runtime.
type PersistedCompactionState struct {
	ScopeID      string
	Version      int64
	ActiveConfig []byte
}

// CompactionEffectiveSource loads authoritative rollout state from shared storage.
type CompactionEffectiveSource interface {
	LoadEffectiveCompaction(context.Context, string) (PersistedCompactionState, error)
}

// EffectiveCompactionSnapshot fences one claim/finalization against a durable version.
type EffectiveCompactionSnapshot struct {
	ScopeID string
	Version int64
	Config  CompactionConfig
}

// PersistedCompactionReader validates the durable JSON snapshot on every read.
type PersistedCompactionReader struct {
	Source  CompactionEffectiveSource
	ScopeID string
}

// Read returns no static-config fallback: unavailable or malformed durable state fails closed.
func (r PersistedCompactionReader) Read(ctx context.Context) (EffectiveCompactionSnapshot, error) {
	if r.Source == nil || r.ScopeID == "" {
		return EffectiveCompactionSnapshot{}, ErrInvalidCompactionConfig
	}
	s, err := r.Source.LoadEffectiveCompaction(ctx, r.ScopeID)
	if err != nil {
		return EffectiveCompactionSnapshot{}, err
	}
	var raw struct {
		Mode                string `json:"mode"`
		Percent             int    `json:"percent"`
		RecoveryDrillPassed bool   `json:"recovery_drill_passed"`
	}
	if s.ScopeID != r.ScopeID || s.Version < 1 || json.Unmarshal(s.ActiveConfig, &raw) != nil {
		return EffectiveCompactionSnapshot{}, ErrInvalidCompactionConfig
	}
	cfg := CompactionConfig{Mode: CompactionMode(raw.Mode), Percent: raw.Percent, RecoveryDrillPassed: raw.RecoveryDrillPassed}
	if err := cfg.Validate(); err != nil {
		return EffectiveCompactionSnapshot{}, err
	}
	return EffectiveCompactionSnapshot{ScopeID: s.ScopeID, Version: s.Version, Config: cfg}, nil
}

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
