package testledger

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

// Entry is one line of .github/test_ledger_allow.txt: "<suite> | <selector> | <class> | <reason>".
// The selector names a unit by its id or its file; a Go one is "<package dir> <TestName>" or a test
// file, and a Playwright file may end in " [<project>]" (spec §4.5).
type Entry struct {
	Suite, Selector, Class, Reason string
	Line                           int
}

// The closed set of reasons a declared test may not run in CI; there is no "pending" (spec §4.5).
var classes = []string{"paid", "outward", "measure", "platform"}

var suites = []string{"go", "playwright", "vitest", "pytest", "python", "script"}

// ParseAllow reads the allowlist; a malformed line, an unknown class or suite, or an entry
// listed twice is an error.
func ParseAllow(r io.Reader) ([]Entry, error) {
	var entries []Entry
	seen := map[string]int{}
	err := eachLine(r, func(n int, line string) error {
		if strings.HasPrefix(line, "#") {
			return nil
		}
		fields := strings.Split(line, "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		switch {
		case len(fields) != 4:
			return fmt.Errorf("allowlist line %d: want <suite> | <selector> | <class> | <reason>", n)
		case !slices.Contains(suites, fields[0]):
			return fmt.Errorf("allowlist line %d: unknown suite %q", n, fields[0])
		case fields[1] == "":
			return fmt.Errorf("allowlist line %d: empty selector", n)
		case !slices.Contains(classes, fields[2]):
			return fmt.Errorf("allowlist line %d: class %q is not one of %s", n, fields[2], strings.Join(classes, ", "))
		case fields[3] == "":
			return fmt.Errorf("allowlist line %d: no reason", n)
		}
		key := fields[0] + " " + fields[1]
		if first, ok := seen[key]; ok {
			return fmt.Errorf("allowlist line %d: %s is already listed on line %d", n, key, first)
		}
		seen[key] = n
		entries = append(entries, Entry{Suite: fields[0], Selector: fields[1], Class: fields[2], Reason: fields[3], Line: n})
		return nil
	})
	return entries, err
}
