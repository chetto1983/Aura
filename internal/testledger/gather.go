package testledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The files each JavaScript suite runs, as its config includes them: web/playwright.config.ts
// (testDir ./e2e, Playwright's default testMatch), web/vitest.config.ts (src/**/*.{test,spec}.{ts,tsx})
// and packages/create-aura's Vitest defaults.
var (
	playwrightFiles = regexp.MustCompile(`^web/e2e/.*\.(spec|test)\.[cm]?[jt]sx?$`)
	vitestFiles     = regexp.MustCompile(`^(web/src/.*\.(test|spec)\.tsx?|packages/create-aura/.*\.(test|spec)\.[cm]?[jt]sx?)$`)
	pythonFiles     = regexp.MustCompile(`^scripts/.*_test\.py$`)
	moduleLine      = regexp.MustCompile(`(?m)^module\s+(\S+)`)
)

// DeclareTree reads what the tree at root declares, from `git ls-files`: every Go test from source,
// and the files of the suites declared by file. The shell self-tests are what
// scripts/run_script_tests.sh --list discovers, so discovery has one definition.
func DeclareTree(root string) (Declared, error) {
	listed, err := output(root, exec.Command("git", "ls-files", "-z"))
	if err != nil {
		return Declared{}, err
	}
	tracked := strings.Split(strings.TrimSuffix(listed, "\x00"), "\x00")
	discovered, err := output(root, exec.Command("bash", "scripts/run_script_tests.sh", "--list"))
	if err != nil {
		return Declared{}, err
	}
	tree, err := os.OpenRoot(root)
	if err != nil {
		return Declared{}, err
	}
	defer func() { _ = tree.Close() }()
	modules := map[string]string{}
	var goTests []string
	var declared Declared
	for _, rel := range tracked {
		goTool := !IgnoredByGoTool(rel) // the go tool's rule; Vitest runs src/__tests__ all the same
		switch {
		case goTool && path.Base(rel) == "go.mod":
			data, err := tree.ReadFile(filepath.FromSlash(rel))
			if err != nil {
				return Declared{}, err
			}
			if m := moduleLine.FindSubmatch(data); m != nil {
				modules[path.Dir(rel)] = string(m[1])
			}
		case goTool && strings.HasSuffix(rel, "_test.go"):
			goTests = append(goTests, rel)
		case playwrightFiles.MatchString(rel):
			declared.Files = append(declared.Files, Unit{Suite: "playwright", ID: rel, File: rel})
		case vitestFiles.MatchString(rel) && !strings.Contains(rel, "/node_modules/"):
			declared.Files = append(declared.Files, Unit{Suite: "vitest", ID: rel, File: rel})
		case pythonFiles.MatchString(rel) && !strings.Contains(rel, "/testdata/"):
			declared.Files = append(declared.Files, Unit{Suite: "python", ID: rel, File: rel})
		}
	}
	for rel := range strings.FieldsSeq(discovered) {
		declared.Files = append(declared.Files, Unit{Suite: "script", ID: rel, File: rel})
	}
	files, err := DeclaredGo(root, goTests)
	if err != nil {
		return Declared{}, err
	}
	seen := map[string]bool{}
	for _, f := range files {
		for _, test := range f.Tests {
			id := importPath(modules, f.Dir) + " " + test
			// One name in two files of a package is one test (x_linux_test.go, x_windows_test.go).
			if !seen[id] {
				seen[id] = true
				declared.Units = append(declared.Units, Unit{Suite: "go", ID: id, File: f.Rel})
			}
		}
	}
	return declared, nil
}

// importPath resolves a package directory against the nearest enclosing module.
func importPath(modules map[string]string, dir string) string {
	for d := dir; ; d = path.Dir(d) {
		if module, ok := modules[d]; ok {
			if d == dir {
				return module
			}
			return module + "/" + strings.TrimPrefix(dir, strings.TrimSuffix(d, ".")+"/")
		}
		if d == "." {
			return dir
		}
	}
}

// Artifacts is what the test jobs uploaded: every result, the pytest ids their collection declared,
// and the jobs whose test-ledger artifact arrived.
type Artifacts struct {
	Results []Unit
	Pytest  []Unit
	Arrived map[string]bool
}

// LoadArtifacts reads dir, where each job's artifact is a test-ledger-<job> directory. A file no
// reader knows is an error: a format the ledger cannot read is a run it cannot judge.
func LoadArtifacts(dir, workspace string) (Artifacts, error) {
	jobs, err := os.ReadDir(dir)
	if err != nil {
		return Artifacts{}, err
	}
	arts := Artifacts{Arrived: map[string]bool{}}
	for _, job := range jobs {
		name, ok := strings.CutPrefix(job.Name(), "test-ledger-")
		if !ok || !job.IsDir() {
			return Artifacts{}, fmt.Errorf("%s: want only test-ledger-<job> directories in %s", job.Name(), dir)
		}
		arts.Arrived[name] = true
		if err := arts.loadJob(filepath.Join(dir, job.Name()), workspace); err != nil {
			return Artifacts{}, fmt.Errorf("%s: %w", job.Name(), err)
		}
	}
	return arts, nil
}

func (a *Artifacts) loadJob(dir, workspace string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	job, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = job.Close() }()
	for _, f := range files {
		name := f.Name()
		var units []Unit
		err := withFile(job, name, func(file *os.File) (err error) {
			switch {
			case strings.HasSuffix(name, ".jsonl"):
				units, err = ReadGo(file)
			case strings.HasPrefix(name, "playwright-") && strings.HasSuffix(name, ".json"):
				units, err = ReadPlaywright(file, workspace)
			case strings.HasPrefix(name, "vitest-") && strings.HasSuffix(name, ".json"):
				units, err = ReadVitest(file, workspace)
			case name == "script-tests.txt":
				units, err = ReadScriptLines(file)
			case name == "python-tests.txt":
				units, err = ReadPythonLines(file)
			case strings.HasPrefix(name, "pytest-") && strings.HasSuffix(name, ".collected"):
				return withFile(job, strings.TrimSuffix(name, ".collected")+".xml", func(junit *os.File) error {
					declared, results, err := ReadPytest(file, junit)
					for _, id := range declared {
						a.Pytest = append(a.Pytest, Unit{Suite: "pytest", ID: id, File: strings.SplitN(id, "::", 2)[0]})
					}
					units = results
					return err
				})
			case strings.HasPrefix(name, "pytest-") && strings.HasSuffix(name, ".xml"):
				if _, err := job.Stat(strings.TrimSuffix(name, ".xml") + ".collected"); err != nil {
					return fmt.Errorf("%s has no .collected beside it, so its ids are not declared", name)
				}
			default:
				return fmt.Errorf("%s is no format the test ledger reads", name)
			}
			return err
		})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		a.Results = append(a.Results, units...)
	}
	return nil
}

func withFile(dir *os.Root, name string, fn func(*os.File) error) error {
	f, err := dir.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return fn(f)
}

// JobsFromNeeds reads the workflow's `toJSON(needs)`: every job the gate waits for, with its result.
func JobsFromNeeds(needs []byte, arrived map[string]bool) ([]Job, error) {
	var parsed map[string]struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(needs, &parsed); err != nil {
		return nil, fmt.Errorf("needs: %w", err)
	}
	jobs := make([]Job, 0, len(parsed))
	for _, name := range slices.Sorted(maps.Keys(parsed)) {
		jobs = append(jobs, Job{Name: name, Result: parsed[name].Result, Artifact: arrived[name]})
	}
	return jobs, nil
}

func output(dir string, cmd *exec.Cmd) (string, error) {
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", errors.Join(fmt.Errorf("%s: %w", strings.Join(cmd.Args, " "), err), errors.New(stderr.String()))
	}
	return string(out), nil
}
