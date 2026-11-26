package testledger

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Status is what one report says a unit did.
type Status string

// The three outcomes a report can give a unit.
const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Unit is one test as one report saw it. ID is unique within its suite; File is the
// repository-relative file that declares it, where the report says.
type Unit struct {
	Suite  string
	ID     string
	File   string
	Status Status
}

// ReadGo reads a `go test -json` stream (scripts/go_test_ledger.sh). A top-level test is
// "<import path> <TestName>"; subtests are their parent's business and are dropped.
func ReadGo(r io.Reader) ([]Unit, error) {
	var units []Unit
	err := eachLine(r, func(n int, line string) error {
		var event struct{ Action, Package, Test string }
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("line %d is not go test JSON: %w", n, err)
		}
		status := Status(event.Action)
		if event.Test == "" || strings.Contains(event.Test, "/") || (status != Pass && status != Fail && status != Skip) {
			return nil
		}
		units = append(units, Unit{Suite: "go", ID: event.Package + " " + event.Test, Status: status})
		return nil
	})
	return units, err
}

type playwrightSuite struct {
	Title  string            `json:"title"`
	File   string            `json:"file"`
	Specs  []playwrightSpec  `json:"specs"`
	Suites []playwrightSuite `json:"suites"`
}

type playwrightSpec struct {
	Title string `json:"title"`
	Tests []struct {
		ProjectName string `json:"projectName"`
		Status      string `json:"status"`
	} `json:"tests"`
}

var playwrightStatus = map[string]Status{"expected": Pass, "flaky": Pass, "unexpected": Fail, "skipped": Skip}

// ReadPlaywright reads Playwright's JSON report. A unit is a test in one project:
// "<file> › <describe…> › <title> [<project>]".
func ReadPlaywright(r io.Reader, workspace string) ([]Unit, error) {
	var report struct {
		Config struct {
			RootDir string `json:"rootDir"`
		} `json:"config"`
		Suites []playwrightSuite `json:"suites"`
	}
	if err := json.NewDecoder(r).Decode(&report); err != nil {
		return nil, fmt.Errorf("playwright report: %w", err)
	}
	var units []Unit
	var walk func(s playwrightSuite, titles []string) error
	walk = func(s playwrightSuite, titles []string) error {
		file, err := relative(workspace, filepath.Join(report.Config.RootDir, s.File))
		if err != nil {
			return err
		}
		for _, spec := range s.Specs {
			for _, test := range spec.Tests {
				status, ok := playwrightStatus[test.Status]
				if !ok {
					return fmt.Errorf("playwright report: %s: unknown status %q", file, test.Status)
				}
				id := strings.Join(append(append([]string{file}, titles...), spec.Title), " › ") + " [" + test.ProjectName + "]"
				units = append(units, Unit{Suite: "playwright", ID: id, File: file, Status: status})
			}
		}
		for _, child := range s.Suites {
			if err := walk(child, append(slices.Clip(titles), child.Title)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, s := range report.Suites {
		if err := walk(s, nil); err != nil {
			return nil, err
		}
	}
	return units, nil
}

var vitestStatus = map[string]Status{"passed": Pass, "failed": Fail, "skipped": Skip, "pending": Skip, "todo": Skip}

// ReadVitest reads Vitest's JSON reporter output. A unit is "<file> > <full name>".
func ReadVitest(r io.Reader, workspace string) ([]Unit, error) {
	var report struct {
		TestResults []struct {
			Name             string `json:"name"`
			AssertionResults []struct {
				FullName string `json:"fullName"`
				Status   string `json:"status"`
			} `json:"assertionResults"`
		} `json:"testResults"`
	}
	if err := json.NewDecoder(r).Decode(&report); err != nil {
		return nil, fmt.Errorf("vitest report: %w", err)
	}
	var units []Unit
	for _, result := range report.TestResults {
		file, err := relative(workspace, result.Name)
		if err != nil {
			return nil, err
		}
		for _, a := range result.AssertionResults {
			status, ok := vitestStatus[a.Status]
			if !ok {
				return nil, fmt.Errorf("vitest report: %s: unknown status %q", file, a.Status)
			}
			units = append(units, Unit{Suite: "vitest", ID: file + " > " + a.FullName, File: file, Status: status})
		}
	}
	return units, nil
}

// ReadScriptLines reads scripts/run_script_tests.sh's "<path> pass|fail|skip <seconds>" lines.
func ReadScriptLines(r io.Reader) ([]Unit, error) {
	var units []Unit
	err := eachLine(r, func(n int, line string) error {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return fmt.Errorf("script line %d: want <path> <status> <seconds>, got %q", n, line)
		}
		status, err := parseStatus(fields[1])
		if err != nil {
			return fmt.Errorf("script line %d: %w", n, err)
		}
		units = append(units, Unit{Suite: "script", ID: fields[0], File: fields[0], Status: status})
		return nil
	})
	return units, err
}

// ReadPythonLines reads scripts/unittest_ledger.py's "<test id> pass|fail|skip" lines. The id's
// first dotted part is its module under scripts/.
func ReadPythonLines(r io.Reader) ([]Unit, error) {
	var units []Unit
	err := eachLine(r, func(n int, line string) error {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("python line %d: want <test id> <status>, got %q", n, line)
		}
		status, err := parseStatus(fields[1])
		if err != nil {
			return fmt.Errorf("python line %d: %w", n, err)
		}
		module, _, _ := strings.Cut(fields[0], ".")
		units = append(units, Unit{Suite: "python", ID: fields[0], File: "scripts/" + module + ".py", Status: status})
		return nil
	})
	return units, err
}

type junitCase struct {
	Classname string    `xml:"classname,attr"`
	Name      string    `xml:"name,attr"`
	Skipped   *struct{} `xml:"skipped"`
	Failure   *struct{} `xml:"failure"`
	Error     *struct{} `xml:"error"`
}

// ReadPytest reads the node ids `pytest --collect-only -q` printed (the declared set) and the
// JUnit report of the run. A JUnit case is matched to its node id through pytest's classname, the
// module's dotted path plus any class; one no collected id names keeps its JUnit name.
func ReadPytest(collected, junit io.Reader) (declared []string, units []Unit, err error) {
	byKey := map[string]string{}
	err = eachLine(collected, func(_ int, line string) error {
		if strings.Contains(line, "::") {
			declared = append(declared, line)
			byKey[junitKey(line)] = line
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var report struct {
		Suites []struct {
			Cases []junitCase `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.NewDecoder(junit).Decode(&report); err != nil {
		return nil, nil, fmt.Errorf("pytest junit report: %w", err)
	}
	for _, suite := range report.Suites {
		for _, c := range suite.Cases {
			status := Pass
			switch {
			case c.Skipped != nil:
				status = Skip
			case c.Failure != nil || c.Error != nil:
				status = Fail
			}
			key := c.Classname + "." + c.Name
			id, file := key, ""
			if node, ok := byKey[key]; ok {
				id, file = node, strings.SplitN(node, "::", 2)[0]
			}
			units = append(units, Unit{Suite: "pytest", ID: id, File: file, Status: status})
		}
	}
	return declared, units, nil
}

// junitKey turns a node id, path/to/test_x.py::Class::test_y[p], into pytest's JUnit
// classname.name, path.to.test_x.Class.test_y[p]. The [p] suffix stays as pytest writes it in
// both forms, a "::" inside it included (pytest 9.1.1: test_param[a::b] in each).
func junitKey(node string) string {
	names, params, parametrized := strings.Cut(node, "[")
	file, rest, _ := strings.Cut(names, "::")
	key := strings.ReplaceAll(strings.TrimSuffix(file, ".py"), "/", ".") + "." + strings.ReplaceAll(rest, "::", ".")
	if parametrized {
		key += "[" + params
	}
	return key
}

func parseStatus(s string) (Status, error) {
	switch status := Status(s); status {
	case Pass, Fail, Skip:
		return status, nil
	default:
		return "", fmt.Errorf("unknown status %q", s)
	}
}

// relative makes a report's absolute path repository-relative, refusing one outside workspace.
func relative(workspace, abs string) (string, error) {
	rel, err := filepath.Rel(workspace, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the workspace %s", abs, workspace)
	}
	return path.Clean(filepath.ToSlash(rel)), nil
}

func eachLine(r io.Reader, fn func(n int, line string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for n := 1; scanner.Scan(); n++ {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			if err := fn(n, line); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
