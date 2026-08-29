// delegation_card.go is the "what the human reads" concern for a finished
// background delegation (51-11, SWARM-12 legs 1+2): the durable record card
// DeliverReport writes to aura.conversation_turns, the exactly-one Telegram
// message shape D-15 locks (single worker AND the N>1 fan-out block), and the
// uncapped report markdown the artifact seam archives. Split out of
// delegation_delivery.go rather than grown inline -- this package's own
// brief.go/report.go/swarm_depth.go/transcript_api.go concern-split precedent
// (CLAUDE.md's NO GOD CLASS rule).
//
// Every rune cap here is counted with utf8.RuneCountInString and cut on a
// rune boundary -- never a byte length -- mirroring internal/arcadedb/browse.go's
// shipped truncateRunes idiom (the same shape, kept package-local rather than
// extracted to a shared package: internal/arcadedb and
// internal/documents/filecard each already keep their own copy for the same
// reason -- no import edge is worth a four-line helper).
package swarm

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// maxCardGoalRunes and maxCardSummaryRunes are the two rune caps UI-SPEC §4
// locks (the summary cap verbatim; the goal cap is the spec's own stated
// default -- "one line, no wrapping" is the locked requirement, 80 is the
// number chosen to satisfy it on a phone). Both the record card and the
// Telegram message share them, so the artifact list's title convention and
// the chip's own goal-line cap agree on what the delegation was "about."
const (
	maxCardGoalRunes    = 80
	maxCardSummaryRunes = 300
)

// delegationClosingLine is the Telegram message's fixed final line (D-15,
// UI-SPEC §4): always present, always last, never varied per outcome.
const delegationClosingLine = "Dettagli nel cockpit."

// capRunes returns s unchanged when it already fits in n runes. Otherwise it
// returns exactly n runes total: the first n-1 runes of s, cut on a rune
// boundary (never a byte index -- a multibyte rune is never split), followed
// by one trailing ellipsis rune. n<=0 returns the empty string.
func capRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// DelegationRecordCard renders the durable conversation-history record for
// one finished (or dead-lettered) worker (51-11, replacing the raw
// []ChildReport JSON Amendment #172 measured landing in
// aura.conversation_turns): a status glyph, the goal on one capped line, the
// child id and elapsed duration, a capped summary/error slot, and -- when
// artifactName is non-empty -- a final line naming the full report artifact.
// With an empty artifactName the artifact line is simply absent; nothing
// else about the card changes (a nil/failed archive degrade, delegation_artifact.go).
func DelegationRecordCard(report ChildReport, elapsed time.Duration, artifactName string) string {
	var b strings.Builder
	b.WriteString(cardGlyph(report.Status))
	b.WriteString(" ")
	b.WriteString(capRunes(report.Goal, maxCardGoalRunes))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s · %s\n", report.ChildID, elapsed.String())
	switch report.Status {
	case StatusDeadLetter:
		fmt.Fprintf(&b, "Non consegnato dopo %d tentativi.\n", report.Attempts)
	case StatusFailed, StatusStalled:
		b.WriteString(capRunes(report.Error, maxCardSummaryRunes))
		b.WriteString("\n")
	default: // StatusOK and any forward-compat status render the summary slot
		b.WriteString(capRunes(report.Summary, maxCardSummaryRunes))
		b.WriteString("\n")
	}
	if artifactName != "" {
		fmt.Fprintf(&b, "Report completo: %s\n", artifactName)
	}
	return strings.TrimRight(b.String(), "\n")
}

// cardGlyph maps a ChildReport status to the SAME glyph vocabulary the
// Telegram message uses (UI-SPEC §4): ✅ ok, ❌ failed, ⏱ stalled (a reap --
// terminal, D-03), ⚠️ dead_letter (retry budget exhausted).
func cardGlyph(status string) string {
	switch status {
	case StatusOK:
		return "✅"
	case StatusFailed:
		return "❌"
	case StatusStalled:
		return "⏱"
	case StatusDeadLetter:
		return "⚠️"
	default:
		return "❌"
	}
}

// fanoutLabel maps a status to the Italian per-line label the N>1 Telegram
// block uses (UI-SPEC §4, verbatim vocabulary).
func fanoutLabel(status string) string {
	switch status {
	case StatusOK:
		return "completato"
	case StatusStalled:
		return "bloccato"
	case StatusDeadLetter:
		return "non consegnato"
	default: // StatusFailed and any forward-compat status render as a failure
		return "fallito"
	}
}

// DelegationReportMarkdown renders the WHOLE consolidated report -- summary
// and error verbatim, uncapped -- under a heading naming the goal, the child
// id, the status and the duration. This is the text the artifact seam
// archives (delegation_artifact.go): the card and the Telegram message carry
// only the bounded summary and a pointer here (UI-SPEC E4 long-text).
func DelegationReportMarkdown(report ChildReport, elapsed time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", report.Goal)
	fmt.Fprintf(&b, "- Worker: %s\n", report.ChildID)
	fmt.Fprintf(&b, "- Stato: %s\n", report.Status)
	fmt.Fprintf(&b, "- Durata: %s\n", elapsed.String())
	if report.Status == StatusDeadLetter {
		fmt.Fprintf(&b, "- Tentativi: %d\n", report.Attempts)
	}
	if report.Summary != "" {
		b.WriteString("\n")
		b.WriteString(report.Summary)
		b.WriteString("\n")
	}
	if report.Error != "" {
		b.WriteString("\n## Errore\n\n")
		b.WriteString(report.Error)
		b.WriteString("\n")
	}
	return b.String()
}

// TelegramDelegationMessage renders the EXACTLY-ONE Telegram message D-15
// locks for a whole fan-out's worth of reports: the three single-worker
// shapes (N=1, UI-SPEC §4's fenced examples, taken verbatim) and the N>1
// fan-out block added 2026-08-29 by the operator's "uno per fan-out"
// decision. A nil or empty slice -- every worker reaped or dead-lettered
// before writing a report at all -- still returns one non-empty message
// naming that, ending in the same closing line: never silence, never the
// empty string.
func TelegramDelegationMessage(reports []ChildReport) string {
	switch len(reports) {
	case 0:
		return fmt.Sprintf("⚠️ Nessun worker ha prodotto un report.\n\n%s", delegationClosingLine)
	case 1:
		return telegramSingleWorkerMessage(reports[0])
	default:
		lines := telegramFanoutLines(reports)
		return strings.Join(lines, "\n") + "\n\n" + delegationClosingLine
	}
}

// telegramSingleWorkerMessage renders the N=1 shape: UI-SPEC §4's three
// fenced examples, verbatim (glyph + capped goal, blank line, capped
// summary/error, blank line, closing line -- the dead-letter shape carries
// no summary paragraph at all, only the goal line and the closing line).
func telegramSingleWorkerMessage(r ChildReport) string {
	goal := capRunes(r.Goal, maxCardGoalRunes)
	switch r.Status {
	case StatusOK:
		return fmt.Sprintf("✅ Worker completato: %s\n\n%s\n\n%s",
			goal, capRunes(r.Summary, maxCardSummaryRunes), delegationClosingLine)
	case StatusDeadLetter:
		return fmt.Sprintf("⚠️ Worker non consegnato dopo %d tentativi: %s\n\n%s",
			r.Attempts, goal, delegationClosingLine)
	default: // StatusFailed and StatusStalled at N=1 both render the failure shape
		body := r.Error
		if body == "" {
			body = r.Summary
		}
		return fmt.Sprintf("❌ Worker fallito: %s\n\n%s\n\n%s",
			goal, capRunes(body, maxCardSummaryRunes), delegationClosingLine)
	}
}

// telegramFanoutLines renders the N>1 status-line block (UI-SPEC §4's
// fan-out block): one "<glyph> <goal>: <label>" line per report, ordered by
// GoalIndex, whose TOTAL is capped at maxCardSummaryRunes runes. The per-line
// goal cap is derived (clamp(300/N-16, 12, 80)), not fixed; if the assembled
// lines still exceed the budget after that, trailing lines are dropped and a
// final "+<M> altri" count line replaces them -- a worker is never silently
// omitted, its existence is always counted (rendered-lines + dropped == N).
func telegramFanoutLines(reports []ChildReport) []string {
	sorted := append([]ChildReport(nil), reports...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].GoalIndex < sorted[j].GoalIndex })

	n := len(sorted)
	goalCap := clampInt(300/n-16, 12, 80)
	lines := make([]string, 0, n)
	for _, r := range sorted {
		lines = append(lines, fmt.Sprintf("%s %s: %s", cardGlyph(r.Status), capRunes(r.Goal, goalCap), fanoutLabel(r.Status)))
	}
	return dropToFanoutBudget(lines, n)
}

// dropToFanoutBudget drops trailing lines (and appends a "+<M> altri" count
// line naming how many) until the joined body fits maxCardSummaryRunes.
// total is the original worker count N, so the count line can always state
// exactly how many were dropped even after the caller's own lines slice has
// shrunk.
func dropToFanoutBudget(lines []string, total int) []string {
	if fanoutBodyRunes(lines) <= maxCardSummaryRunes {
		return lines
	}
	for len(lines) > 0 {
		lines = lines[:len(lines)-1]
		dropped := total - len(lines)
		withCount := append(append([]string(nil), lines...), fmt.Sprintf("+%d altri", dropped))
		if fanoutBodyRunes(withCount) <= maxCardSummaryRunes {
			return withCount
		}
	}
	// Every worker dropped and even the bare count line does not fit --
	// unreachable in practice (a bare "+N altri" line is always short), but
	// never return an empty slice: TelegramDelegationMessage(nil) already
	// handles the true zero-report case, this is a defensive floor.
	return []string{fmt.Sprintf("+%d altri", total)}
}

func fanoutBodyRunes(lines []string) int {
	return utf8.RuneCountInString(strings.Join(lines, "\n"))
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
