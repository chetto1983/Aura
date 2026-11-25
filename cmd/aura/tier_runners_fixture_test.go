package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The fixtures are written to t.TempDir, not kept under testdata: a tracked *_test.go
// carrying a made-up tag would be read as a real tier by TestEveryTaggedTestFileRunsInCIOrHasAReason
// and by scripts/tagged_tier_compile.sh, which compiles every tagged file it finds.

const tierFixtureList = "[tags]\n"

func tierFixture(t *testing.T, files map[string]string) (violations, table []string) {
	t.Helper()
	root := t.TempDir()
	if _, ok := files[manualTiersPath]; !ok {
		files[manualTiersPath] = tierFixtureList
	}
	for rel, body := range files {
		file := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var rels []string
	err := filepath.WalkDir(root, func(file string, _ fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(file, "_test.go") {
			rel, _ := filepath.Rel(root, file)
			rels = append(rels, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tierCheck(t, root, rels)
}

// taggedTest is a test file behind constraint declaring the named TestXxx functions.
func taggedTest(constraint string, tests ...string) string {
	src := "package fixture\n"
	if constraint != "" {
		src = "//go:build " + constraint + "\n\n" + src
	}
	for _, name := range tests {
		src += "\nfunc " + name + "(t *testing.T) {}\n"
	}
	return src
}

func ciWorkflow(steps ...string) string {
	return "on: [push]\njobs:\n  it:\n    steps:\n      - run: " + strings.Join(steps, "\n      - run: ") + "\n"
}

func assertTierViolations(t *testing.T, violations []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.ContainsFunc(violations, func(v string) bool { return strings.Contains(v, w) }) {
			t.Errorf("want a violation containing %q", w)
		}
	}
	if len(violations) != len(want) {
		t.Errorf("want %d violations, got %d:\n%s", len(want), len(violations), strings.Join(violations, "\n"))
	}
}

func TestTierRunnersResolveEveryInvocationForm(t *testing.T) {
	violations, table := tierFixture(t, map[string]string{
		".github/workflows/ci.yml": `
on:
  push:
    branches: [master]
env:
  WORKFLOW_TAGS: workflow_env
jobs:
  unit:
    env:
      GOFLAGS: "-tags=goflags_env"
      JOB_TAGS: job_env
    steps:
      - uses: actions/checkout@v6
      - run: go test -race -tags "space_a space_b" ./x/...
      - run: go test -tags=comma_a,comma_b -run 'TestComma|TestNone' github.com/fixture/mod/x
      - run: GOFLAGS=-tags=goflags_inline go test ./x
      - run: go test ./x
      - run: go test -tags "$WORKFLOW_TAGS $JOB_TAGS $STEP_TAGS" ./x
        env:
          STEP_TAGS: step_env
      - run: |
          if ! make tier; then exit 1; fi
      - run: bash scripts/tier.sh
      - if: ${{ !cancelled() }}
        run: go test -tags always_step ./x
`,
		"go.mod": "module github.com/fixture/mod\n",
		"Makefile": `
TIER_TAGS = make_var
TIER_TAGS += make_append
.PHONY: tier prereq nested

tier: prereq
	go test -count=1 \
	  -tags "$(TIER_TAGS)" ./x

prereq:
	$(MAKE) nested

nested:
	@bash scripts/nested.sh
`,
		"scripts/tier.sh": `#!/usr/bin/env bash
TAGS="${FIXTURE_TAGS:-script_default}"
cat <<'PY'
go test -tags heredoc_text ./x
PY
go test -tags "${TAGS}" ./x
`,
		"scripts/nested.sh":          "go test -tags=make_script ./x\n",
		"x/space_test.go":            taggedTest("space_a && space_b", "TestSpace"),
		"x/sub/deep_test.go":         taggedTest("space_a", "TestDeep"),
		"x/comma_test.go":            taggedTest("comma_a && comma_b", "TestComma"),
		"x/goflags_env_test.go":      taggedTest("goflags_env", "TestGoflagsEnv"),
		"x/goflags_inline_test.go":   taggedTest("goflags_inline", "TestGoflagsInline"),
		"x/env_test.go":              taggedTest("workflow_env && job_env && step_env", "TestEnv"),
		"x/make_test.go":             taggedTest("make_var && make_append", "TestMake"),
		"x/nested_test.go":           taggedTest("make_script", "TestNested"),
		"x/script_test.go":           taggedTest("script_default", "TestScript"),
		"x/always_test.go":           taggedTest("always_step", "TestAlways"),
		"x/helpers_test.go":          taggedTest("comma_a"),
		"x/toolchain_test.go":        taggedTest("(linux && amd64.v2) || windows || go1.21 || cgo || race || unix || goexperiment.jsonv2", "TestToolchain"),
		"x/ignored_test.go":          taggedTest("ignore", "TestIgnored"),
		"x/negated_test.go":          taggedTest("!negated_only", "TestNegated"),
		"x/testdata/fixture_test.go": taggedTest("testdata_only", "TestFixture"),
		"x/untagged_test.go":         taggedTest("", "TestUntagged"),
	})
	assertTierViolations(t, violations)
	want := map[string]string{
		"x/space_test.go":          "ci.yml unit",
		"x/sub/deep_test.go":       "ci.yml unit",
		"x/comma_test.go":          "ci.yml unit",
		"x/goflags_env_test.go":    "ci.yml unit",
		"x/goflags_inline_test.go": "ci.yml unit",
		"x/env_test.go":            "ci.yml unit",
		"x/make_test.go":           "ci.yml unit -> make tier",
		"x/nested_test.go":         "ci.yml unit -> make tier -> make prereq -> make nested -> scripts/nested.sh",
		"x/script_test.go":         "ci.yml unit -> scripts/tier.sh",
		"x/always_test.go":         "ci.yml unit",
		"x/helpers_test.go":        "ci.yml unit",
	}
	for rel, via := range want {
		if !slices.Contains(table, rel+"\treached\t"+via) {
			t.Errorf("%s: want it reached by %q, table:\n%s", rel, via, strings.Join(table, "\n"))
		}
	}
	if len(table) != len(want) {
		t.Errorf("want %d tagged files, got %d:\n%s", len(want), len(table), strings.Join(table, "\n"))
	}
}

func TestTierRunnersRejectAMissingScriptOrTarget(t *testing.T) {
	for name, run := range map[string]string{"script": "bash scripts/missing.sh", "make target": "make missing"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
			if err := os.MkdirAll(filepath.Dir(workflow), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(workflow, []byte(ciWorkflow(run)), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := workflowTierRunners(root); err == nil {
				t.Fatalf("%q resolved, want an error naming what is missing", run)
			}
		})
	}
}

func TestManualTiersRejectAMalformedList(t *testing.T) {
	for name, list := range map[string]string{
		"duplicate":       "[tags]\ntwice_tier | one\ntwice_tier | two\n",
		"unknown section": "[tiers]\nsome_tier | reason\n",
		"no section":      "some_tier | reason\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "manual.txt")
			if err := os.WriteFile(file, []byte(list), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readManualTiers(file); err == nil {
				t.Fatalf("accepted:\n%s", list)
			}
		})
	}
}
