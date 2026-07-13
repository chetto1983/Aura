package config

import (
	"context"
	"errors"
	"testing"
)

type effectiveSourceFixture struct {
	state PersistedCompactionState
	err   error
}

func (f effectiveSourceFixture) LoadEffectiveCompaction(context.Context, string) (PersistedCompactionState, error) {
	return f.state, f.err
}

func TestEffectiveSnapshotFailsClosedAndPreservesVersion(t *testing.T) {
	r := PersistedCompactionReader{Source: effectiveSourceFixture{state: PersistedCompactionState{ScopeID: "scope", Version: 7, ActiveConfig: []byte(`{"mode":"canary","percent":5,"recovery_drill_passed":true}`)}}, ScopeID: "scope"}
	s, err := r.Read(t.Context())
	if err != nil || s.Version != 7 || s.Config.Percent != 5 {
		t.Fatalf("snapshot=%+v err=%v", s, err)
	}
	r.Source = effectiveSourceFixture{err: errors.New("db unavailable")}
	if _, err = r.Read(t.Context()); err == nil {
		t.Fatal("unavailable durable state did not fail closed")
	}
}

func TestCompactionConfigDefaultsDisabled(t *testing.T) {
	cfg, err := ParseCompactionConfig("", "")
	if err != nil || cfg.Mode != CompactionDisabled || cfg.Percent != 0 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestCanaryDeterministic(t *testing.T) {
	cfg := CompactionConfig{Mode: CompactionCanary, Percent: 20, RecoveryDrillPassed: true}
	a := cfg.Selected("tenant-a", "conversation-a")
	for range 20 {
		if cfg.Selected("tenant-a", "conversation-a") != a {
			t.Fatal("sampling changed")
		}
	}
}

func TestCompactionConfigRejectsIllegalOrIncompatibleActivation(t *testing.T) {
	for _, tc := range []struct{ mode, percent string }{{"canary", "2"}, {"disabled", "5"}, {"enabled", "50"}, {"shadow", "1"}} {
		if _, err := ParseCompactionConfig(tc.mode, tc.percent); err == nil {
			t.Fatalf("accepted %q/%q", tc.mode, tc.percent)
		}
	}
	if err := (CompactionConfig{Mode: CompactionCanary, Percent: 5}).Validate(); err == nil {
		t.Fatal("canary activation without recovery drill passed")
	}
}
