package main

import (
	"bytes"
	"cmp"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tier_runners_test.go: every tracked test file behind a feature build tag is executed by
// some CI `go test` invocation — tags that satisfy its constraint, a package list that
// includes it, a -run that selects one of its tests — or manualTiersPath says why not.
// The mcp_live_integration tier was broken from 2026-09-24 to 2026-11-25 and nobody saw
// it, because nothing ran -tags mcp_live_integration; this makes the next such file fail
// here, and so does a tagged file that its tier's -run filter never selects.

const manualTiersPath = ".github/manual_test_tiers.txt"

func TestEveryTaggedTestFileRunsInCIOrHasAReason(t *testing.T) {
	root := repoRootForTest(t)
	list := exec.Command("git", "ls-files", "-z", "--", "*_test.go")
	list.Dir = root
	out, err := list.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var rels []string
	for rel := range bytes.SplitSeq(out, []byte{0}) {
		if len(rel) > 0 {
			rels = append(rels, string(rel))
		}
	}
	violations, table := tierCheck(t, root, rels)
	for _, violation := range violations {
		t.Error(violation)
	}
	for _, row := range table {
		t.Log(row)
	}
}

// tierCheck runs the whole control over a tree and returns its violations and one
// "<file>\t<reached|listed>\t<by what>" row per tagged test file.
func tierCheck(t *testing.T, root string, rels []string) (violations, table []string) {
	t.Helper()
	files, err := tierTestFiles(root, rels, toolchainTagsForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	runs, err := workflowTierRunners(root)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := readManualTiers(filepath.Join(root, manualTiersPath))
	if err != nil {
		t.Fatal(err)
	}
	return tierViolations(files, runs, manual)
}

func tierViolations(files []tierTestFile, runs []goTestRun, manual manualTiers) (violations, table []string) {
	credited := manual.credited(runs)
	byRel := map[string]tierTestFile{}
	byDir := map[string][]tierTestFile{}
	for _, f := range files {
		byRel[f.rel] = f
		byDir[f.dir] = append(byDir[f.dir], f)
	}
	excused := map[string]bool{}
	for _, f := range files {
		if len(f.gating) == 0 {
			continue
		}
		run, reached := reachingRun(f, byDir[f.dir], credited)
		fileReason, fileListed := manual.files[f.rel]
		var tagExcuse string
		for _, tag := range f.gating {
			if _, ok := manual.tags[tag]; ok && !reached {
				excused[tag] = true
				tagExcuse = cmp.Or(tagExcuse, tag)
			}
		}
		switch {
		case reached:
			table = append(table, fmt.Sprintf("%s\treached\t%s", f.rel, run.via))
			if fileListed {
				violations = append(violations, fmt.Sprintf("%s [files] %s is reached by %s: remove it", manualTiersPath, f.rel, run.via))
			}
		case fileListed && tagExcuse != "":
			violations = append(violations, fmt.Sprintf("%s [files] %s is already excused by [tags] %s: keep one", manualTiersPath, f.rel, tagExcuse))
		case fileListed:
			table = append(table, fmt.Sprintf("%s\tlisted (file)\t%s", f.rel, fileReason))
		case tagExcuse != "":
			table = append(table, fmt.Sprintf("%s\tlisted (tag %s)\t%s", f.rel, tagExcuse, manual.tags[tagExcuse]))
		default:
			violations = append(violations, fmt.Sprintf("%s (tags %s) runs in no CI go test: %s. Wire it, or list it in %s with a reason",
				f.rel, strings.Join(f.gating, ","), whyUnreached(f, credited), manualTiersPath))
		}
	}
	return append(violations, manual.staleness(byRel, runs, excused)...), table
}
