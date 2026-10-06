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

// The floor decides whether per-identity memory is safe to run at all, so its boundary is
// pinned exactly: 26.4.2 closed CVE-2026-44221, and 26.10.1 stopped treating a vector
// filter that matches nothing as no filter (ArcadeData/arcadedb#8959), which turn recall's
// space and label filters rely on.
func TestVersionComparisonAtTheFloor(t *testing.T) {
	t.Parallel()
	below := []string{"26.10.0", "26.9.1", "26.4.2", "26.4.1", "25.12.0", "1.0.0"}
	atOrAbove := []string{"26.10.1", "26.10.2", "26.11.0", "27.0.0"}
	for _, raw := range below {
		v, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", raw, err)
		}
		if !v.Less(minSecureVersion) {
			t.Errorf("%s is below the floor but was accepted", raw)
		}
	}
	for _, raw := range atOrAbove {
		v, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", raw, err)
		}
		if v.Less(minSecureVersion) {
			t.Errorf("%s meets the floor but was refused", raw)
		}
	}
}
