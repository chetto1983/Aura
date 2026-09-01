package config

import (
	"reflect"
	"testing"
	"time"
)

func TestRetentionConfigContractIsRegistered(t *testing.T) {
	t.Helper()
	field, ok := reflect.TypeFor[Config]().FieldByName("Retention")
	if !ok {
		t.Fatal("Config.Retention is missing")
	}
	if field.Type.Name() != "RetentionConfig" {
		t.Fatalf("Config.Retention type = %s, want RetentionConfig", field.Type)
	}

	want := map[string]string{
		"AURA_RETENTION_TEMP_HOURS":             "24",
		"AURA_RETENTION_PROD_TRACE_HOURS":       "24",
		"AURA_RETENTION_DEV_TRACE_HOURS":        "168",
		"AURA_RETENTION_METADATA_TRACE_HOURS":   "336",
		"AURA_RETENTION_CONVERSATION_DAYS":      "0",
		"AURA_RETENTION_REASONING_SUCCESS_DAYS": "30",
		"AURA_RETENTION_REASONING_FAILED_DAYS":  "7",
		"AURA_RETENTION_DISK_WARN_PERCENT":      "70",
		"AURA_RETENTION_DISK_URGENT_PERCENT":    "80",
		"AURA_RETENTION_DISK_STOP_PERCENT":      "85",
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
		"AURA_RETENTION_REASONING_SUCCESS_DAYS", "AURA_RETENTION_REASONING_FAILED_DAYS",
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

func TestReasoningRetentionPolicy(t *testing.T) {
	cfg := loadRetentionConfig(ProfileDev)
	if cfg.ReasoningSuccessTTL != 30*24*time.Hour {
		t.Fatalf("reasoning success TTL = %s, want 30 days", cfg.ReasoningSuccessTTL)
	}
	if cfg.ReasoningFailedTTL != 7*24*time.Hour {
		t.Fatalf("reasoning failed TTL = %s, want 7 days", cfg.ReasoningFailedTTL)
	}
	for _, test := range []struct {
		status string
		want   time.Duration
	}{
		{status: "succeeded", want: 30 * 24 * time.Hour},
		{status: "failed", want: 7 * 24 * time.Hour},
		{status: "cancelled", want: 7 * 24 * time.Hour},
	} {
		got, err := cfg.ReasoningTTL(test.status)
		if err != nil || got != test.want {
			t.Errorf("ReasoningTTL(%q) = %s, %v; want %s", test.status, got, err, test.want)
		}
	}
	if _, err := cfg.ReasoningTTL("running"); err == nil {
		t.Fatal("non-terminal reasoning status received a TTL")
	}

	for _, test := range []struct {
		name string
		edit func(*RetentionConfig)
	}{
		{name: "zero success", edit: func(c *RetentionConfig) { c.ReasoningSuccessTTL = 0 }},
		{name: "negative success", edit: func(c *RetentionConfig) { c.ReasoningSuccessTTL = -time.Hour }},
		{name: "zero failed", edit: func(c *RetentionConfig) { c.ReasoningFailedTTL = 0 }},
		{name: "negative failed", edit: func(c *RetentionConfig) { c.ReasoningFailedTTL = -time.Hour }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := cfg
			test.edit(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid reasoning retention")
			}
		})
	}
}

func TestReasoningRetentionPolicyOverrides(t *testing.T) {
	t.Setenv("AURA_RETENTION_REASONING_SUCCESS_DAYS", "21")
	t.Setenv("AURA_RETENTION_REASONING_FAILED_DAYS", "5")
	cfg := loadRetentionConfig(ProfileDev)
	if cfg.ReasoningSuccessTTL != 21*24*time.Hour || cfg.ReasoningFailedTTL != 5*24*time.Hour {
		t.Fatalf("reasoning override TTLs = %s/%s, want 21d/5d",
			cfg.ReasoningSuccessTTL, cfg.ReasoningFailedTTL)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid shorter overrides: %v", err)
	}

	for _, test := range []struct {
		name    string
		success time.Duration
		failed  time.Duration
	}{
		{name: "success widens class", success: 31 * 24 * time.Hour, failed: 7 * 24 * time.Hour},
		{name: "failed widens class", success: 30 * 24 * time.Hour, failed: 8 * 24 * time.Hour},
		{name: "failed outlives success", success: 5 * 24 * time.Hour, failed: 6 * 24 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := cfg
			invalid.ReasoningSuccessTTL = test.success
			invalid.ReasoningFailedTTL = test.failed
			if err := invalid.Validate(); err == nil {
				t.Fatal("Validate() accepted a widening reasoning override")
			}
		})
	}
}
