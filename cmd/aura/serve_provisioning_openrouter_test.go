package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// serve_provisioning_openrouter_test.go proves the plan 02-06 composition-root wiring:
// both saga ports are wired whenever the pool and AURA_AUTHULA_SECRET are present, whether
// or not a management key is set yet, and the key is read at call time.

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

// TestOpenRouterPortsAreWiredBeforeTheManagementKeyExists proves the ports no longer hang on a
// key captured at boot: they exist whenever the pool and AURA_AUTHULA_SECRET do.
func TestOpenRouterPortsAreWiredBeforeTheManagementKeyExists(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: &config.Config{AuthulaSecret: validProvisioningAuthulaSecret}}
	if openRouterKeyMinterFor(chat) == nil || openRouterKeyRevokerFor(chat) == nil {
		t.Fatal("ports are nil without a management key; they must be wired and decide at call time")
	}
	if openRouterKeyMinterFor(nil) != nil || openRouterKeyRevokerFor(&chatEnv{cfg: chat.cfg}) != nil {
		t.Fatal("a nil chat or a nil pool must still yield nil ports")
	}
}

func TestOpenRouterKeyConfigRefusesABlankManagementKey(t *testing.T) {
	cfg := openRouterKeyConfig{managementKey: func(context.Context) (string, error) { return "  ", nil }}
	if _, err := cfg.key(context.Background()); !errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		t.Fatalf("key() error = %v, want ErrManagementKeyUnset", err)
	}
}

func TestMintingAdapterWithoutAManagementKey(t *testing.T) {
	adapter := openRouterMintingAdapter{openRouterKeyConfig{managementKey: func(context.Context) (string, error) { return "", nil }}}
	if set, err := adapter.ManagementKeySet(context.Background()); set || err != nil {
		t.Fatalf("ManagementKeySet = %v, %v; want false, nil", set, err)
	}
	if _, err := adapter.Mint(context.Background(), openrouterprovision.MintRequest{}); !errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		t.Fatalf("Mint error = %v, want ErrManagementKeyUnset", err)
	}
}

func TestDisablerIsWiredWithThePool(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: &config.Config{AuthulaSecret: validProvisioningAuthulaSecret}}
	if openRouterKeyDisablerFor(chat) == nil {
		t.Fatal("openRouterKeyDisablerFor: want a port whenever the pool and AURA_AUTHULA_SECRET exist")
	}
	if openRouterKeyDisablerFor(nil) != nil {
		t.Fatal("openRouterKeyDisablerFor(nil): want nil")
	}
}
