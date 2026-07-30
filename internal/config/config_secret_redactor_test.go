package config

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/secret"
)

func TestLoadDBRefreshesConfiguredSecretSnapshot(t *testing.T) {
	configured := "configured-after-dotenv-load-0123456789"
	t.Setenv("AURA_TEST_BOOT_TOKEN", configured)

	_ = LoadDB()
	got := secret.RedactConfigured("value=" + configured)

	if strings.Contains(got, configured) {
		t.Fatalf("LoadDB left configured secret visible: %q", got)
	}
	if !strings.Contains(got, secret.ConfiguredValuePlaceholder) {
		t.Fatalf("LoadDB redaction missing placeholder: %q", got)
	}
}
