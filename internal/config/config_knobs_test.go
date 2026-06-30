package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestKnobRegistry round-trips the QUAL-04 promise (D-08): the registry is the
// single source of truth for the hot-path AURA_* knobs. It asserts the structural
// invariants that keep the slice honest — no duplicate Name, every enum row carries
// a non-empty Enum (and non-enum rows carry none), exactly the four secret knobs are
// flagged Secret, the AURA_PROFILE enum row spells the four runtime profiles, the two
// representative reliability/gate knobs are present with the right kind, and no Tier C
// (agent-tools/loop/llm) knob leaked in (D-16).
func TestKnobRegistry(t *testing.T) {
	reg := knobRegistry()
	if len(reg) == 0 {
		t.Fatal("knobRegistry() returned an empty slice")
	}

	byName := make(map[string]KnobSpec, len(reg))
	for _, k := range reg {
		if _, dup := byName[k.Name]; dup {
			t.Errorf("duplicate knob Name %q — the registry must hold exactly one row per knob", k.Name)
		}
		byName[k.Name] = k
	}

	// Every KindEnum row carries a non-empty Enum; non-enum rows carry none.
	for _, k := range reg {
		switch k.Kind {
		case KindEnum:
			if len(k.Enum) == 0 {
				t.Errorf("KindEnum knob %q has an empty Enum set", k.Name)
			}
		default:
			if len(k.Enum) != 0 {
				t.Errorf("non-enum knob %q (kind %d) carries a stray Enum %v", k.Name, k.Kind, k.Enum)
			}
		}
	}

	// Exactly the four secret knobs are flagged Secret (drives plan-05 redaction).
	wantSecret := map[string]bool{
		"AURA_OBJECTSTORE_ACCESS_KEY": true,
		"AURA_OBJECTSTORE_SECRET_KEY": true,
		"GARAGE_RPC_SECRET":           true,
		"AURA_AUTHULA_SECRET":         true,
	}
	gotSecret := map[string]bool{}
	for _, k := range reg {
		if k.Secret {
			gotSecret[k.Name] = true
		}
	}
	if !reflect.DeepEqual(gotSecret, wantSecret) {
		t.Errorf("secret knob set = %v, want exactly the four secret knobs %v", gotSecret, wantSecret)
	}

	// AURA_PROFILE is the KindEnum row spelling the four runtime profiles.
	p, ok := byName["AURA_PROFILE"]
	if !ok {
		t.Fatal("AURA_PROFILE row is missing from the registry")
	}
	if p.Kind != KindEnum {
		t.Errorf("AURA_PROFILE Kind = %d, want KindEnum (%d)", p.Kind, KindEnum)
	}
	wantEnum := []string{
		string(ProfileDev),
		string(ProfileLocalTrusted),
		string(ProfileSingleUserHardened),
		string(ProfileServerProduction),
	}
	if !slices.Equal(p.Enum, wantEnum) {
		t.Errorf("AURA_PROFILE Enum = %v, want %v", p.Enum, wantEnum)
	}

	// Representative reliability/gate knobs are catalogued with the right kind.
	if k, ok := byName["AURA_OBJECTSTORE_REPLICATION_FACTOR"]; !ok || k.Kind != KindInt {
		t.Errorf("AURA_OBJECTSTORE_REPLICATION_FACTOR present=%v kind=%d, want present KindInt (%d)", ok, k.Kind, KindInt)
	}
	if k, ok := byName["AURA_AGUI_CORS_PERMISSIVE"]; !ok || k.Kind != KindBool {
		t.Errorf("AURA_AGUI_CORS_PERMISSIVE present=%v kind=%d, want present KindBool (%d)", ok, k.Kind, KindBool)
	}

	// No Tier C knob (agent-tools/loop/llm) leaked into the Tier A+B cut (D-16).
	for _, k := range reg {
		for _, banned := range []string{"AURA_LOOP_", "AURA_LLM_", "AURA_SWARM_MAX_DEPTH", "AURA_FS_"} {
			if strings.Contains(k.Name, banned) {
				t.Errorf("Tier C knob %q must not be catalogued (matched banned prefix %q)", k.Name, banned)
			}
		}
	}
}
