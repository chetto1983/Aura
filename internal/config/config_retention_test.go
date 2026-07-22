package config

import (
	"reflect"
	"testing"
	"time"
)

func TestRetentionConfigContractIsRegistered(t *testing.T) {
	t.Helper()
	field, ok := reflect.TypeOf(Config{}).FieldByName("Retention")
	if !ok {
		t.Fatal("Config.Retention is missing")
	}
	if field.Type.Name() != "RetentionConfig" {
		t.Fatalf("Config.Retention type = %s, want RetentionConfig", field.Type)
	}

	want := map[string]string{
		"AURA_RETENTION_TEMP_HOURS":           "24",
		"AURA_RETENTION_PROD_TRACE_HOURS":     "24",
		"AURA_RETENTION_DEV_TRACE_HOURS":      "168",
		"AURA_RETENTION_METADATA_TRACE_HOURS": "336",
		"AURA_RETENTION_CONVERSATION_DAYS":    "0",
		"AURA_RETENTION_DISK_WARN_PERCENT":    "70",
		"AURA_RETENTION_DISK_URGENT_PERCENT":  "80",
		"AURA_RETENTION_DISK_STOP_PERCENT":    "85",
	}
	got := make(map[string]string)
	for _, spec := range knobRegistry() {
		if _, expected := want[spec.Name]; expected {
			got[spec.Name] = spec.Default
		}
	}
	for name, defaultValue := range want {
		if got[name] != defaultValue {
			t.Errorf("knob %s default = %q, want %q", name, got[name], defaultValue)
		}
	}
}

func TestRetentionConfigDefaultsAndEnvironment(t *testing.T) {
	for _, name := range []string{
		"AURA_RETENTION_TEMP_HOURS", "AURA_RETENTION_PROD_TRACE_HOURS",
		"AURA_RETENTION_DEV_TRACE_HOURS", "AURA_RETENTION_METADATA_TRACE_HOURS",
		"AURA_RETENTION_CONVERSATION_DAYS", "AURA_RETENTION_BATCH_SIZE",
		"AURA_RETENTION_LEASE_SEC", "AURA_RETENTION_MAX_DURATION_SEC",
		"AURA_RETENTION_DISK_WARN_PERCENT", "AURA_RETENTION_DISK_URGENT_PERCENT",
		"AURA_RETENTION_DISK_STOP_PERCENT",
	} {
		t.Setenv(name, "")
	}
	cfg := LoadDB().Retention
	if cfg.TemporaryTTL != 24*time.Hour || cfg.ProductionFullTraceTTL != 24*time.Hour ||
		cfg.TrustedDevelopmentFullTraceTTL != 7*24*time.Hour || cfg.MetadataTraceTTL != 14*24*time.Hour {
		t.Fatalf("retention TTL defaults = %+v", cfg)
	}
	if cfg.ConversationTTL != 0 {
		t.Fatalf("conversation TTL = %s, want unlimited", cfg.ConversationTTL)
	}
	if cfg.DiskWarnPercent != 70 || cfg.DiskUrgentPercent != 80 || cfg.DiskStopOptionalPercent != 85 {
		t.Fatalf("disk thresholds = %d/%d/%d", cfg.DiskWarnPercent, cfg.DiskUrgentPercent, cfg.DiskStopOptionalPercent)
	}
	if got := cfg.ActiveFullTraceTTL(ProfileLocalTrusted); got != 7*24*time.Hour {
		t.Fatalf("trusted-development trace TTL = %s", got)
	}
	if got := cfg.ActiveFullTraceTTL(ProfileServerProduction); got != 24*time.Hour {
		t.Fatalf("production trace TTL = %s", got)
	}
}

func TestRetentionConfigValidationBoundaries(t *testing.T) {
	valid := loadRetentionConfig(ProfileDev)
	for _, tc := range []struct {
		name string
		edit func(*RetentionConfig)
	}{
		{"warn zero", func(c *RetentionConfig) { c.DiskWarnPercent = 0 }},
		{"warn equals urgent", func(c *RetentionConfig) { c.DiskWarnPercent = c.DiskUrgentPercent }},
		{"urgent equals stop", func(c *RetentionConfig) { c.DiskUrgentPercent = c.DiskStopOptionalPercent }},
		{"stop above one hundred", func(c *RetentionConfig) { c.DiskStopOptionalPercent = 101 }},
		{"negative conversation", func(c *RetentionConfig) { c.ConversationTTL = -time.Hour }},
		{"zero batch", func(c *RetentionConfig) { c.BatchSize = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil")
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid defaults: %v", err)
	}
}
