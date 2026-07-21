package config

import (
	"reflect"
	"testing"
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
