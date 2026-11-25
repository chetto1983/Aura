package main

import (
	"slices"
	"strings"
	"testing"
)

// Every way the control must fail, one fixture each. The review findings of 2026-11-25
// are named where a case exists because of one.

func TestTierRunnersFailClosed(t *testing.T) {
	const gate = "ci.yml it -> scripts/gate.sh"
	cases := []struct {
		name  string
		files map[string]string
		want  []string
		row   string
	}{
		{
			name:  "a tagged file no invocation runs",
			files: map[string]string{"x/orphan_test.go": taggedTest("orphan_tier", "TestOrphan")},
			want:  []string{"x/orphan_test.go (tags orphan_tier) runs in no CI go test: no go test invocation sets tags satisfying orphan_tier"},
		},
		{
			name:  "a [tags] entry that excuses nothing",
			files: map[string]string{manualTiersPath: "[tags]\ngone_tier | paid, by hand\n"},
			want:  []string{"[tags] gone_tier excuses no unreached test file"},
		},
		{
			name: "a [tags] entry whose tier now runs",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("go test -tags wired_tier ./x"),
				manualTiersPath:            "[tags]\nwired_tier | paid, by hand\n",
				"x/wired_test.go":          taggedTest("wired_tier", "TestWired"),
			},
			want: []string{"[tags] wired_tier excuses no unreached test file"},
		},
		{
			name: "stale or doubled [files] entries",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("go test -tags wired_tier ./x"),
				manualTiersPath: "[tags]\nmanual_tier | paid\n[files]\nx/wired_test.go | by hand\n" +
					"x/gone_test.go | by hand\nx/manual_test.go | by hand\n",
				"x/wired_test.go":  taggedTest("wired_tier", "TestWired"),
				"x/manual_test.go": taggedTest("manual_tier", "TestManual"),
			},
			want: []string{
				"[files] x/wired_test.go is reached by ci.yml it",
				"[files] x/gone_test.go is not a tracked test file behind a feature tag",
				"[files] x/manual_test.go is already excused by [tags] manual_tier",
			},
		},
		{
			name: "entries without a reason",
			files: map[string]string{
				manualTiersPath:   "[tags]\nbare_tier\n[files]\nx/other_test.go |\n",
				"x/bare_test.go":  taggedTest("bare_tier", "TestBare"),
				"x/other_test.go": taggedTest("other_tier", "TestOther"),
			},
			want: []string{"[tags] bare_tier has no reason", "[files] x/other_test.go has no reason"},
		},
		{
			// I2: `go test` counts only where a command starts. M1: GOFLAGS counts only for a
			// `go test`. Compiling, an unresolvable tag and a script's child script do not run.
			name: "what does not count as a runner",
			files: map[string]string{
				".github/workflows/ci.yml": `on: [push]
jobs:
  it:
    steps:
      - run: go vet -tags vet_only ./x
      - run: go test -run '^$' -tags compile_only ./x
      - run: go test -tags "$1" ./x
      - run: echo go test -tags echo_only ./x
      - run: GOFLAGS=-tags=goflags_vet go vet ./x
      - run: golangci-lint run ./x
        env:
          GOFLAGS: -tags=goflags_lint
      - run: echo make deep && bash scripts/outer.sh
`,
				"scripts/outer.sh":       "bash scripts/inner.sh\n",
				"scripts/inner.sh":       "go test -tags deep_tier ./x\n",
				"x/vet_test.go":          taggedTest("vet_only", "TestVet"),
				"x/compile_test.go":      taggedTest("compile_only", "TestCompile"),
				"x/positional_test.go":   taggedTest("positional_tier", "TestPositional"),
				"x/echo_test.go":         taggedTest("echo_only", "TestEcho"),
				"x/goflags_vet_test.go":  taggedTest("goflags_vet", "TestGoflagsVet"),
				"x/goflags_lint_test.go": taggedTest("goflags_lint", "TestGoflagsLint"),
				"x/deep_test.go":         taggedTest("deep_tier", "TestDeep"),
			},
			want: []string{
				"x/vet_test.go (tags vet_only) runs in no CI go test",
				"x/compile_test.go (tags compile_only) runs in no CI go test: invocations with its tags and package select none",
				"x/positional_test.go (tags positional_tier) runs in no CI go test",
				"x/echo_test.go (tags echo_only) runs in no CI go test",
				"x/goflags_vet_test.go (tags goflags_vet) runs in no CI go test",
				"x/goflags_lint_test.go (tags goflags_lint) runs in no CI go test",
				"x/deep_test.go (tags deep_tier) runs in no CI go test",
			},
		},
		{
			// C2: a tag that runs somewhere does not make every file behind it run.
			name: "a tag that runs, a file that does not",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow(
					"go test -tags run_tier -run 'TestOther|TestMine/sub' ./x",
					"go test -tags pkg_tier ./y",
					"go test -tags half_a ./x",
					`go test -tags pkgvar_tier "./$pkg"`,
					"go test -tags helper_tier -run TestNothing ./x"),
				"x/run_test.go":     taggedTest("run_tier", "TestMine", "TestMineToo"),
				"x/other_test.go":   taggedTest("run_tier", "TestOtherX"),
				"x/miss_test.go":    taggedTest("run_tier", "TestMissed"),
				"x/pkg_test.go":     taggedTest("pkg_tier", "TestPkg"),
				"x/half_test.go":    taggedTest("half_a && half_b", "TestHalf"),
				"x/pkgvar_test.go":  taggedTest("pkgvar_tier", "TestPkgvar"),
				"x/helper_test.go":  taggedTest("helper_tier"),
				"x/sibling_test.go": taggedTest("helper_tier", "TestSibling"),
			},
			want: []string{
				"x/miss_test.go (tags run_tier) runs in no CI go test: invocations with its tags and package select none of its tests with -run",
				"x/pkg_test.go (tags pkg_tier) runs in no CI go test: invocations with its tags never name ./x",
				"x/half_test.go (tags half_a,half_b) runs in no CI go test: no go test invocation sets tags satisfying half_a && half_b",
				"x/pkgvar_test.go (tags pkgvar_tier) runs in no CI go test: invocations with its tags never name ./x",
				"x/helper_test.go (tags helper_tier) runs in no CI go test",
				"x/sibling_test.go (tags helper_tier) runs in no CI go test",
			},
		},
		{
			// C1: the script exits before its go test; the route override strips the credit.
			name: "a route override strips the credit",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("bash scripts/gate.sh"),
				"scripts/gate.sh":          "exit 0\ngo test -tags route_tier ./x\n",
				manualTiersPath:            "[tags]\n[routes]\nroute_tier | " + gate + " | exits 0 first\n",
				"x/route_test.go":          taggedTest("route_tier", "TestRoute"),
			},
			want: []string{"x/route_test.go (tags route_tier) runs in no CI go test"},
		},
		{
			name: "a route override plus a manual reason is green",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("bash scripts/gate.sh"),
				"scripts/gate.sh":          "exit 0\ngo test -tags route_tier ./x\n",
				manualTiersPath:            "[tags]\nroute_tier | needs a real LLM key\n[routes]\nroute_tier | " + gate + " | exits 0 first\n",
				"x/route_test.go":          taggedTest("route_tier", "TestRoute"),
			},
		},
		{
			name: "a route override the route no longer needs",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("bash scripts/gate.sh"),
				"scripts/gate.sh":          "go test -tags other_tier ./x\n",
				manualTiersPath:            "[tags]\n[routes]\nroute_tier | " + gate + " | exits 0 first\n",
				"x/other_test.go":          taggedTest("other_tier", "TestOther"),
			},
			want: []string{"[routes] route_tier via " + gate + ": that route no longer credits the tag"},
		},
		{
			// I1: a step that may not run on a push is no runner unless listed.
			name: "steps and workflows that may not run on a push",
			files: map[string]string{
				".github/workflows/ci.yml": `on: [push]
jobs:
  it:
    steps:
      - if: env.HAS_SECRET == 'true'
        run: go test -tags if_tier ./x
      - continue-on-error: true
        run: go test -tags coe_tier ./x
  gated:
    if: github.ref == 'refs/heads/master'
    steps:
      - run: go test -tags job_if_tier ./x
`,
				".github/workflows/manual.yml":  "on:\n  workflow_dispatch:\njobs:\n  it:\n    steps:\n      - run: go test -tags dispatch_tier ./x\n",
				".github/workflows/release.yml": "on:\n  push:\n    tags: ['v*']\njobs:\n  it:\n    steps:\n      - run: go test -tags release_tier ./x\n",
				"x/if_test.go":                  taggedTest("if_tier", "TestIf"),
				"x/coe_test.go":                 taggedTest("coe_tier", "TestCoe"),
				"x/job_if_test.go":              taggedTest("job_if_tier", "TestJobIf"),
				"x/dispatch_test.go":            taggedTest("dispatch_tier", "TestDispatch"),
				"x/release_test.go":             taggedTest("release_tier", "TestRelease"),
			},
			want: []string{
				"x/if_test.go (tags if_tier) runs in no CI go test",
				"x/coe_test.go (tags coe_tier) runs in no CI go test",
				"x/job_if_test.go (tags job_if_tier) runs in no CI go test",
				"x/dispatch_test.go (tags dispatch_tier) runs in no CI go test",
				"x/release_test.go (tags release_tier) runs in no CI go test",
			},
		},
		{
			// Round 2, I-1: a run accepted through [conditional] says so in the table.
			name: "a listed conditional step counts, a stale one fails",
			files: map[string]string{
				".github/workflows/ci.yml": "on: [push]\njobs:\n  it:\n    steps:\n      - if: env.HAS_SECRET == 'true'\n        run: go test -tags if_tier ./x\n",
				manualTiersPath:            "[tags]\n[conditional]\nif_tier | ci.yml it | needs the bot secret\ngone_tier | ci.yml it | needs the bot secret\n",
				"x/if_test.go":             taggedTest("if_tier", "TestIf"),
			},
			want: []string{"[conditional] gone_tier via ci.yml it: no conditional step there passes it any more"},
			row:  "x/if_test.go\treached (conditional: if: env.HAS_SECRET == 'true')\tci.yml it",
		},
		{
			// Round 2, I-2: [conditional] is keyed by route, so a second conditional step for
			// the same tag is not accepted with the first.
			name: "a conditional entry accepts its own route only",
			files: map[string]string{
				".github/workflows/ci.yml": `on: [push]
jobs:
  a:
    steps:
      - if: env.HAS_SECRET == 'true'
        run: go test -tags c_tier -run TestA ./x
  b:
    steps:
      - if: env.HAS_OTHER == 'true'
        run: go test -tags c_tier -run TestB ./x
`,
				manualTiersPath: "[tags]\n[conditional]\nc_tier | ci.yml a | needs the secret\n",
				"x/c_test.go":   taggedTest("c_tier", "TestA", "TestB"),
			},
			want: []string{"x/c_test.go: TestB is selected by no CI go test, though the file runs through ci.yml a"},
		},
		{
			// Round 2, (a): every test of a reached file must be selected by some run.
			name: "a reached file with a test no -run selects",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("go test -tags ab_tier -run '^(TestA|TestC)$' ./x", "go test -tags ab_tier -run TestC ./x"),
				"x/ab_test.go":             taggedTest("ab_tier", "TestA", "TestB", "TestC"),
			},
			want: []string{"x/ab_test.go: TestB is selected by no CI go test"},
		},
		{
			name: "a [tests] entry excuses one test, stale ones fail",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("go test -tags ab_tier -run '^TestA$' ./x"),
				manualTiersPath: `[tags]
[tests]
x/ab_test.go | TestB | needs a real mailbox
x/ab_test.go | TestA | stale: CI selects it
x/ab_test.go | TestGone | stale: no such test
x/orphan_test.go | TestOrphan | stale: the file is unreached
x/ab_test.go | TestC |
`,
				"x/ab_test.go":     taggedTest("ab_tier", "TestA", "TestB", "TestC"),
				"x/orphan_test.go": taggedTest("orphan_tier", "TestOrphan"),
			},
			want: []string{
				"[tests] x/ab_test.go TestA: CI selects it",
				"[tests] x/ab_test.go TestGone: CI selects it",
				"[tests] x/orphan_test.go TestOrphan: CI selects it",
				"[tests] x/ab_test.go TestC has no reason",
				"x/orphan_test.go (tags orphan_tier) runs in no CI go test",
			},
		},
		{
			// Round 2, (b): a Python driver's go test lists count only as far as they are
			// literal; a driver run through `-m` is not read.
			name: "what a Python driver does not resolve",
			files: map[string]string{
				".github/workflows/ci.yml": ciWorkflow("PYTHONPATH=scripts python3 scripts/driver.py --tier all", "python3 -m unittest scripts/unit.py"),
				"scripts/driver.py": `name = "TestPyVar"
SUITES = [
    ("var", ["go", "test", "-tags=py_var", "-run", name, "./x/"]),
    ("fstring", ["go", "test", "-tags=py_fstring", f"./{name}/"]),
    ("concat", ["go", "test", "-tags=" "py_concat", "./x/"]),
]
`,
				"scripts/unit.py":      "CMD = [\"go\", \"test\", \"-tags=py_unit\", \"./x/\"]\n",
				"x/py_var_test.go":     taggedTest("py_var", "TestPyVar"),
				"x/py_fstring_test.go": taggedTest("py_fstring", "TestPyFstring"),
				"x/py_concat_test.go":  taggedTest("py_concat", "TestPyConcat"),
				"x/py_unit_test.go":    taggedTest("py_unit", "TestPyUnit"),
			},
			want: []string{
				"x/py_var_test.go (tags py_var) runs in no CI go test: invocations with its tags and package select none",
				"x/py_fstring_test.go (tags py_fstring) runs in no CI go test: invocations with its tags never name ./x",
				"x/py_concat_test.go (tags py_concat) runs in no CI go test: no go test invocation sets tags",
				"x/py_unit_test.go (tags py_unit) runs in no CI go test: no go test invocation sets tags",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, table := tierFixture(t, tc.files)
			assertTierViolations(t, violations, tc.want...)
			if tc.row != "" && !slices.Contains(table, tc.row) {
				t.Errorf("want table row %q, got:\n%s", tc.row, strings.Join(table, "\n"))
			}
		})
	}
}
