package pimwatch

import (
	"fmt"
	"strings"
	"time"
)

// refRunes bounds a ref on a decision's line. It is longer than lineRunes because a Graph id alone
// can pass that, and a cut id corrects nothing.
const refRunes = 1024

// renderDecisions writes a day's verdicts for a tool, one to a line: what was decided and by what,
// the ref a correction takes, then sender and subject. Every field is made plain, so a stranger's
// text cannot forge a line, and the tool's whole result stands in the untrusted frame.
func renderDecisions(lang Lang, loc *time.Location, day DayDecisions) string {
	if len(day.Lines) == 0 {
		return "Nothing was decided on " + day.Day + "."
	}
	p := phrasesFor(lang)
	out := make([]string, 0, len(day.Lines)+1)
	for i, line := range day.Lines {
		decided := fmt.Sprintf("%d. %s by %s", i+1, line.Tier, line.By)
		if label := plainLine(line.Label); label != "" {
			decided += ", label " + label
		}
		ref, _ := line.ref()
		out = append(out, decided+" | "+boundedLine(ref, refRunes)+" | "+line.describe(p, loc))
	}
	if len(day.Lines) >= decisionsLimit {
		out = append(out, fmt.Sprintf("Only the newest %d are listed.", decisionsLimit))
	}
	return strings.Join(out, "\n")
}
