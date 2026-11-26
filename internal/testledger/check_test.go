package testledger

import (
	"slices"
	"strings"
	"testing"
)

const mod = "github.com/chetto1983/aura/"

func goUnit(dir, test string) Unit {
	return Unit{Suite: "go", ID: mod + dir + " " + test, File: dir + "/x_test.go"}
}

func ran(u Unit, status Status) Unit {
	return Unit{Suite: u.Suite, ID: u.ID, File: u.File, Status: status}
}

func fileUnit(suite, file string) Unit {
	return Unit{Suite: suite, ID: file, File: file}
}

func entry(suite, selector, class string) Entry {
	return Entry{Suite: suite, Selector: selector, Class: class, Reason: "because"}
}

func problems(r Report) []string {
	var out []string
	for _, v := range r.Violations {
		out = append(out, v.Suite+" "+v.Subject+": "+v.Problem)
	}
	return out
}

func assertProblems(t *testing.T, r Report, want ...string) {
	t.Helper()
	got := problems(r)
	if len(got) != len(want) {
		t.Fatalf("got %d violations, want %d:\n  %s", len(got), len(want), strings.Join(got, "\n  "))
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Errorf("violation %d: got %q, want it to contain %q", i, got[i], want[i])
		}
	}
}

func TestCheckGo(t *testing.T) {
	runs, fails, skips, paid, gone := goUnit("a", "TestRuns"), goUnit("a", "TestFails"), goUnit("a", "TestSkips"), goUnit("b", "TestPaid"), goUnit("b", "TestGone")
	twice := goUnit("c", "TestSkippedHereRunThere")
	declared := Declared{Units: []Unit{runs, fails, skips, paid, gone, twice}}
	results := []Unit{ran(runs, Pass), ran(fails, Fail), ran(skips, Skip), ran(twice, Skip), ran(twice, Pass)}
	report := Check(declared, results, []Entry{entry("go", "b TestPaid", "paid")}, nil)
	assertProblems(t, report,
		"go "+mod+"a TestSkips: skipped in every job that ran it",
		"go "+mod+"b TestGone: declared, and no job executed it",
	)
	if c := report.Counts["go"]; *c != (Count{Declared: 6, Executed: 3, Skipped: 1, Excused: 1}) {
		t.Errorf("counts: %+v", *c)
	}
}

func TestCheckStaleEntries(t *testing.T) {
	runs := goUnit("a", "TestRuns")
	report := Check(Declared{Units: []Unit{runs}}, []Unit{ran(runs, Pass)}, []Entry{
		entry("go", "a/x_test.go", "measure"),
		entry("go", "z TestNowhere", "paid"),
		entry("script", "scripts/a_test.sh", "platform"),
	}, nil)
	assertProblems(t, report,
		"allowlist go a/x_test.go: excuses "+mod+"a TestRuns, which a job executed",
		"allowlist go z TestNowhere: matches no declared test",
		"allowlist script scripts/a_test.sh: matches no declared test",
	)
}

func TestCheckFileSuites(t *testing.T) {
	spec := "web/e2e/a.spec.ts"
	desktop := Unit{Suite: "playwright", ID: spec + " › t [chrome]", File: spec}
	mobile := Unit{Suite: "playwright", ID: spec + " › t [mobile-chrome]", File: spec}
	other := Unit{Suite: "playwright", ID: "web/e2e/b.spec.ts › u [mobile-chrome]", File: "web/e2e/b.spec.ts"}
	declared := Declared{Files: []Unit{
		fileUnit("playwright", spec),
		fileUnit("playwright", "web/e2e/b.spec.ts"),
		fileUnit("playwright", "web/e2e/never.spec.ts"),
		fileUnit("playwright", "web/e2e/remote.spec.ts"),
		fileUnit("vitest", "web/src/x.test.ts"),
		fileUnit("script", "scripts/a_test.sh"),
		fileUnit("python", "scripts/a_test.py"),
	}}
	results := []Unit{
		ran(desktop, Pass), ran(mobile, Skip), ran(other, Skip),
		{Suite: "vitest", ID: "web/src/x.test.ts > x", File: "web/src/x.test.ts", Status: Pass},
		{Suite: "script", ID: "scripts/a_test.sh", File: "scripts/a_test.sh", Status: Fail},
		{Suite: "python", ID: "a_test.T.test_x", File: "scripts/a_test.py", Status: Skip},
	}
	report := Check(declared, results, []Entry{
		entry("playwright", spec+" [mobile-chrome]", "platform"),
		entry("playwright", "web/e2e/remote.spec.ts", "outward"),
	}, nil)
	assertProblems(t, report,
		"playwright web/e2e/b.spec.ts › u [mobile-chrome]: skipped in every job that ran it",
		"playwright web/e2e/never.spec.ts: in no report",
		"python a_test.T.test_x: skipped in every job that ran it",
	)
	if c := report.Counts["playwright"]; *c != (Count{Declared: 4, Executed: 1, Skipped: 1, Excused: 2}) {
		t.Errorf("playwright counts: %+v", *c)
	}
	if c := report.Counts["script"]; *c != (Count{Declared: 1, Executed: 1}) {
		t.Errorf("a failing script still executed: %+v", *c)
	}
}

func TestCheckPytest(t *testing.T) {
	declared := Declared{Units: []Unit{
		{Suite: "pytest", ID: "pkg/test_a.py::test_ran", File: "pkg/test_a.py"},
		{Suite: "pytest", ID: "pkg/test_a.py::test_missing", File: "pkg/test_a.py"},
	}}
	results := []Unit{{Suite: "pytest", ID: "pkg/test_a.py::test_ran", File: "pkg/test_a.py", Status: Pass}}
	assertProblems(t, Check(declared, results, nil, nil), "pytest pkg/test_a.py::test_missing: declared, and no job executed it")
}

func TestCheckJobs(t *testing.T) {
	report := Check(Declared{}, nil, nil, []Job{
		{Name: "unit-test", Result: "success", Artifact: true},
		{Name: "web-e2e", Result: "failure", Artifact: true},
		{Name: "musr-e2e", Result: "success"},
		{Name: "web-test", Result: "cancelled"},
	})
	assertProblems(t, report,
		"job musr-e2e: uploaded no test-ledger artifact",
		"job web-e2e: ended failure",
		"job web-test: ended cancelled",
		"job web-test: uploaded no test-ledger artifact",
	)
}

func TestCheckReportsEntriesAndSortsViolations(t *testing.T) {
	a, b := goUnit("z", "TestZ"), goUnit("a", "TestA")
	allow := []Entry{entry("go", "z TestZ", "paid")}
	report := Check(Declared{Units: []Unit{a, b}}, nil, allow, nil)
	if !slices.Equal(report.Entries, allow) {
		t.Errorf("the report must carry every entry, got %+v", report.Entries)
	}
	assertProblems(t, report, "go "+mod+"a TestA: declared, and no job executed it")
	if !report.Failed() {
		t.Error("a report with a violation has failed")
	}
	if (Report{}).Failed() {
		t.Error("an empty report has not failed")
	}
}

func TestCheckMatchesByIDAndKeepsSuitesApart(t *testing.T) {
	file := "scripts/a_test.py"
	skipped := Unit{Suite: "python", ID: "a_test.T.test_skip", File: file, Status: Skip}
	other := Unit{Suite: "python", ID: "a_test.T.test_other", File: file, Status: Skip}
	report := Check(Declared{Files: []Unit{fileUnit("python", file)}}, []Unit{skipped, other}, []Entry{
		entry("python", "a_test.T.test_skip", "platform"),
		entry("script", file, "platform"),
	}, nil)
	assertProblems(t, report,
		"allowlist script "+file+": matches no declared test",
		"python a_test.T.test_other: skipped in every job that ran it",
	)
}

func TestCheckCountsAUnitSeenInTwoReportsOnce(t *testing.T) {
	script := Unit{Suite: "script", ID: "scripts/a_test.sh", File: "scripts/a_test.sh", Status: Pass}
	report := Check(Declared{Files: []Unit{fileUnit("script", "scripts/a_test.sh")}}, []Unit{script, script}, nil, nil)
	if c := report.Counts["script"]; *c != (Count{Declared: 1, Executed: 1}) {
		t.Fatalf("one script run in two jobs is one execution: %+v", *c)
	}
}
