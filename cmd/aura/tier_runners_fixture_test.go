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
// carrying a made-up tag would be read as a real tier by TestEveryTestTierHasACIRunnerOrAReason
// and by scripts/tagged_tier_compile.sh, which compiles every tagged file it finds.

const tierFixtureHeader = "# fixture\n"

func tierFixtureViolations(t *testing.T, files map[string]string) (map[string][]string, []string) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var tests []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, "_test.go") {
			rel, _ := filepath.Rel(root, path)
			tests = append(tests, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	used, runners, manual := tierInventory(t, root, tests)
	return runners, tierRunnerViolations(used, runners, manual)
}

func taggedTest(constraint string) string {
	return "//go:build " + constraint + "\n\npackage fixture\n"
}

func TestTierRunnersResolveEveryInvocationForm(t *testing.T) {
	runners, violations := tierFixtureViolations(t, map[string]string{
		".github/workflows/ci.yml": `
jobs:
  unit:
    env:
      GOFLAGS: "-tags=goflags_env"
    steps:
      - uses: actions/checkout@v6
      - run: go test -race -tags "space_a space_b" ./...
      - run: go test -tags=comma_a,comma_b -run 'A|B' ./x
      - run: GOFLAGS=-tags=goflags_inline go test ./x
      - run: |
          if ! make tier; then exit 1; fi
      - run: bash scripts/tier.sh
`,
		"Makefile": `
TIER_TAGS = make_var
.PHONY: tier prereq nested

tier: prereq
	go test -count=1 \
	  -tags $(TIER_TAGS) ./x

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
		manualTiersPath:              tierFixtureHeader,
		"x/space_test.go":            taggedTest("space_a && space_b"),
		"x/comma_test.go":            taggedTest("comma_a || comma_b"),
		"x/goflags_test.go":          taggedTest("goflags_env && goflags_inline"),
		"x/make_test.go":             taggedTest("make_var && make_script"),
		"x/script_test.go":           taggedTest("script_default"),
		"x/toolchain_test.go":        taggedTest("(linux && amd64.v2) || windows || go1.21 || cgo || race || unix || goexperiment.jsonv2"),
		"x/ignored_test.go":          taggedTest("ignore"),
		"x/negated_test.go":          taggedTest("!negated_only"),
		"x/testdata/fixture_test.go": taggedTest("testdata_only"),
		"x/untagged_test.go":         "package fixture\n",
	})
	if len(violations) > 0 {
		t.Fatalf("every tag has a runner, got violations:\n%s", strings.Join(violations, "\n"))
	}
	want := map[string]string{
		"space_a":        "ci.yml unit",
		"comma_b":        "ci.yml unit",
		"goflags_env":    "ci.yml unit",
		"goflags_inline": "ci.yml unit",
		"make_var":       "ci.yml unit -> make tier",
		"make_script":    "ci.yml unit -> make tier -> make prereq -> make nested -> scripts/nested.sh",
		"script_default": "ci.yml unit -> scripts/tier.sh",
	}
	for tag, via := range want {
		if !slices.Contains(runners[tag], via) {
			t.Errorf("tag %s: want runner %q, got %q", tag, via, runners[tag])
		}
	}
	if _, ok := runners["heredoc_text"]; ok {
		t.Error("a heredoc body was read as a shell command")
	}
}

func TestTierRunnersFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name: "a tag with no runner",
			files: map[string]string{
				"x/orphan_test.go": taggedTest("orphan_tier"),
			},
			want: []string{"build tag orphan_tier gates 1 tracked test file(s)"},
		},
		{
			name: "a manual entry whose tag gates nothing",
			files: map[string]string{
				manualTiersPath: tierFixtureHeader + "gone_tier paid, by hand\n",
			},
			want: []string{"gone_tier gates no tracked test file any more"},
		},
		{
			name: "a manual entry whose tag now has a runner",
			files: map[string]string{
				".github/workflows/ci.yml": "jobs:\n  it:\n    steps:\n      - run: go test -tags wired_tier ./x\n",
				manualTiersPath:            tierFixtureHeader + "wired_tier paid, by hand\n",
				"x/wired_test.go":          taggedTest("wired_tier"),
			},
			want: []string{"wired_tier now runs in CI (ci.yml it)"},
		},
		{
			name: "a manual entry without a reason",
			files: map[string]string{
				manualTiersPath:  tierFixtureHeader + "bare_tier\n",
				"x/bare_test.go": taggedTest("bare_tier"),
			},
			want: []string{"bare_tier has no reason"},
		},
		{
			// Compiling a tier, an unresolvable tag value and a tag only a script's own
			// child script passes are not runners.
			name: "what does not count as a runner",
			files: map[string]string{
				".github/workflows/ci.yml": `
jobs:
  it:
    steps:
      - run: go vet -tags vet_only ./x
      - run: go test -run '^$' -tags compile_only ./x
      - run: go test -tags "$1" ./x
      - run: echo make deep && bash scripts/outer.sh
`,
				"scripts/outer.sh":     "bash scripts/inner.sh\n",
				"scripts/inner.sh":     "go test -tags deep_tier ./x\n",
				"x/vet_test.go":        taggedTest("vet_only"),
				"x/compile_test.go":    taggedTest("compile_only"),
				"x/deep_test.go":       taggedTest("deep_tier"),
				"x/positional_test.go": taggedTest("positional_tier"),
			},
			want: []string{"build tag compile_only", "build tag deep_tier", "build tag positional_tier", "build tag vet_only"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.files[manualTiersPath]; !ok {
				tc.files[manualTiersPath] = tierFixtureHeader
			}
			_, violations := tierFixtureViolations(t, tc.files)
			if len(violations) != len(tc.want) {
				t.Fatalf("want %d violations, got %d:\n%s", len(tc.want), len(violations), strings.Join(violations, "\n"))
			}
			for i, want := range tc.want {
				if !strings.Contains(violations[i], want) {
					t.Errorf("violation %d: want it to contain %q, got %q", i, want, violations[i])
				}
			}
		})
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
			if err := os.WriteFile(workflow, []byte("jobs:\n  it:\n    steps:\n      - run: "+run+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := workflowTierRunners(root); err == nil {
				t.Fatalf("%q resolved, want an error naming what is missing", run)
			}
		})
	}
}

func TestManualTiersRejectADuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual.txt")
	if err := os.WriteFile(path, []byte("twice_tier one\ntwice_tier\ttwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readManualTiers(path); err == nil {
		t.Fatal("a tag listed twice was accepted")
	}
}
