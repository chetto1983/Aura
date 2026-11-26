package testledger

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const workspace = "/home/runner/work/Aura/Aura"

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func render(units []Unit) []string {
	out := make([]string, 0, len(units))
	for _, u := range units {
		out = append(out, u.Suite+" | "+u.ID+" | "+u.File+" | "+string(u.Status))
	}
	return out
}

func assertUnits(t *testing.T, got []Unit, want ...string) {
	t.Helper()
	if !slices.Equal(render(got), want) {
		t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(render(got), "\n  "), strings.Join(want, "\n  "))
	}
}

func TestReadGoKeepsTopLevelTests(t *testing.T) {
	units, err := ReadGo(open(t, "go.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units,
		"go | github.com/chetto1983/aura/scripts/testdata/go_test_ledger/pass TestPass |  | pass",
		"go | github.com/chetto1983/aura/scripts/testdata/go_test_ledger/fail TestFail |  | fail",
		"go | github.com/chetto1983/aura/scripts/testdata/go_test_ledger/skip TestSkip |  | skip",
		"go | github.com/chetto1983/aura/internal/testledger TestBuildConstraint |  | pass",
	)
}

func TestReadGoRefusesWhatIsNotGoTestJSON(t *testing.T) {
	if _, err := ReadGo(strings.NewReader("{\"Action\":\"pass\"}\nFAIL\tpkg\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("a non-JSON line must be an error naming it, got %v", err)
	}
}

func TestReadPlaywright(t *testing.T) {
	units, err := ReadPlaywright(open(t, "playwright.json"), workspace)
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units,
		"playwright | web/e2e/attachment-filename.spec.ts › a chat attachment carries its real name to the object store [chrome] | web/e2e/attachment-filename.spec.ts | fail",
		"playwright | web/e2e/attachment-filename.spec.ts › a chat attachment carries its real name to the object store [mobile-chrome] | web/e2e/attachment-filename.spec.ts | skip",
	)
}

func TestReadPlaywrightNestedSuitesAndStatuses(t *testing.T) {
	report := `{"config":{"rootDir":"/w/web/e2e"},"suites":[{"title":"a.spec.ts","file":"a.spec.ts","specs":[],
	  "suites":[{"title":"group","file":"a.spec.ts","specs":[{"title":"t","tests":[
	    {"projectName":"chrome","status":"expected"},{"projectName":"mobile-chrome","status":"flaky"}]}]}]}]}`
	units, err := ReadPlaywright(strings.NewReader(report), "/w")
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units,
		"playwright | web/e2e/a.spec.ts › group › t [chrome] | web/e2e/a.spec.ts | pass",
		"playwright | web/e2e/a.spec.ts › group › t [mobile-chrome] | web/e2e/a.spec.ts | pass",
	)
	for _, bad := range []string{
		`{"config":{"rootDir":"/w/web/e2e"},"suites":[{"file":"a.spec.ts","specs":[{"title":"t","tests":[{"projectName":"c","status":"interrupted"}]}]}]}`,
		`{"config":{"rootDir":"/elsewhere"},"suites":[{"file":"a.spec.ts","specs":[]}]}`,
		`not json`,
	} {
		if _, err := ReadPlaywright(strings.NewReader(bad), "/w"); err == nil {
			t.Errorf("want an error for %s", bad)
		}
	}
}

func TestReadVitest(t *testing.T) {
	units, err := ReadVitest(open(t, "vitest-mixed.json"), workspace)
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units,
		"vitest | web/src/mixed.test.ts > mixed passes | web/src/mixed.test.ts | pass",
		"vitest | web/src/mixed.test.ts > mixed fails | web/src/mixed.test.ts | fail",
		"vitest | web/src/mixed.test.ts > mixed is skipped | web/src/mixed.test.ts | skip",
		"vitest | web/src/mixed.test.ts > mixed is a todo | web/src/mixed.test.ts | skip",
	)
	units, err = ReadVitest(open(t, "vitest.json"), workspace)
	if err != nil || len(units) != 2 || units[0].File != "packages/create-aura/src/__tests__/cli.test.ts" {
		t.Fatalf("create-aura report: %v, %v", units, err)
	}
	for _, bad := range []string{
		`{"testResults":[{"name":"/w/a.test.ts","assertionResults":[{"fullName":"x","status":"weird"}]}]}`,
		`{"testResults":[{"name":"/other/a.test.ts","assertionResults":[]}]}`,
		`[`,
	} {
		if _, err := ReadVitest(strings.NewReader(bad), "/w"); err == nil {
			t.Errorf("want an error for %s", bad)
		}
	}
}

func TestReadScriptLines(t *testing.T) {
	units, err := ReadScriptLines(open(t, "script-tests.txt"))
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units,
		"script | scripts/testdata/run_script_tests/passes_test.sh | scripts/testdata/run_script_tests/passes_test.sh | pass",
		"script | scripts/testdata/run_script_tests/fails.sh | scripts/testdata/run_script_tests/fails.sh | fail",
		"script | scripts/testdata/run_script_tests/skips.sh | scripts/testdata/run_script_tests/skips.sh | skip",
	)
	for _, bad := range []string{"scripts/x_test.sh pass\n", "scripts/x_test.sh maybe 3\n"} {
		if _, err := ReadScriptLines(strings.NewReader(bad)); err == nil {
			t.Errorf("want an error for %q", bad)
		}
	}
}

func TestReadPythonLines(t *testing.T) {
	units, err := ReadPythonLines(open(t, "python-tests.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 4 {
		t.Fatalf("want the four recorded tests, got %v", render(units))
	}
	if u := units[0]; u.File != "scripts/test_ledger_test.py" || u.Status != Pass ||
		u.ID != "test_ledger_test.AppendTests.test_appends_the_stream_under_its_file_name" {
		t.Errorf("first unit: %+v", u)
	}
	for _, bad := range []string{"test_x.T.test_y\n", "test_x.T.test_y gone\n"} {
		if _, err := ReadPythonLines(strings.NewReader(bad)); err == nil {
			t.Errorf("want an error for %q", bad)
		}
	}
}

func TestReadPytest(t *testing.T) {
	declared, units, err := ReadPytest(open(t, "pytest.collected"), open(t, "pytest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pkg/tests/test_mixed.py::test_passes",
		"pkg/tests/test_mixed.py::test_fails",
		"pkg/tests/test_mixed.py::test_skips",
		"pkg/tests/test_mixed.py::test_param[1]",
		"pkg/tests/test_mixed.py::test_param[2]",
		"pkg/tests/test_mixed.py::TestGroup::test_in_class",
	}
	if !slices.Equal(declared, want) {
		t.Fatalf("declared: %v", declared)
	}
	assertUnits(t, units,
		"pytest | pkg/tests/test_mixed.py::test_passes | pkg/tests/test_mixed.py | pass",
		"pytest | pkg/tests/test_mixed.py::test_fails | pkg/tests/test_mixed.py | fail",
		"pytest | pkg/tests/test_mixed.py::test_skips | pkg/tests/test_mixed.py | skip",
		"pytest | pkg/tests/test_mixed.py::test_param[1] | pkg/tests/test_mixed.py | pass",
		"pytest | pkg/tests/test_mixed.py::test_param[2] | pkg/tests/test_mixed.py | pass",
		"pytest | pkg/tests/test_mixed.py::TestGroup::test_in_class | pkg/tests/test_mixed.py | pass",
	)

	// A JUnit case no collected id names is still reported, under its JUnit name.
	_, units, err = ReadPytest(strings.NewReader(""), strings.NewReader(`<testsuites><testsuite><testcase classname="a.b" name="test_c"><error/></testcase></testsuite></testsuites>`))
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units, "pytest | a.b.test_c |  | fail")
	if _, _, err := ReadPytest(strings.NewReader(""), strings.NewReader("<testsuites>")); err == nil {
		t.Fatal("a truncated JUnit report must be an error")
	}
}

func TestReadPytestKeepsAColonPairInsideTheParameters(t *testing.T) {
	collected := "pkg/test_colon.py::test_param[a::b]\npkg/test_colon.py::TestK::test_in_class[x::y]\n"
	junit := `<testsuites><testsuite>
	  <testcase classname="pkg.test_colon" name="test_param[a::b]"/>
	  <testcase classname="pkg.test_colon.TestK" name="test_in_class[x::y]"/>
	</testsuite></testsuites>`
	_, units, err := ReadPytest(strings.NewReader(collected), strings.NewReader(junit))
	if err != nil {
		t.Fatal(err)
	}
	assertUnits(t, units,
		"pytest | pkg/test_colon.py::test_param[a::b] | pkg/test_colon.py | pass",
		"pytest | pkg/test_colon.py::TestK::test_in_class[x::y] | pkg/test_colon.py | pass",
	)
}
