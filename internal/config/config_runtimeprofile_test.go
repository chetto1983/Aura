package config

import "testing"

// TestParseProfile locks ParseProfile as a total transform (D-03): the four named
// profiles map to their constant, empty/unknown defaults to the loudest, most
// permissive tier (dev), and Strict() collapses {dev,local_trusted}→lenient vs
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
		// Unset/empty defaults to dev (D-03) — preserves today's full-host behavior.
		{name: "empty defaults to dev", in: "", want: ProfileDev, wantStrict: false},
		// Unknown never panics, never errors — total parser defaults to dev.
		{name: "garbage defaults to dev", in: "not-a-profile", want: ProfileDev, wantStrict: false},
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
