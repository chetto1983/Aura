package main

import "testing"

// serve builds the de-provisioning saga before the Authula provider exists and attaches the
// two Authula legs afterwards (serve.go, SetAuthulaTeardown). That only reaches the cockpit's
// removal route AND the cron grace-window sweep if both are handed the same instance, so the
// builder must memoize rather than assemble a second copy of the same wiring.
func TestBuildDeprovisionerIsOneSharedInstance(t *testing.T) {
	chat := &chatEnv{}

	first, second := buildDeprovisioner(chat), buildDeprovisioner(chat)

	if first == nil {
		t.Fatal("buildDeprovisioner returned nil; the route's wiring has no typed-nil guard")
	}
	if first != second {
		t.Fatalf("buildDeprovisioner returned %p then %p; want one shared instance", first, second)
	}
}

// A nil environment still answers with a usable saga: PurgeExpired is a safe no-op on it.
func TestBuildDeprovisionerToleratesNoEnvironment(t *testing.T) {
	if buildDeprovisioner(nil) == nil {
		t.Fatal("buildDeprovisioner(nil) = nil, want an empty saga")
	}
}
