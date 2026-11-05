package config

import "testing"

func TestLoadDecideReadsTheTwoVariablesAndDefaultsNeither(t *testing.T) {
	t.Setenv("AURA_DECIDE_BASE_URL", "")
	t.Setenv("AURA_DECIDE_MODEL", "")
	if got := LoadDecide(); got != (DecideConfig{}) {
		t.Fatalf("LoadDecide() = %+v, want no address and no model by default", got)
	}
	t.Setenv("AURA_DECIDE_BASE_URL", " http://aura-llama-decide:8085 ")
	t.Setenv("AURA_DECIDE_MODEL", " "+DecideOff+" ")
	want := DecideConfig{BaseURL: "http://aura-llama-decide:8085", Model: DecideOff}
	if got := LoadDecide(); got != want {
		t.Fatalf("LoadDecide() = %+v, want %+v", got, want)
	}
}
