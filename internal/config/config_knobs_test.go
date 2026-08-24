package config

import (
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestKnobRegistry round-trips the QUAL-04 promise (D-08): the registry is the
// single source of truth for the hot-path AURA_* knobs. It asserts the structural
// invariants that keep the slice honest — no duplicate Name, every enum row carries
// a non-empty Enum (and non-enum rows carry none), exactly the seven secret knobs are
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

	// Exactly the seven secret knobs are flagged Secret (drives rendered redaction).
	wantSecret := map[string]bool{
		"AURA_OBJECTSTORE_ACCESS_KEY": true,
		"AURA_OBJECTSTORE_SECRET_KEY": true,
		"GARAGE_RPC_SECRET":           true,
		"AURA_GARAGE_ADMIN_TOKEN":     true,
		"AURA_AUTHULA_SECRET":         true,
		"AURA_TRACE_ENCRYPT_KEY":      true,
	}
	gotSecret := map[string]bool{}
	for _, k := range reg {
		if k.Secret {
			gotSecret[k.Name] = true
		}
	}
	if !reflect.DeepEqual(gotSecret, wantSecret) {
		t.Errorf("secret knob set = %v, want exactly the seven secret knobs %v", gotSecret, wantSecret)
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
	if k, ok := byName["AURA_AGUI_RUN_DETACH"]; !ok || k.Kind != KindBool {
		t.Errorf("AURA_AGUI_RUN_DETACH present=%v kind=%d, want present KindBool (%d)", ok, k.Kind, KindBool)
	}
	if k, ok := byName["AURA_AGUI_RUN_BUFFER_EVENTS"]; !ok || k.Kind != KindInt {
		t.Errorf("AURA_AGUI_RUN_BUFFER_EVENTS present=%v kind=%d, want present KindInt (%d)", ok, k.Kind, KindInt)
	}
	if k, ok := byName["AURA_REASONING_TRACE"]; !ok || k.Kind != KindString {
		t.Errorf("AURA_REASONING_TRACE present=%v kind=%d, want present KindString (%d)", ok, k.Kind, KindString)
	}
	if k, ok := byName["AURA_TRACE_FULL_ACK"]; !ok || k.Kind != KindBool {
		t.Errorf("AURA_TRACE_FULL_ACK present=%v kind=%d, want present KindBool (%d)", ok, k.Kind, KindBool)
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

// findViolation returns the first Violation naming knob, and whether one exists.
func findViolation(vs []Violation, knob string) (Violation, bool) {
	for _, v := range vs {
		if v.Knob == knob {
			return v, true
		}
	}
	return Violation{}, false
}

// checkableKnobs returns the registry rows the re-parse pass actually validates
// (int/bool/enum) — KindString rows carry no parse check, so they cannot violate.
func checkableKnobs() []KnobSpec {
	var out []KnobSpec
	for _, k := range knobRegistry() {
		if k.Kind == KindInt || k.Kind == KindBool || k.Kind == KindEnum {
			out = append(out, k)
		}
	}
	return out
}

// reparseProfiles is the full profile axis the invariants sample over.
var reparseProfiles = []RuntimeProfile{
	ProfileDev, ProfileLocalTrusted, ProfileSingleUserHardened, ProfileServerProduction,
}

// drawGarbage produces a string that fails strconv.Atoi AND strconv.ParseBool AND any
// Enum match: 2-8 letters drawn from a class that excludes digits and the bool-literal
// letters (t/f/T/F), so it can never be a valid int, bool token, or runtime-profile name.
func drawGarbage(rt *rapid.T) string {
	return rapid.StringMatching(`[g-su-zG-SU-Z]{2,8}`).Draw(rt, "garbage")
}

// TestReparsePass is the example-grounded truth table behind the PROF-04 engine:
// invalid int/bool/enum ⇒ Fatal under strict tiers, Warn under lenient tiers, naming
// the knob; unset/whitespace ⇒ no violation (uses the default); a valid value ⇒ none.
func TestReparsePass(t *testing.T) {
	const set, unset = true, false
	cases := []struct {
		name     string
		profile  RuntimeProfile
		knob     string
		value    string
		setValue bool
		wantViol bool
		wantSev  Severity
	}{
		{"int garbage / production ⇒ Fatal", ProfileServerProduction, "AURA_AGUI_BUFFER_CAP", "notanumber", set, true, Fatal},
		{"int garbage / hardened ⇒ Fatal", ProfileSingleUserHardened, "AURA_OBJECTSTORE_REPLICATION_FACTOR", "two", set, true, Fatal},
		{"int garbage / dev ⇒ Warn", ProfileDev, "AURA_AGUI_BUFFER_CAP", "notanumber", set, true, Warn},
		{"int garbage / local_trusted ⇒ Warn", ProfileLocalTrusted, "AURA_AGUI_BUFFER_CAP", "notanumber", set, true, Warn},
		{"bool garbage / production ⇒ Fatal", ProfileServerProduction, "AURA_CONTEXT_COMPACTION_ENABLED", "yesnt", set, true, Fatal},
		{"bool garbage / dev ⇒ Warn", ProfileDev, "AURA_VISION_CLOUD", "maybe", set, true, Warn},
		{"enum garbage / production ⇒ Fatal", ProfileServerProduction, "AURA_PROFILE", "bogus_tier", set, true, Fatal},
		{"enum garbage / dev ⇒ Warn", ProfileDev, "AURA_PROFILE", "bogus_tier", set, true, Warn},
		{"valid int ⇒ none", ProfileServerProduction, "AURA_AGUI_BUFFER_CAP", "128", set, false, Warn},
		{"valid bool ⇒ none", ProfileServerProduction, "AURA_CONTEXT_COMPACTION_ENABLED", "true", set, false, Warn},
		{"valid enum ⇒ none", ProfileServerProduction, "AURA_PROFILE", string(ProfileLocalTrusted), set, false, Warn},
		{"whitespace ⇒ none (uses default)", ProfileServerProduction, "AURA_AGUI_BUFFER_CAP", "   ", set, false, Warn},
		// WR-01: a whitespace-PADDED but non-empty int silently falls back at runtime
		// because envutil.IntDefault parses the raw (untrimmed) value — so the re-parse
		// pass must flag it, not mask it by trimming first.
		{"padded int ' 128' ⇒ Fatal (raw parse mirrors envutil, WR-01)", ProfileServerProduction, "AURA_AGUI_BUFFER_CAP", " 128", set, true, Fatal},
		{"unset ⇒ none (uses default)", ProfileServerProduction, "AURA_AGUI_BUFFER_CAP", "", unset, false, Warn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearPostgresEnv(t)
			if tc.setValue {
				t.Setenv(tc.knob, tc.value)
			}
			vs := reparsePass(tc.profile)
			v, found := findViolation(vs, tc.knob)
			if found != tc.wantViol {
				t.Fatalf("reparsePass(%s) knob %q: violation present=%v, want %v (got %+v)", tc.profile, tc.knob, found, tc.wantViol, vs)
			}
			if !tc.wantViol {
				return
			}
			if v.Sev != tc.wantSev {
				t.Errorf("knob %q under %s: sev=%v, want %v", tc.knob, tc.profile, v.Sev, tc.wantSev)
			}
			if v.Knob != tc.knob {
				t.Errorf("violation must NAME the knob: got %q, want %q", v.Knob, tc.knob)
			}
		})
	}
}

// TestRapidEnvStrictness is PROF-04 / criterion #1 invariant 1 (the centrepiece):
// for ANY cataloged checkable knob and ANY garbage value, reparsePass under a strict
// profile yields a Fatal naming that knob, and under a lenient profile at most a Warn
// (never Fatal). Proven for every knob × garbage, not a single example.
func TestRapidEnvStrictness(t *testing.T) {
	clearPostgresEnv(t)
	knobs := checkableKnobs()
	rapid.Check(t, func(rt *rapid.T) {
		k := rapid.SampledFrom(knobs).Draw(rt, "knob")
		p := rapid.SampledFrom(reparseProfiles).Draw(rt, "profile")
		garbage := drawGarbage(rt)

		os.Setenv(k.Name, garbage)
		defer os.Unsetenv(k.Name)

		v, found := findViolation(reparsePass(p), k.Name)
		if !found {
			rt.Fatalf("knob %q=%q under %q produced no violation (garbage must always violate)", k.Name, garbage, p)
		}
		if p.Strict() {
			if v.Sev != Fatal {
				rt.Fatalf("strict profile %q: knob %q sev=%v, want Fatal", p, k.Name, v.Sev)
			}
		} else if v.Sev != Warn {
			rt.Fatalf("lenient profile %q: knob %q sev=%v, want Warn (lenient must NEVER be Fatal)", p, k.Name, v.Sev)
		}
	})
}

// TestRapidEnvNoFalsePositive is invariant 2: for ANY cataloged knob set to a VALID
// value of its kind, the re-parse pass yields NO violation for that knob (under any
// profile). Guards the engine against flagging well-formed configuration.
func TestRapidEnvNoFalsePositive(t *testing.T) {
	clearPostgresEnv(t)
	knobs := checkableKnobs()
	rapid.Check(t, func(rt *rapid.T) {
		k := rapid.SampledFrom(knobs).Draw(rt, "knob")
		p := rapid.SampledFrom(reparseProfiles).Draw(rt, "profile")

		var valid string
		switch k.Kind {
		case KindInt:
			valid = strconv.Itoa(rapid.IntRange(-1_000_000, 1_000_000).Draw(rt, "intval"))
		case KindBool:
			valid = rapid.SampledFrom([]string{"true", "false", "1", "0", "t", "f", "T", "F", "TRUE", "FALSE"}).Draw(rt, "boolval")
		case KindEnum:
			valid = rapid.SampledFrom(k.Enum).Draw(rt, "enumval")
		}

		os.Setenv(k.Name, valid)
		defer os.Unsetenv(k.Name)

		if v, found := findViolation(reparsePass(p), k.Name); found {
			rt.Fatalf("valid value %q for knob %q under %q produced a false-positive violation %+v", valid, k.Name, p, v)
		}
	})
}

// TestRapidEnvAggregationMonotonic is invariant 3: adding a second bad knob never
// removes the first's violation (the pass lists EVERY unmet requirement, not just the
// first). Violation count is monotonic non-decreasing as bad knobs accumulate.
func TestRapidEnvAggregationMonotonic(t *testing.T) {
	clearPostgresEnv(t)
	knobs := checkableKnobs()
	if len(knobs) < 2 {
		t.Skip("need at least two checkable knobs for the aggregation invariant")
	}
	rapid.Check(t, func(rt *rapid.T) {
		p := rapid.SampledFrom(reparseProfiles).Draw(rt, "profile")
		i := rapid.IntRange(0, len(knobs)-1).Draw(rt, "i")
		// j distinct from i (two-knob requirement) without rejection sampling.
		j := (i + 1 + rapid.IntRange(0, len(knobs)-2).Draw(rt, "joff")) % len(knobs)
		kA, kB := knobs[i], knobs[j]

		os.Setenv(kA.Name, drawGarbage(rt))
		defer os.Unsetenv(kA.Name)
		before := reparsePass(p)
		if _, found := findViolation(before, kA.Name); !found {
			rt.Fatalf("baseline: bad knob %q produced no violation", kA.Name)
		}

		os.Setenv(kB.Name, drawGarbage(rt))
		defer os.Unsetenv(kB.Name)
		after := reparsePass(p)
		if _, still := findViolation(after, kA.Name); !still {
			rt.Fatalf("adding bad knob %q dropped the violation for %q (aggregation must be monotonic)", kB.Name, kA.Name)
		}
		if len(after) < len(before) {
			rt.Fatalf("violation count decreased %d -> %d after adding a second bad knob", len(before), len(after))
		}
	})
}
