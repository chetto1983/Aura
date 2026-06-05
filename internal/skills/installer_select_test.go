package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestNormalizeRepoShorthand proves the amendment-#49 owner/repo expansion: the
// skills.sh catalog `source` shorthand becomes a canonical GitHub URL, while real
// URLs, scp-style remotes, and malformed strings pass through untouched (and the
// malformed ones still die in validateRepoURL).
func TestNormalizeRepoShorthand(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"anthropics/skills", "https://github.com/anthropics/skills"},
		{"gracefullight/stock-checker", "https://github.com/gracefullight/stock-checker"},
		{"owner-x/repo.name_1", "https://github.com/owner-x/repo.name_1"},
		{"https://github.com/anthropics/skills", "https://github.com/anthropics/skills"},
		{"git://host/repo", "git://host/repo"},
		{"user@host:path", "user@host:path"},
		{"owner/repo/extra", "owner/repo/extra"}, // 3 segments: NOT a shorthand
		{"/abs/path", "/abs/path"},               // empty first segment
		{"owner/", "owner/"},                     // empty second segment
		{"owner repo/x", "owner repo/x"},         // whitespace: untouched
		{"-flag/repo", "-flag/repo"},             // '-' prefix survives for validateRepoURL to reject
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeRepoShorthand(c.in); got != c.want {
			t.Errorf("normalizeRepoShorthand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The '-'-prefixed shorthand must still be rejected downstream (option injection).
	if err := validateRepoURL(normalizeRepoShorthand("-flag/repo")); err == nil {
		t.Error("validateRepoURL accepted a '-'-prefixed pseudo-shorthand")
	}
}

// TestLocateSkillDirByName proves the multi-skill selector: a named lookup returns
// ONLY the matching depth-1/2 dir, and a named miss is a loud ErrNoSkillFound —
// never a silent fallback to the first skill found (amendment #49).
func TestLocateSkillDirByName(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\n---\nbody"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mk("algorithmic-art") // alphabetically first — the trap the selector avoids
	mk("xlsx")
	mk("nested/pdf") // depth-2 layout

	got, err := locateSkillDir(root, "xlsx")
	if err != nil {
		t.Fatalf("locateSkillDir xlsx: %v", err)
	}
	if filepath.Base(got) != "xlsx" {
		t.Errorf("selector returned %q, want the xlsx dir", got)
	}

	got, err = locateSkillDir(root, "pdf")
	if err != nil {
		t.Fatalf("locateSkillDir pdf (depth 2): %v", err)
	}
	if filepath.Base(got) != "pdf" {
		t.Errorf("selector returned %q, want the nested pdf dir", got)
	}

	if _, err := locateSkillDir(root, "absent"); !errors.Is(err, ErrNoSkillFound) {
		t.Errorf("named miss: err = %v, want ErrNoSkillFound (no silent first-found fallback)", err)
	}

	// Legacy empty-name behavior still returns the first skill found.
	if _, err := locateSkillDir(root, ""); err != nil {
		t.Errorf("legacy first-found walk broke: %v", err)
	}
}

// TestFormatTokenAndMerge proves the catalog format-boost primitives: alias
// mapping (excel→xlsx), no-token queries, and the installs-desc dedup merge.
func TestFormatTokenAndMerge(t *testing.T) {
	if tok := formatToken("excel yahoo finance stock market"); tok != "xlsx" {
		t.Errorf("formatToken excel… = %q, want xlsx", tok)
	}
	if tok := formatToken("make me a PDF report"); tok != "pdf" {
		t.Errorf("formatToken pdf… = %q, want pdf", tok)
	}
	if tok := formatToken("yahoo finance data"); tok != "" {
		t.Errorf("formatToken no-format = %q, want empty", tok)
	}

	a := []CatalogItem{{ID: "s/a", Name: "a", Installs: 10}, {ID: "s/b", Name: "b", Installs: 5}}
	b := []CatalogItem{{ID: "s/xlsx", Name: "xlsx", Installs: 1000}, {ID: "s/a", Name: "a", Installs: 10}}
	merged := mergeCatalogResults(a, b)
	if len(merged) != 3 {
		t.Fatalf("merge len = %d, want 3 (dedup by ID)", len(merged))
	}
	if merged[0].Name != "xlsx" {
		t.Errorf("merge[0] = %q, want xlsx first (installs desc)", merged[0].Name)
	}
}
