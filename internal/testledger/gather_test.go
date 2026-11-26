package testledger

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeclareTreeOnThisRepository(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	declared, err := DeclareTree(root)
	if err != nil {
		t.Fatal(err)
	}
	has := func(units []Unit, suite, id string) bool {
		return slices.ContainsFunc(units, func(u Unit) bool { return u.Suite == suite && u.ID == id })
	}
	for _, want := range []struct{ suite, id string }{
		{"go", "github.com/chetto1983/aura/internal/testledger TestDeclareTreeOnThisRepository"},
		{"go", "github.com/chetto1983/aura/tools/deadexport TestAnalyzeHonoursBuildTags"},
	} {
		if !has(declared.Units, want.suite, want.id) {
			t.Errorf("declared units lack %s %s", want.suite, want.id)
		}
	}
	for _, want := range []struct{ suite, file string }{
		{"playwright", "web/e2e/native-media-live.spec.ts"},
		{"vitest", "web/src/chat/voice/voiceMocks.test.ts"},
		{"vitest", "packages/create-aura/src/__tests__/cli.test.ts"},
		{"script", "scripts/run_script_tests_test.sh"},
		{"python", "scripts/unittest_ledger_test.py"},
	} {
		if !has(declared.Files, want.suite, want.file) {
			t.Errorf("declared files lack %s %s", want.suite, want.file)
		}
	}
	for _, u := range append(declared.Units, declared.Files...) {
		if strings.Contains(u.File, "/testdata/") {
			t.Errorf("%s %s is a fixture, not a declared test", u.Suite, u.ID)
		}
	}
	ids := map[string]bool{}
	for _, u := range declared.Units {
		if ids[u.Suite+" "+u.ID] {
			t.Errorf("%s %s is declared twice", u.Suite, u.ID)
		}
		ids[u.Suite+" "+u.ID] = true
	}
}

func TestImportPath(t *testing.T) {
	modules := map[string]string{".": "example.com/root", "tools/x": "example.com/x"}
	for dir, want := range map[string]string{
		".":           "example.com/root",
		"internal/a":  "example.com/root/internal/a",
		"tools/x":     "example.com/x",
		"tools/x/sub": "example.com/x/sub",
		"tools/xy":    "example.com/root/tools/xy",
	} {
		if got := importPath(modules, dir); got != want {
			t.Errorf("%s: got %s, want %s", dir, got, want)
		}
	}
}

func writeArtifact(t *testing.T, dir, job string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, "test-ledger-"+job, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeArtifact(t, dir, "unit-test", map[string]string{"unit-race.jsonl": fixture(t, "go.jsonl")})
	writeArtifact(t, dir, "web-e2e", map[string]string{"playwright-123.json": fixture(t, "playwright.json")})
	writeArtifact(t, dir, "web-test", map[string]string{"vitest-web.json": fixture(t, "vitest-mixed.json")})
	writeArtifact(t, dir, "build-and-lint", map[string]string{
		"script-tests.txt": fixture(t, "script-tests.txt"),
		"python-tests.txt": fixture(t, "python-tests.txt"),
	})
	writeArtifact(t, dir, "ingest-sidecar-test", map[string]string{
		"pytest-ingest.collected": fixture(t, "pytest.collected"),
		"pytest-ingest.xml":       fixture(t, "pytest.xml"),
	})
	got, err := LoadArtifacts(dir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, u := range got.Results {
		counts[u.Suite]++
	}
	want := map[string]int{"go": 4, "playwright": 2, "vitest": 4, "script": 3, "python": 4, "pytest": 6}
	for suite, n := range want {
		if counts[suite] != n {
			t.Errorf("%s: got %d results, want %d", suite, counts[suite], n)
		}
	}
	if len(got.Pytest) != 6 || got.Pytest[0].File != "pkg/tests/test_mixed.py" {
		t.Errorf("the collected pytest ids are declared units: %+v", got.Pytest)
	}
	jobs := slices.Sorted(maps.Keys(got.Arrived))
	if !slices.Equal(jobs, []string{"build-and-lint", "ingest-sidecar-test", "unit-test", "web-e2e", "web-test"}) {
		t.Errorf("arrived: %v", jobs)
	}
}

func TestLoadArtifactsRefuses(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"an unknown file":            {"notes.md": "x"},
		"a JUnit report with no ids": {"pytest-ingest.xml": "<testsuites/>"},
		"a collection with no JUnit": {"pytest-ingest.collected": "a.py::t\n"},
		"a malformed go stream":      {"x.jsonl": "not json\n"},
	} {
		dir := t.TempDir()
		writeArtifact(t, dir, "job", files)
		if _, err := LoadArtifacts(dir, workspace); err == nil {
			t.Errorf("%s must be an error", name)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadArtifacts(dir, workspace); err == nil {
		t.Error("anything but test-ledger-<job> directories must be an error")
	}
	if _, err := LoadArtifacts(filepath.Join(dir, "missing"), workspace); err == nil {
		t.Error("a missing artifacts directory must be an error")
	}
}

func TestJobsFromNeeds(t *testing.T) {
	needs := `{"unit-test":{"result":"success","outputs":{}},"web-e2e":{"result":"failure","outputs":{}}}`
	jobs, err := JobsFromNeeds([]byte(needs), map[string]bool{"unit-test": true})
	if err != nil {
		t.Fatal(err)
	}
	want := []Job{{Name: "unit-test", Result: "success", Artifact: true}, {Name: "web-e2e", Result: "failure"}}
	if !slices.Equal(jobs, want) {
		t.Fatalf("got %+v", jobs)
	}
	if _, err := JobsFromNeeds([]byte("{"), nil); err == nil {
		t.Fatal("malformed needs must be an error")
	}
}

func TestSummary(t *testing.T) {
	report := Check(Declared{Units: []Unit{goUnit("a", "TestGone")}}, nil, []Entry{entry("go", "a TestGone", "paid")}, nil)
	report.Violations = append(report.Violations, Violation{"go", "x | y", "declared, and no job executed it"})
	md := Summary(report, true, 0)
	for _, want := range []string{
		"## Test ledger (report-only)",
		"| go | 1 | 0 | 0 | 1 |",
		"| go | a TestGone | paid | because |",
		"### Violations (1)",
		"x \\| y",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("summary lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(Summary(Report{Counts: map[string]*Count{}}, false, 0), "report-only") {
		t.Error("an enforcing summary does not say report-only")
	}
	limited := Summary(Report{Violations: []Violation{{"go", "a", "x"}, {"go", "b", "x"}, {"go", "c", "x"}}}, false, 2)
	if strings.Count(limited, "\n- `go`") != 2 || !strings.Contains(limited, "…and 1 more") {
		t.Errorf("a limited summary lists the first two and counts the rest:\n%s", limited)
	}
}
