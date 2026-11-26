package testledger

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Summary renders a report as Markdown: the counts per suite, every allowlist entry with its class
// and reason, and the violations (spec §4.3), at most limit of them when limit > 0. GitHub caps a
// step summary at 1 MiB, so the gate writes a limited one there and the whole list to its log.
func Summary(r Report, reportOnly bool, limit int) string {
	var b strings.Builder
	title := "## Test ledger"
	if reportOnly {
		title += " (report-only)"
	}
	b.WriteString(title + "\n\n| suite | declared | executed | skipped | excused |\n|---|---|---|---|---|\n")
	for _, suite := range slices.Sorted(maps.Keys(r.Counts)) {
		c := r.Counts[suite]
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", suite, c.Declared, c.Executed, c.Skipped, c.Excused)
	}
	b.WriteString("\nGo and pytest count tests. Playwright, Vitest, Python and shell self-tests are declared by\n" +
		"file, so `declared` counts files and the other columns count the tests their reports list.\n")
	fmt.Fprintf(&b, "\n### Allowlist (%d)\n\n", len(r.Entries))
	if len(r.Entries) > 0 {
		b.WriteString("| suite | selector | class | reason |\n|---|---|---|---|\n")
		for _, e := range r.Entries {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(e.Suite), cell(e.Selector), cell(e.Class), cell(e.Reason))
		}
	}
	fmt.Fprintf(&b, "\n### Violations (%d)\n\n", len(r.Violations))
	for i, v := range r.Violations {
		if limit > 0 && i == limit {
			fmt.Fprintf(&b, "\n…and %d more: the job log lists every one.\n", len(r.Violations)-limit)
			break
		}
		fmt.Fprintf(&b, "- `%s` %s: %s\n", v.Suite, cell(v.Subject), v.Problem)
	}
	return b.String()
}

// cell keeps a value from breaking a Markdown table row.
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
