package pimwatch

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// maxNames is how many senders a line of a written digest names.
const maxNames = 2

// writtenEntry is one numbered line of a written digest and the items it stands for.
type writtenEntry struct {
	section string
	text    string
	lines   []Line
}

// writtenPhrases is what only a written digest says.
type writtenPhrases struct {
	do, look, know string
	quiet          string // count
	quietOne       string
}

var writtenTexts = map[Lang]writtenPhrases{
	LangIT: {
		do: "Da fare", look: "Da guardare", know: "Da sapere",
		quiet: "Altre %d voci non chiedono nulla.", quietOne: "Un'altra voce non chiede nulla.",
	},
	LangEN: {
		do: "To do", look: "Worth a look", know: "Good to know",
		quiet: "%d more ask nothing of you.", quietOne: "1 more asks nothing of you.",
	},
}

func writtenPhrasesFor(lang Lang) writtenPhrases {
	if w, ok := writtenTexts[lang]; ok {
		return w
	}
	return writtenTexts[LangEN]
}

func (w writtenPhrases) section(section string) string {
	switch section {
	case sectionDo:
		return w.do
	case sectionLook:
		return w.look
	default:
		return w.know
	}
}

// senders names who a line comes from, as the PIM read them and with their address, because the
// name is whatever the sender typed. One address is one person, however its mails name it.
func senders(items []Line) string {
	var names []string
	seen := map[string]int{}
	for _, item := range items {
		who := item.sender()
		key := strings.ToLower(plainLine(item.From))
		if key == "" {
			key = strings.ToLower(who)
		}
		if key == "" {
			continue
		}
		if at, ok := seen[key]; ok {
			if len(who) > len(names[at]) {
				names[at] = who
			}
			continue
		}
		seen[key] = len(names)
		names = append(names, who)
	}
	if len(names) > maxNames {
		return fmt.Sprintf("%s +%d", strings.Join(names[:maxNames], ", "), len(names)-maxNames)
	}
	return strings.Join(names, ", ")
}

// signed puts in front of the model's words who a line comes from, and keeps the mark of a sender
// that failed authentication. Both come from Aura, so no subject can change them.
func signed(p phrases, items []Line, text string) string {
	if names := senders(items); names != "" {
		text = names + ": " + text
	}
	if slices.ContainsFunc(items, func(item Line) bool { return item.AuthFailed }) {
		text += " [" + p.authFailed + "]"
	}
	return text
}

// writtenEntries puts what is owed first in its section, up to the cap, then the model's lines,
// and counts what the model left out of known. A told item it left out is nobody's business.
func writtenEntries(p phrases, loc *time.Location, known, owed []Line, written []writtenLine) (entries []writtenEntry, quiet, stopped int) {
	shown := owed[:min(len(owed), maxDigestLines)]
	entries = make([]writtenEntry, 0, len(shown)+len(written))
	for _, line := range shown {
		entries = append(entries, writtenEntry{section: sectionLook, text: line.describe(p, loc), lines: []Line{line}})
	}
	cited := make(map[int]bool, len(known))
	for _, line := range written {
		items := make([]Line, 0, len(line.Items))
		for _, number := range line.Items {
			cited[number] = true
			items = append(items, known[number-1])
		}
		entries = append(entries, writtenEntry{section: line.Section, text: signed(p, items, line.Text), lines: items})
	}
	for i, line := range known {
		if !cited[i+1] && !line.Told {
			quiet++
		}
	}
	slices.SortStableFunc(entries, func(a, b writtenEntry) int {
		return slices.Index(writtenSections, a.section) - slices.Index(writtenSections, b.section)
	})
	return entries, quiet, len(owed) - len(shown)
}

// renderWritten writes the digest the model answered. known are the items as it read them, owed
// what it must not touch, and unseen counts what it never read.
func renderWritten(lang Lang, loc *time.Location, known, owed []Line, written []writtenLine, unseen int, missing unread, discarded int) Outcome {
	p, w := phrasesFor(lang), writtenPhrasesFor(lang)
	entries, quiet, stopped := writtenEntries(p, loc, known, owed, written)
	unseen += stopped
	if len(entries) == 0 && quiet == 0 && unseen == 0 && missing.none() {
		return Outcome{}
	}
	body := make([]string, 0, 2*len(entries))
	groups := make([][]Line, 0, len(entries))
	heading := ""
	for i, entry := range entries {
		if title := w.section(entry.section); title != heading {
			heading = title
			body = append(body, title)
		}
		body = append(body, fmt.Sprintf("%d. %s", i+1, entry.text))
		groups = append(groups, entry.lines)
	}
	var tail []string
	switch {
	case quiet == 1:
		tail = append(tail, w.quietOne)
	case quiet > 1:
		tail = append(tail, fmt.Sprintf(w.quiet, quiet))
	}
	tail = append(tail, digestTail(p, unseen, missing, discarded)...)
	refs, quoted := recordParts(groups, body)
	return Outcome{
		Notice: strings.Join(append(append([]string{p.digestTitle}, body...), tail...), "\n"),
		Head:   strings.Join(append([]string{p.digestTitle}, tail...), "\n"),
		Refs:   refs,
		Quoted: quoted,
	}
}
