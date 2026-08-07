package tools

import (
	"regexp"
	"strings"
	"testing"
)

// The pure half of read_file/search_files: the char budget that produces next_offset, the
// filename suggestions a miss returns instead of a bare ENOENT, and the count renderer. None of
// it needs a box, and the container-gated tier contributes ZERO coverage (CLAUDE.md), so it is
// proved here or not at all.

func TestTruncateToCharBudgetCutsOnALineBoundary(t *testing.T) {
	content := "alpha\nbeta\ngamma\ndelta"

	// Under budget: everything, and the line count is what the caller pages with.
	kept, lines := truncateToCharBudget(content, 1000)
	if kept != content || lines != 4 {
		t.Errorf("under budget = (%q, %d), want the whole content and 4 lines", kept, lines)
	}

	// Over budget: the cut lands on a LINE boundary, never mid-line — the continuation offset
	// read_file returns is a line number, so a mid-line cut would make next_offset resume in
	// the middle of a line the caller already half-read.
	kept, lines = truncateToCharBudget(content, 12)
	if strings.HasSuffix(kept, "\n") || strings.Contains(kept, "gamma") {
		t.Errorf("kept = %q, want a clean prefix of whole lines", kept)
	}
	if lines != strings.Count(kept, "\n")+1 {
		t.Errorf("linesKept = %d does not match the lines in %q", lines, kept)
	}
	for _, line := range strings.Split(kept, "\n") {
		if !strings.Contains(content, line) {
			t.Errorf("line %q is not a whole line of the source", line)
		}
	}
}

func TestTruncateToCharBudgetKeepsARuneWhole(t *testing.T) {
	// One pathological line longer than the whole budget: there is no line boundary to cut on,
	// so the fallback clamps bytes — and must not split a multi-byte rune, which would emit
	// invalid UTF-8 into the model's context.
	line := strings.Repeat("è", 40) // 2 bytes per rune
	kept, lines := truncateToCharBudget(line, 15)
	if lines != 1 {
		t.Errorf("a single oversized line should report 1 line, got %d", lines)
	}
	if !utf8Valid(kept) {
		t.Errorf("truncation split a rune: %q", kept)
	}
	if len(kept) > 15 {
		t.Errorf("kept %d bytes, over the 15-byte budget", len(kept))
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestTruncateRuneBoundaryNeverSplitsARune(t *testing.T) {
	s := "aàbèc" // mixed 1- and 2-byte runes
	for cut := 0; cut <= len(s); cut++ {
		got := truncateRuneBoundary(s, cut)
		if len(got) > cut {
			t.Fatalf("truncateRuneBoundary(%q, %d) returned %d bytes", s, cut, len(got))
		}
		if !utf8Valid(got) {
			t.Fatalf("truncateRuneBoundary(%q, %d) = %q split a rune", s, cut, got)
		}
	}
}

func TestScoreSimilarNamesSurfacesTheRealFile(t *testing.T) {
	// The point of the suggestion list: a wrong extension or a case slip should surface the
	// file that exists, rather than the model retrying the same miss.
	got := scoreSimilarNames("report.md", []string{"unrelated.go", "report.txt", "notes.md", "REPORT.MD"})
	if len(got) == 0 {
		t.Fatal("no suggestions for a near-miss filename")
	}
	// An exact case-insensitive hit must outrank a same-stem-different-extension one.
	if !strings.EqualFold(got[0], "REPORT.MD") {
		t.Errorf("best suggestion = %q, want the case-insensitive exact match first (%v)", got[0], got)
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "report.txt") {
		t.Errorf("same-stem candidate missing from %v", got)
	}
	if strings.Contains(joined, "unrelated.go") {
		t.Errorf("an unrelated name was suggested: %v", got)
	}
}

func TestSplitExtAndBoxPathHelpers(t *testing.T) {
	for _, tc := range []struct{ in, stem, ext string }{
		{"report.md", "report", ".md"},
		{"archive.tar.gz", "archive.tar", ".gz"},
		{"noext", "noext", ""},
		// A leading dot is the whole name, not an extension: ".env" is a file called .env.
		{".env", ".env", ""},
	} {
		stem, ext := splitExt(tc.in)
		if stem != tc.stem || ext != tc.ext {
			t.Errorf("splitExt(%q) = (%q, %q), want (%q, %q)", tc.in, stem, ext, tc.stem, tc.ext)
		}
	}

	for _, tc := range []struct{ in, dir, base string }{
		{"/workspace/sub/a.txt", "/workspace/sub", "a.txt"},
		{"/a.txt", "/", "a.txt"},
		{"a.txt", ".", "a.txt"},
	} {
		if got := boxDir(tc.in); got != tc.dir {
			t.Errorf("boxDir(%q) = %q, want %q", tc.in, got, tc.dir)
		}
		if got := boxBase(tc.in); got != tc.base {
			t.Errorf("boxBase(%q) = %q, want %q", tc.in, got, tc.base)
		}
	}

	// boxJoin must not double the separator when the directory is the box root.
	if got := boxJoin("/", "a.txt"); got != "/a.txt" {
		t.Errorf("boxJoin(\"/\", …) = %q, want /a.txt", got)
	}
	if got := boxJoin("/workspace", "a.txt"); got != "/workspace/a.txt" {
		t.Errorf("boxJoin = %q, want /workspace/a.txt", got)
	}
}

func TestRenderSearchCountsOmitsFilesWithNoMatch(t *testing.T) {
	files := []boxFile{
		{Rel: "a.go", Content: []byte("port := 8080\nport := 9090\n")},
		{Rel: "b.md", Content: []byte("nothing here\n")},
	}
	re := regexp.MustCompile(`port`)

	got := renderSearchCounts(files, "", re, 50, 0)
	if !strings.Contains(got, "a.go: 2") {
		t.Errorf("count output = %q, want a.go with 2 matches", got)
	}
	// A zero-match file must be ABSENT, not reported as ": 0" — count mode answers "where are
	// the matches", and a list of zeroes buries the answer.
	if strings.Contains(got, "b.md") {
		t.Errorf("a file with no matches was listed: %q", got)
	}

	// The glob filter narrows the same call rather than needing a second one.
	if got := renderSearchCounts(files, "*.md", re, 50, 0); strings.Contains(got, "a.go") {
		t.Errorf("glob filter ignored: %q", got)
	}
}
