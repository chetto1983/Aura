package config

import "testing"

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
