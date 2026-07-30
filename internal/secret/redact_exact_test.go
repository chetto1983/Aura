package secret

import (
	"strings"
	"testing"
)

func TestRedactExact(t *testing.T) {
	configuredDSN := "postgres://aura:configured-password@db.internal/aura"
	configuredToken := "aura-configured-token-0123456789"
	discoveredDSN := "postgres://agent:discovered-password@remote.example/data"
	redactor := NewExactRedactor([]string{
		"AURA_DB_URL=" + configuredDSN,
		"AURA_SERVICE_TOKEN=" + configuredToken,
		"AURA_SHORT_TOKEN=tiny",
		"AURA_PUBLIC_LABEL=ordinary",
		"MALFORMED_ENTRY",
	})

	input := "db=" + configuredDSN + " token=" + configuredToken + " discovered=" + discoveredDSN + " tiny=tiny"
	got := redactor.Redact(input)

	for _, configured := range []string{configuredDSN, configuredToken} {
		if strings.Contains(got, configured) {
			t.Fatalf("configured secret survived exact redaction: %q", got)
		}
	}
	if count := strings.Count(got, ConfiguredValuePlaceholder); count != 2 {
		t.Fatalf("placeholder count = %d, want 2 in %q", count, got)
	}
	if !strings.Contains(got, discoveredDSN) {
		t.Fatalf("agent-discovered DSN was over-redacted: %q", got)
	}
	if !strings.Contains(got, "tiny=tiny") {
		t.Fatalf("below-floor value was over-redacted: %q", got)
	}
	if empty := redactor.Redact(""); empty != "" {
		t.Fatalf("Redact(empty) = %q", empty)
	}
	if plain := redactor.Redact("ordinary working data"); plain != "ordinary working data" {
		t.Fatalf("Redact(plain) = %q", plain)
	}
}
