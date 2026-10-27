package config

import "testing"

// TestParseProfile locks ParseProfile as a total transform: the four named profiles
// map to their constant, empty/unknown falls to DefaultProfile (single_user_hardened,
// so the lenient dev tier is only ever an explicit choice), and Strict() collapses
// {dev,local_trusted}→lenient vs
// {single_user_hardened,server_production}→strict (D-07/D-14). Mirrors the
// pure-function table style of TestGuardWebBind.
func TestParseProfile(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		want       RuntimeProfile
		wantStrict bool
	}{
		{name: "dev", in: "dev", want: ProfileDev, wantStrict: false},
		{name: "local_trusted", in: "local_trusted", want: ProfileLocalTrusted, wantStrict: false},
		{name: "single_user_hardened", in: "single_user_hardened", want: ProfileSingleUserHardened, wantStrict: true},
		{name: "server_production", in: "server_production", want: ProfileServerProduction, wantStrict: true},
		{name: "empty defaults to hardened", in: "", want: ProfileSingleUserHardened, wantStrict: true},
		// Unknown never panics, never errors; a typo fails toward the strict tier.
		{name: "garbage defaults to hardened", in: "not-a-profile", want: ProfileSingleUserHardened, wantStrict: true},
		// Surrounding whitespace is trimmed before the match.
		{name: "whitespace trimmed", in: "  server_production  ", want: ProfileServerProduction, wantStrict: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseProfile(tc.in)
			if got != tc.want {
				t.Errorf("ParseProfile(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got.Strict() != tc.wantStrict {
				t.Errorf("ParseProfile(%q).Strict() = %v, want %v", tc.in, got.Strict(), tc.wantStrict)
			}
		})
	}
}

// TestRuntimeProfileFieldsLoad locks that loadBase populates the runtime fields from env
// (LoadDB returns them as-is), mirroring TestWebAuthConfigLoad: AURA_PROFILE unset ->
// single_user_hardened, and GARAGE_RPC_SECRET round-trips from its upstream env name
// (D-13/PROF-03).
func TestRuntimeProfileFieldsLoad(t *testing.T) {
	clearPostgresEnv(t)

	// Defaults: unset AURA_PROFILE -> single_user_hardened, empty rpc secret.
	cfg := LoadDB()
	if cfg.Profile != ProfileSingleUserHardened {
		t.Errorf("Profile default = %q, want %q", cfg.Profile, ProfileSingleUserHardened)
	}
	if cfg.GarageRPCSecret != "" {
		t.Errorf("GarageRPCSecret default = %q, want empty", cfg.GarageRPCSecret)
	}

	t.Setenv("AURA_PROFILE", "server_production")
	t.Setenv("GARAGE_RPC_SECRET", "abc")

	cfg = LoadDB()
	if cfg.Profile != ProfileServerProduction {
		t.Errorf("Profile override = %q, want %q", cfg.Profile, ProfileServerProduction)
	}
	if cfg.GarageRPCSecret != "abc" {
		t.Errorf("GarageRPCSecret override = %q, want abc", cfg.GarageRPCSecret)
	}
}
