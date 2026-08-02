package arcadedb

import "testing"

func TestParseVersionHandlesWhatTheServerActuallyReports(t *testing.T) {
	t.Parallel()
	cases := map[string]version{
		// verbatim from a live 26.7.3 server
		"26.7.3 (build 4ea63d480cdd6caa2c61c663340a77a878a84e8f/1784308304225/26.7.3-hotfix)": {26, 7, 3},
		"26.4.2":            {26, 4, 2},
		"26.7.3-hotfix":     {26, 7, 3},
		"25.10.1 (build x)": {25, 10, 1},
	}
	for raw, want := range cases {
		got, err := parseVersion(raw)
		if err != nil {
			t.Errorf("parseVersion(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("parseVersion(%q) = %v, want %v", raw, got, want)
		}
	}
	for _, bad := range []string{"", "26.7", "not a version", "26.x.3"} {
		if _, err := parseVersion(bad); err == nil {
			t.Errorf("parseVersion(%q) returned no error", bad)
		}
	}
}

// The comparison decides whether per-identity memory is safe to run at all, so
// the boundary is worth pinning exactly: 26.4.2 is the FIRST patched release.
func TestVersionComparisonAtTheCVEBoundary(t *testing.T) {
	t.Parallel()
	affected := []string{"26.4.1", "26.3.9", "25.12.0", "1.0.0"}
	patched := []string{"26.4.2", "26.4.3", "26.7.3", "27.0.0"}
	for _, raw := range affected {
		v, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", raw, err)
		}
		if !v.Less(minSecureVersion) {
			t.Errorf("%s should be treated as affected by CVE-2026-44221", raw)
		}
	}
	for _, raw := range patched {
		v, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", raw, err)
		}
		if v.Less(minSecureVersion) {
			t.Errorf("%s is patched but was treated as affected", raw)
		}
	}
}
