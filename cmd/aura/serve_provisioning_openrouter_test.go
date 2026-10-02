package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

// The reconciler tells a deleted key from a live one through these two calls: a person's key
// by its hash with the management key, the services key by asking the provider with the key
// itself, since the settings keep no hash for it.
func TestMintingAdapterAsksTheProviderAboutAKey(t *testing.T) {
	auth := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth[r.URL.Path] = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/keys/hash-gone":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/key" && r.Header.Get("Authorization") == "Bearer sk-dead":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/key":
			_, _ = w.Write([]byte(`{"data":{"usage":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	adapter := openRouterMintingAdapter{openRouterKeyConfig{
		client: srv.Client(), baseURL: srv.URL,
		managementKey: func(context.Context) (string, error) { return "sk-mgmt", nil },
	}}
	ctx := context.Background()

	if _, err := adapter.Get(ctx, "hash-gone"); !errors.Is(err, openrouterprovision.ErrKeyNotFound) || auth["/keys/hash-gone"] != "Bearer sk-mgmt" {
		t.Fatalf("Get error = %v auth = %q; want ErrKeyNotFound asked with the management key", err, auth["/keys/hash-gone"])
	}
	if err := adapter.CheckKey(ctx, "sk-dead"); !errors.Is(err, openrouterprovision.ErrKeyRevoked) {
		t.Fatalf("CheckKey(dead) error = %v, want ErrKeyRevoked", err)
	}
	if err := adapter.CheckKey(ctx, "sk-live"); err != nil || auth["/key"] != "Bearer sk-live" {
		t.Fatalf("CheckKey(live) error = %v auth = %q; want nil, asked with the key itself", err, auth["/key"])
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
