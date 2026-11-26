package testledger

import (
	"cmp"
	"path"
	"slices"
	"strings"
)

// Declared is what the tree says must run. Units are declared one by one (Go tests from source,
// pytest ids from its collection); Files are declared by file (Playwright, Vitest, Python and shell
// self-tests), the report then being the inventory inside a file (spec §4.1).
type Declared struct {
	Units []Unit
	Files []Unit
}

// Job is one test job the gate needs: its result and whether its test-ledger artifact arrived.
type Job struct {
	Name     string
	Result   string
	Artifact bool
}

// Violation is one reason the ledger is red.
type Violation struct {
	Suite, Subject, Problem string
}

// Count is one suite's tally: declared units or files, and the units executed, skipped with no
// excuse, and excused by the allowlist.
type Count struct {
	Declared, Executed, Skipped, Excused int
}

// Report is the gate's verdict: the counts, every allowlist entry, and every violation, sorted.
type Report struct {
	Counts     map[string]*Count
	Entries    []Entry
	Violations []Violation
}

// Failed reports whether the ledger is red.
func (r Report) Failed() bool { return len(r.Violations) > 0 }

type checker struct {
	report   Report
	allow    []Entry
	used     []bool
	executed map[string]bool
	skipped  map[string]bool
	byFile   map[string][]Unit
}

// Check judges results against declared, the allowlist and the jobs (spec §4.3). A unit is executed
// when any report has it pass or fail; union across jobs.
func Check(declared Declared, results []Unit, allow []Entry, jobs []Job) Report {
	c := checker{
		report:   Report{Counts: map[string]*Count{}, Entries: allow},
		allow:    allow,
		used:     make([]bool, len(allow)),
		executed: map[string]bool{},
		skipped:  map[string]bool{},
		byFile:   map[string][]Unit{},
	}
	for _, u := range results {
		k := key(u.Suite, u.ID)
		if u.Status == Skip {
			c.skipped[k] = true
		} else {
			c.executed[k] = true
		}
		if f := key(u.Suite, u.File); u.File != "" && !slices.ContainsFunc(c.byFile[f], func(v Unit) bool { return v.ID == u.ID }) {
			c.byFile[f] = append(c.byFile[f], u)
		}
	}
	for _, u := range declared.Units {
		c.count(u.Suite).Declared++
		c.judge(u, "declared, and no job executed it")
	}
	for _, f := range declared.Files {
		c.count(f.Suite).Declared++
		units := c.byFile[key(f.Suite, f.File)]
		if len(units) == 0 {
			c.judge(f, "in no report: no job ran this file")
			continue
		}
		for _, u := range units {
			c.judge(u, "")
		}
	}
	for i, e := range allow {
		if !c.used[i] {
			c.violate("allowlist", e.Suite+" "+e.Selector, "matches no declared test that a job did not execute")
		}
	}
	for _, j := range jobs {
		if j.Result != "success" {
			c.violate("job", j.Name, "ended "+j.Result)
		}
		if !j.Artifact {
			c.violate("job", j.Name, "uploaded no test-ledger artifact")
		}
	}
	slices.SortFunc(c.report.Violations, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Suite, b.Suite), cmp.Compare(a.Subject, b.Subject), cmp.Compare(a.Problem, b.Problem))
	})
	return c.report
}

// judge places one unit: executed, excused, skipped with no excuse, or missing.
func (c *checker) judge(u Unit, missing string) {
	k := key(u.Suite, u.ID)
	switch {
	case c.executed[k]:
		c.count(u.Suite).Executed++
		for i, e := range c.allow {
			if matches(e, u) {
				c.used[i] = true
				c.violate("allowlist", e.Suite+" "+e.Selector, "excuses "+u.ID+", which a job executed")
			}
		}
	case c.excuse(u):
		c.count(u.Suite).Excused++
	case c.skipped[k]:
		c.count(u.Suite).Skipped++
		c.violate(u.Suite, u.ID, "skipped in every job that ran it, and no allowlist entry excuses it")
	default:
		c.violate(u.Suite, u.ID, missing)
	}
}

func (c *checker) excuse(u Unit) bool {
	excused := false
	for i, e := range c.allow {
		if matches(e, u) {
			c.used[i] = true
			excused = true
		}
	}
	return excused
}

func (c *checker) count(suite string) *Count {
	if c.report.Counts[suite] == nil {
		c.report.Counts[suite] = &Count{}
	}
	return c.report.Counts[suite]
}

func (c *checker) violate(suite, subject, problem string) {
	if !slices.Contains(c.report.Violations, Violation{suite, subject, problem}) {
		c.report.Violations = append(c.report.Violations, Violation{suite, subject, problem})
	}
}

// matches reports whether an allowlist entry names u: by id, by file, by "<file> [<project>]"
// for Playwright, or by "<package dir> <TestName>" for Go.
func matches(e Entry, u Unit) bool {
	if e.Suite != u.Suite {
		return false
	}
	if e.Selector == u.ID || (u.File != "" && e.Selector == u.File) {
		return true
	}
	switch u.Suite {
	case "playwright":
		if i := strings.LastIndex(u.ID, " ["); i >= 0 {
			return e.Selector == u.File+u.ID[i:]
		}
	case "go":
		if _, test, ok := strings.Cut(u.ID, " "); ok && u.File != "" {
			return e.Selector == path.Dir(u.File)+" "+test
		}
	}
	return false
}

func key(suite, id string) string { return suite + "\x00" + id }
