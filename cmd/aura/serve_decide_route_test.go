package main

import "testing"

// The sidecar's address is read when the wiring first asks, which is after main has loaded .env,
// and the answer is then the process's for good: the tick's route and the cockpit's flag see one read.
func TestDecideBootIsReadOnceWhenFirstAsked(t *testing.T) {
	boot := newDecideBoot()
	t.Setenv("AURA_DECIDE_BASE_URL", "http://aura-llama-decide:8085")
	if got := boot().BaseURL; got != "http://aura-llama-decide:8085" {
		t.Fatalf("BaseURL = %q, want the address set before the first read", got)
	}
	t.Setenv("AURA_DECIDE_BASE_URL", "http://elsewhere.example:1")
	if got := boot().BaseURL; got != "http://aura-llama-decide:8085" {
		t.Fatalf("BaseURL = %q after the environment moved, want the first read", got)
	}
}
