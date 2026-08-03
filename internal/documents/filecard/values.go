package filecard

import (
	"fmt"
	"sort"
	"strings"
)

// The card has ONE rule for choosing what to keep when a file holds more than
// fits, and it is frequency — Postgres' own most_common_vals, as the package
// comment explains. It governs a spreadsheet column's values and a document's
// recurring words alike, so it lives here rather than in either.

// topValues keeps the most frequent counted values and drops anything below
// floor. Ties break on the text, so two builds of the same file render the same
// card and a test can assert on it.
func topValues(counts map[string]int, floor, limit int) []Value {
	values := make([]Value, 0, len(counts))
	for text, count := range counts {
		if count < floor {
			continue
		}
		values = append(values, Value{Text: text, Count: count})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		return values[i].Text < values[j].Text
	})
	if len(values) > limit {
		values = values[:limit]
	}
	return values
}

// joinValues renders frequent values the one way a card renders them.
func joinValues(values []Value) string {
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, fmt.Sprintf("%s (%d)", value.Text, value.Count))
	}
	return strings.Join(rendered, ", ")
}
