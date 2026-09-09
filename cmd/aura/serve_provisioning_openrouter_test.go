package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// serve_provisioning_openrouter_test.go proves the plan 02-06 composition-root wiring:
// both saga ports are non-nil in a real boot (management credential + pool present),
// both nil when the credential is absent, and a boot without the credential logs one
// INFO line naming what is degraded rather than failing closed.

// openRouterManagementConfiguredCfg is a config with the pool-independent fields
// openRouterKeyMinterFor/openRouterKeyRevokerFor need: the management credential and a
// valid AURA_AUTHULA_SECRET (identitykey.NewStore's own KEK derivation requirement).
func openRouterManagementConfiguredCfg() *config.Config {
	return &config.Config{
		AuthulaSecret:           validProvisioningAuthulaSecret,
		OpenRouterManagementKey: "sk-or-mgmt-test-credential",
	}
}

// TestProvisionerForBuildsNonNilMinterWhenConfigured proves openRouterKeyMinterFor
// yields a non-nil port when the pool + credential are both present.
func TestProvisionerForBuildsNonNilMinterWhenConfigured(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: openRouterManagementConfiguredCfg()}
	if minter := openRouterKeyMinterFor(chat); minter == nil {
		t.Fatal("openRouterKeyMinterFor: want non-nil port when the management credential and pool are present")
	}
}

// TestRevokerForBuildsNonNilRevokerWhenConfigured is TestProvisionerFor's mirror for
// the reverse-saga port.
func TestRevokerForBuildsNonNilRevokerWhenConfigured(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: openRouterManagementConfiguredCfg()}
	if revoker := openRouterKeyRevokerFor(chat); revoker == nil {
		t.Fatal("openRouterKeyRevokerFor: want non-nil port when the management credential and pool are present")
	}
}

// TestProvisionerForNilWhenCredentialAbsent proves both constructors degrade to nil —
// never a boot-fatal error — when the management credential is unset, matching D-13's
// local-backend-deployment case.
func TestProvisionerForNilWhenCredentialAbsent(t *testing.T) {
	cfg := &config.Config{AuthulaSecret: validProvisioningAuthulaSecret} // no OpenRouterManagementKey
	chat := &chatEnv{pool: newLazyPool(t), cfg: cfg}
	if minter := openRouterKeyMinterFor(chat); minter != nil {
		t.Fatal("openRouterKeyMinterFor: want nil when the management credential is absent")
	}
	if revoker := openRouterKeyRevokerFor(chat); revoker != nil {
		t.Fatal("openRouterKeyRevokerFor: want nil when the management credential is absent")
	}
	// Nil chat / nil pool degrade the same way.
	if minter := openRouterKeyMinterFor(nil); minter != nil {
		t.Fatal("openRouterKeyMinterFor(nil): want nil")
	}
	if revoker := openRouterKeyRevokerFor(&chatEnv{cfg: openRouterManagementConfiguredCfg()}); revoker != nil {
		t.Fatal("openRouterKeyRevokerFor with a nil pool: want nil")
	}
}

// TestOpenRouterManagementKeyAbsentLogsDegradedBoot proves a boot with the management
// credential absent logs one INFO line naming what is degraded and does not panic —
// asserted on the log line itself, not merely on the absence of a crash.
func TestOpenRouterManagementKeyAbsentLogsDegradedBoot(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	chat := &chatEnv{pool: newLazyPool(t), cfg: &config.Config{AuthulaSecret: validProvisioningAuthulaSecret}}
	if minter := openRouterKeyMinterFor(chat); minter != nil {
		t.Fatal("openRouterKeyMinterFor: want nil when the management credential is absent")
	}

	out := buf.String()
	if !strings.Contains(out, "level=INFO") {
		t.Fatalf("boot without the management credential did not log at INFO level:\n%s", out)
	}
	if !strings.Contains(out, "AURA_OPENROUTER_MANAGEMENT_KEY") {
		t.Fatalf("boot log did not name the missing credential:\n%s", out)
	}
	if !strings.Contains(out, "no per-identity OpenRouter keys will be minted or revoked") {
		t.Fatalf("boot log did not name what is degraded:\n%s", out)
	}
}
