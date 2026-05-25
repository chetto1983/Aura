package tools

import (
	"fmt"
	"unicode/utf8"
)

const (
	FreshToolResultBudgetBytes         = 16 * 1024
	FreshEvidenceToolResultBudgetBytes = 32 * 1024
	FreshToolResultTrailerReserveBytes = 256
)

type FreshToolResultBudgetOutcome struct {
	OriginalBytes int
	FinalBytes    int
	DroppedBytes  int
	Truncated     bool
}

func ApplyFreshToolResultBudget(raw string, budgetBytes int) (string, FreshToolResultBudgetOutcome) {
	originalBytes := len(raw)
	if budgetBytes <= 0 || originalBytes <= budgetBytes {
		return raw, FreshToolResultBudgetOutcome{
			OriginalBytes: originalBytes,
			FinalBytes:    originalBytes,
		}
	}

	headCapacity := budgetBytes - FreshToolResultTrailerReserveBytes
	if headCapacity < 1 {
		headCapacity = 1
	}
	head := safeBudgetHead(raw, headCapacity)
	droppedBytes := originalBytes - len(head)
	trailer := fmt.Sprintf(
		"\n\n[... %d bytes truncated by tool_result_budget - re-run with a narrower query to see the rest ...]",
		droppedBytes,
	)
	out := head + trailer
	return out, FreshToolResultBudgetOutcome{
		OriginalBytes: originalBytes,
		FinalBytes:    len(out),
		DroppedBytes:  droppedBytes,
		Truncated:     true,
	}
}

func safeBudgetHead(raw string, maxBytes int) string {
	if len(raw) <= maxBytes {
		return raw
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	if cut == 0 {
		_, size := utf8.DecodeRuneInString(raw)
		cut = size
	}
	return raw[:cut]
}
