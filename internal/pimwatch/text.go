package pimwatch

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Lang is the language of the watch's fixed text: the identity's own, else English.
type Lang string

// The two languages the first release writes (spec §9).
const (
	LangIT Lang = "it"
	LangEN Lang = "en"
)

// ResolveLang reads a profile's free-text language.
func ResolveLang(raw string) Lang {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "it") {
		return LangIT
	}
	return LangEN
}

// phrases is every fixed sentence the watch writes.
type phrases struct {
	alertHead       string // account
	moreInDigest    string // count
	authFailed      string
	noSubject       string
	gone            string
	changeNew       string
	changeMoved     string
	changeCancelled string
	gap             string // resource, account, since
	failing         string // resource, account, since
	digestTitle     string
	digestNow       string
	unsorted        string
	digestRest      string // count
	discardedLine   string // count
	unreadable      string // accounts
	droppedLines    string // accounts
	resourceMail    string
	resourceCal     string
	timeLayout      string
	dayLayout       string
	watchTask       string // account
	digestTask      string
}

var texts = map[Lang]phrases{
	LangIT: {
		alertHead:       "Da vedere su %s:",
		moreInDigest:    "Altre %d sono nel riepilogo.",
		authFailed:      "mittente non autenticato",
		noSubject:       "(senza oggetto)",
		gone:            "(non più disponibile)",
		changeNew:       "Nuovo",
		changeMoved:     "Spostato",
		changeCancelled: "Annullato",
		gap:             "Non riesco più a seguire %[1]s su %[2]s: quello che è arrivato dal %[3]s a ora non è stato letto.",
		failing:         "Non riesco a leggere %[1]s su %[2]s dal %[3]s. Riproverò a ogni giro.",
		digestTitle:     "Riepilogo di posta e calendario",
		digestNow:       "Urgenti",
		unsorted:        "Da smistare",
		digestRest:      "E altre %d voci.",
		discardedLine:   "Mail scartate: %d.",
		unreadable:      "Non ho potuto leggere: %s. Le sue voci tornano nel prossimo riepilogo.",
		droppedLines:    "Non ho potuto leggere: %s. Le sue voci restano nella giornata dell'account, nel cockpit.",
		resourceMail:    "la posta",
		resourceCal:     "il calendario",
		timeLayout:      "02/01 15:04",
		dayLayout:       "02/01",
		watchTask:       "Controllo di posta e calendario: %s",
		digestTask:      "Riepilogo di posta e calendario",
	},
	LangEN: {
		alertHead:       "Worth a look on %s:",
		moreInDigest:    "%d more are in the digest.",
		authFailed:      "sender not verified",
		noSubject:       "(no subject)",
		gone:            "(no longer there)",
		changeNew:       "New",
		changeMoved:     "Moved",
		changeCancelled: "Cancelled",
		gap:             "I lost track of the changes to %[1]s on %[2]s: what arrived from %[3]s until now was not read.",
		failing:         "I have not been able to read %[1]s on %[2]s since %[3]s. I will try again on every tick.",
		digestTitle:     "Mail and calendar digest",
		digestNow:       "Urgent",
		unsorted:        "To sort",
		digestRest:      "And %d more.",
		discardedLine:   "Discarded mail: %d.",
		unreadable:      "I could not read: %s. Its items come back in the next digest.",
		droppedLines:    "I could not read: %s. Its items stay in the account's day in the cockpit.",
		resourceMail:    "the mail",
		resourceCal:     "the calendar",
		timeLayout:      "Jan 2 15:04",
		dayLayout:       "Jan 2",
		watchTask:       "Mail and calendar watch: %s",
		digestTask:      "Mail and calendar digest",
	},
}

func phrasesFor(lang Lang) phrases {
	if p, ok := texts[lang]; ok {
		return p
	}
	return texts[LangEN]
}

func (p phrases) resource(resource string) string {
	if resource == ResourceCalendar {
		return p.resourceCal
	}
	return p.resourceMail
}

func (p phrases) word(change ChangeWord) string {
	switch change {
	case WordNew:
		return p.changeNew
	case WordMoved:
		return p.changeMoved
	case WordCancelled:
		return p.changeCancelled
	default:
		return ""
	}
}

func (p phrases) watchTitle(accountID string) string {
	return fmt.Sprintf(p.watchTask, accountName(accountID))
}

const (
	// lineRunes bounds one address or subject in a notification.
	lineRunes = 200
	// nameRunes bounds a sender's display name, so that a long one cannot push the address, which
	// is what the rules read, out of the line.
	nameRunes = 60
)

// plainLine makes attacker text safe to put on one line of a plain-text message: no control
// character, so a subject cannot forge a line, and no invisible format character, so it cannot
// reorder what the person reads.
func plainLine(s string) string { return boundedLine(s, lineRunes) }

// accountName is the name the person gave an account. The PIM's id is the tenant's hex, two
// underscores and that name, and the hex means nothing on a phone. A record's refs keep the whole
// id, because a tool needs it.
func accountName(accountID string) string {
	if _, name, ok := strings.Cut(accountID, "__"); ok && name != "" {
		return plainLine(name)
	}
	return plainLine(accountID)
}

func boundedLine(s string, limit int) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	clean = strings.Join(strings.Fields(clean), " ")
	runes := []rune(clean)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return clean
}

// Line is one item told to the person. From, FromName and Subject are attacker text. An all-day
// Start is its date at midnight UTC.
type Line struct {
	AccountID  string
	Resource   string
	ItemID     string
	CalendarID string
	From       string
	FromName   string
	Subject    string
	Start      time.Time
	AllDay     bool
	AuthFailed bool
	Gone       bool
	Told       bool
	Tier       Tier
	LabelID    string
	Change     ChangeWord
	invitation bool
}

// sender is who a line comes from: the name beside the address, because the name is whatever the
// sender typed. The PIM gives a name alone when a header holds no address.
func (l Line) sender() string {
	name, address := boundedLine(l.FromName, nameRunes), plainLine(l.From)
	switch {
	case name == "" || strings.EqualFold(name, address):
		return address
	case address == "":
		return name
	default:
		return name + " <" + address + ">"
	}
}

// when is a line's date and time. An all-day date is the same wherever the person is.
func (l Line) when(p phrases, loc *time.Location) string {
	if l.AllDay {
		return l.Start.Format(p.dayLayout)
	}
	return l.Start.In(loc).Format(p.timeLayout)
}

// what is a line's change word and subject. A word needs no subject: "(no subject)" would make a
// cancelled meeting read as a mail.
func (l Line) what(p phrases) string {
	word, subject := p.word(l.Change), plainLine(l.Subject)
	switch {
	case word != "" && subject != "":
		return word + ": " + subject
	case word != "":
		return word
	case subject != "":
		return subject
	default:
		return p.noSubject
	}
}

// describe is the sender and subject of a line, what a notification must always carry.
func (l Line) describe(p phrases, loc *time.Location) string {
	if l.Gone {
		return p.gone
	}
	text := l.what(p)
	if from := l.sender(); from != "" {
		text = from + ": " + text
	}
	if !l.Start.IsZero() {
		text += " (" + l.when(p, loc) + ")"
	}
	if l.AuthFailed {
		text += " [" + p.authFailed + "]"
	}
	return text
}

// safeRef is what an id may look like to stand outside the frame. A provider id is usually
// opaque, but a calendar event's can come from an invitation, so an id that looks like anything
// else is quoted with the attacker text.
var safeRef = regexp.MustCompile(`^[A-Za-z0-9._:@=+/-]{1,512}$`)

// ref is a line's address, what "aprila" needs. ok is false when it cannot stand outside the frame.
func (l Line) ref() (text string, ok bool) {
	parts := []string{"account=" + l.AccountID, "resource=" + l.Resource, "id=" + l.ItemID}
	ok = safeRef.MatchString(l.AccountID) && safeRef.MatchString(l.ItemID)
	if l.CalendarID != "" {
		parts = append(parts, "calendar="+l.CalendarID)
		ok = ok && safeRef.MatchString(l.CalendarID)
	}
	return strings.Join(parts, " "), ok
}

// Outcome is what a run has to say. Summary goes to the ledger and holds counts only. Notice is
// the plain text the person gets. Head, Refs and Quoted make the conversation record: Quoted is
// the attacker text, which the handler frames as untrusted.
type Outcome struct {
	Summary string
	Notice  string
	Head    string
	Refs    string
	Quoted  string
}

// oneEach gives every line its own number.
func oneEach(lines []Line) [][]Line {
	groups := make([][]Line, 0, len(lines))
	for _, line := range lines {
		groups = append(groups, []Line{line})
	}
	return groups
}

// recordParts renders numbered text and the items each number stands for into what stands outside
// the frame and what stands inside.
func recordParts(groups [][]Line, numbered []string) (refs, quoted string) {
	outside := make([]string, 0, len(groups))
	inside := make([]string, 0, len(groups))
	for i, group := range groups {
		for _, line := range group {
			ref, safe := line.ref()
			if safe {
				outside = append(outside, fmt.Sprintf("%d. %s", i+1, ref))
				continue
			}
			inside = append(inside, fmt.Sprintf("ref %d: %s", i+1, ref))
		}
	}
	return strings.Join(outside, "\n"), strings.Join(append(numbered, inside...), "\n")
}

// renderAlert writes one notification for a tick: every alarm, then each alerted item with its
// sender and subject, then how many the cap held back. A tick that only held something back says
// nothing: the cap exists to stop the phone, and a count every five minutes would not.
func renderAlert(lang Lang, loc *time.Location, accountID string, lines []Line, capped int, alarms []string) Outcome {
	if len(lines) == 0 && len(alarms) == 0 {
		return Outcome{}
	}
	p := phrasesFor(lang)
	head := make([]string, 0, len(alarms)+1)
	head = append(head, alarms...)
	body := make([]string, 0, len(lines))
	numbered := make([]string, 0, len(lines))
	for i, line := range lines {
		text := line.describe(p, loc)
		body = append(body, "- "+text)
		numbered = append(numbered, fmt.Sprintf("%d. %s", i+1, text))
	}
	var tail []string
	if len(lines) > 0 {
		head = append(head, fmt.Sprintf(p.alertHead, accountName(accountID)))
		if capped > 0 {
			tail = append(tail, fmt.Sprintf(p.moreInDigest, capped))
		}
	}
	refs, quoted := recordParts(oneEach(lines), numbered)
	notice := append(append(append([]string{}, head...), body...), tail...)
	return Outcome{
		Notice: strings.Join(notice, "\n"),
		Head:   strings.Join(append(head, tail...), "\n"),
		Refs:   refs,
		Quoted: quoted,
	}
}

// accountNames joins the names a person gave these accounts.
func accountNames(accounts []string) string {
	names := make([]string, 0, len(accounts))
	for _, account := range accounts {
		names = append(names, accountName(account))
	}
	return strings.Join(names, ", ")
}

// digestTail says what a digest does not show: how many it left out, which accounts it could not
// read, and how much mail was discarded.
func digestTail(p phrases, rest int, missing unread, discarded int) []string {
	var tail []string
	if rest > 0 {
		tail = append(tail, fmt.Sprintf(p.digestRest, rest))
	}
	if len(missing.held) > 0 {
		tail = append(tail, fmt.Sprintf(p.unreadable, accountNames(missing.held)))
	}
	if len(missing.dropped) > 0 {
		tail = append(tail, fmt.Sprintf(p.droppedLines, accountNames(missing.dropped)))
	}
	if discarded > 0 {
		tail = append(tail, fmt.Sprintf(p.discardedLine, discarded))
	}
	return tail
}

// renderDigest writes the day's digest from lines already in digest order. Each change of heading
// starts a group, and rest counts what the cap left out. Discarded mail is a line of its own and
// never makes a digest on its own.
func renderDigest(lang Lang, loc *time.Location, book Book, lines []Line, rest int, missing unread, discarded int) Outcome {
	if len(lines) == 0 && rest == 0 && missing.none() {
		return Outcome{}
	}
	p := phrasesFor(lang)
	body := make([]string, 0, 2*len(lines))
	heading := ""
	for i, line := range lines {
		urgent := line.Tier == TierNow
		label := plainLine(book.LabelName(line.LabelID))
		title := label
		switch {
		case urgent:
			title = p.digestNow
		case label == "":
			title = p.unsorted
		}
		if title != heading {
			heading = title
			body = append(body, title)
		}
		text := fmt.Sprintf("%d. [%s] %s", i+1, accountName(line.AccountID), line.describe(p, loc))
		if urgent && label != "" {
			text += " (" + label + ")"
		}
		body = append(body, text)
	}
	tail := digestTail(p, rest, missing, discarded)
	refs, quoted := recordParts(oneEach(lines), body)
	notice := append(append([]string{p.digestTitle}, body...), tail...)
	return Outcome{
		Notice: strings.Join(notice, "\n"),
		Head:   strings.Join(append([]string{p.digestTitle}, tail...), "\n"),
		Refs:   refs,
		Quoted: quoted,
	}
}
