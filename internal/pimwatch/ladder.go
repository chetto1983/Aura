// Package pimwatch tells a person when something in their mail or calendar needs them, and calls
// no model to do it (docs/superpowers/specs/2026-10-31-mail-calendar-watch-design.md; prd.md §11
// and §15, 2026-11-01). The scheduler's pim_watch and pim_digest handlers drive it through ports.
//
// The first release decides with rules and provider facts alone. Rung 5 of the spec's ladder,
// corrections by similarity and a classifier, does not exist here yet: mail nothing decided is
// `today`, which is the harmless outcome.
//
// Aura keeps ids and verdicts, never a mail index. Sender and subject are read live from the PIM
// when a digest or the decisions view needs them, and the only sender text stored is the key of a
// rule or a correction.
package pimwatch

import (
	"net/mail"
	"strings"
	"time"
)

// Tier says how soon a mail matters.
type Tier string

// The three tiers (spec §5.3).
const (
	TierNow   Tier = "now"
	TierToday Tier = "today"
	TierLater Tier = "later"
)

// Valid reports whether t is one of the three tiers.
func (t Tier) Valid() bool {
	return t == TierNow || t == TierToday || t == TierLater
}

// DecidedBy names the rung that gave a verdict its tier.
type DecidedBy string

// The rungs, plus a calendar change, the decision model's label and the person's own correction.
const (
	ByModel      DecidedBy = "model"
	ByRule       DecidedBy = "rule"
	ByAuthFailed DecidedBy = "auth_failed"
	ByPeople     DecidedBy = "people"
	ByCategory   DecidedBy = "category"
	ByDefault    DecidedBy = "default"
	ByCalendar   DecidedBy = "calendar"
	ByPerson     DecidedBy = "person"
)

// KeyKind says whether a rule matches a whole address or a domain.
type KeyKind string

// The two kinds of rule key.
const (
	KeySender KeyKind = "sender"
	KeyDomain KeyKind = "domain"
)

// Rule maps a sender or a domain to a tier, a label or both. An empty Tier or LabelID says
// nothing about that field.
type Rule struct {
	ID        string
	Key       string
	Kind      KeyKind
	Tier      Tier
	LabelID   string
	Source    string
	CreatedAt time.Time
}

// Label is the person's name for what a mail is about. A Tier, when set, acts as a rule. The
// Description is the only text of it the decision model reads, and Discard sends the mails the
// label is given to out of the digest.
type Label struct {
	ID          string
	Name        string
	Tier        Tier
	Position    int
	Description string
	Discard     bool
}

// LabelInput is what a person writes about a label.
type LabelInput struct {
	Name        string
	Tier        Tier
	Description string
	Discard     bool
}

// Book is an identity's rules and labels, read once per run.
type Book struct {
	senders map[string]Rule
	domains map[string]Rule
	labels  map[string]Label
}

// NewBook indexes rules and labels for the ladder.
func NewBook(rules []Rule, labels []Label) Book {
	book := Book{senders: map[string]Rule{}, domains: map[string]Rule{}, labels: map[string]Label{}}
	for _, rule := range rules {
		if rule.Kind == KeyDomain {
			book.domains[rule.Key] = rule
		} else {
			book.senders[rule.Key] = rule
		}
	}
	for _, label := range labels {
		book.labels[label.ID] = label
	}
	return book
}

// match finds the rule for a sender. A sender rule beats a domain rule outright, even when it
// says less: the rule the person wrote closest to this mail is the one that applies.
func (b Book) match(from string) (Rule, bool) {
	address, domain := SenderKey(from)
	if rule, ok := b.senders[address]; ok && address != "" {
		return rule, true
	}
	rule, ok := b.domains[domain]
	return rule, ok && domain != ""
}

// tierOf is the tier a rule gives: its own, else that of the label it gives.
func (b Book) tierOf(rule Rule) Tier {
	if rule.Tier != "" {
		return rule.Tier
	}
	return b.labels[rule.LabelID].Tier
}

// LabelName is a label's name, "" when the book has no such label.
func (b Book) LabelName(id string) string { return b.labels[id].Name }

// SenderKey derives the lower-case address and domain of a From value. An unparsable value gives
// "", "": a key nothing can match.
func SenderKey(from string) (address, domain string) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(from))
	if err != nil {
		return "", ""
	}
	address = strings.ToLower(parsed.Address)
	_, domain, _ = strings.Cut(address, "@")
	return address, domain
}

// Signals are the facts the PIM states about a mail (spec §5.1). WrittenTo is nil when the
// provider cannot tell, which is not the same as false.
type Signals struct {
	ProviderImportant bool   `json:"providerImportant"`
	ProviderCategory  string `json:"providerCategory"`
	Bulk              bool   `json:"bulk"`
	AuthFailed        bool   `json:"authFailed"`
	ThreadID          string `json:"threadId"`
	WrittenTo         *bool  `json:"writtenTo"`
	Invitation        bool   `json:"invitation"`
}

// ChangeWord is what happened to an event, the fixed word a line puts before its subject. Mail has
// none, and neither has an event nothing shows the change of.
type ChangeWord string

// The three change words, stored on the verdict and sent to the cockpit as these codes.
const (
	WordNew       ChangeWord = "new"
	WordMoved     ChangeWord = "moved"
	WordCancelled ChangeWord = "cancelled"
)

// Decision is what the ladder says about one item.
type Decision struct {
	Tier       Tier
	LabelID    string
	By         DecidedBy
	AuthFailed bool
	Change     ChangeWord
	Discarded  bool
}

// DecideMail walks rungs 1 to 4 and 6 of the ladder (spec §5.3). A rule wins over everything,
// failed authentication included, and the mark stays on the verdict so the person sees it. A rule
// that only gives a label lets the ladder go on, and the mail keeps the label.
//
// `providerImportant` counts only for someone the account has written to: on Graph and IMAP the
// sender sets it, and in the pilot 23 of the 25 mails it marked were `later`. Bulk mail is not
// people, because a list you once replied to must not reach `now`. An unknown writtenTo cannot
// raise a mail, and cannot file it either: doubt goes to the digest.
func DecideMail(book Book, from string, s Signals) Decision {
	decision := Decision{AuthFailed: s.AuthFailed}
	if rule, ok := book.match(from); ok {
		decision.LabelID = rule.LabelID
		if tier := book.tierOf(rule); tier != "" {
			decision.Tier, decision.By = tier, ByRule
			return decision
		}
	}
	written := s.WrittenTo != nil && *s.WrittenTo
	switch {
	case s.AuthFailed:
		decision.Tier, decision.By = TierToday, ByAuthFailed
	case written && !s.Bulk && s.ProviderImportant:
		decision.Tier, decision.By = TierNow, ByPeople
	case written && !s.Bulk:
		decision.Tier, decision.By = TierToday, ByPeople
	case s.WrittenTo == nil && !s.Bulk:
		decision.Tier, decision.By = TierToday, ByDefault
	case fileableCategory(s.ProviderCategory):
		decision.Tier, decision.By = TierLater, ByCategory
	default:
		decision.Tier, decision.By = TierToday, ByDefault
	}
	return decision
}

// fileableCategory is rung 4: Gmail's promotional and social categories. Graph's `other` says
// nothing about content, so it stays undecided, and so does a category nobody named here.
func fileableCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "promotions", "social":
		return true
	default:
		return false
	}
}

// ChangeType is what the PIM says happened to an item.
type ChangeType string

// The three change types (spec §5.1).
const (
	ChangeAdded   ChangeType = "added"
	ChangeUpdated ChangeType = "updated"
	ChangeDeleted ChangeType = "deleted"
)

// eventNowWindow is how close an event must start for a change to it to be `now`.
const eventNowWindow = 24 * time.Hour

// EventFacts are what the calendar's half of the ladder reads. Prior is the fingerprint on the
// verdict row, and Seen says there is such a row. KeptStart is the start that row kept, the time
// the person last knew; it is zero when there is no row.
type EventFacts struct {
	Type            ChangeType
	Start           time.Time
	KeptStart       time.Time
	Cancelled       bool
	OrganizerIsSelf bool
	Fingerprint     string
	Prior           string
	// PriorWord is the word the row kept: a delete of a cancellation already told says nothing, the
	// organizer having cancelled and the person then removing it from the calendar.
	PriorWord ChangeWord
	Seen      bool
}

// deletedFingerprint marks an event the PIM reported deleted, which carries no fingerprint of its
// own, so that a repeated delete says nothing.
const deletedFingerprint = "deleted"

// fingerprintOf is the fingerprint a change leaves on the verdict row.
func (e EventFacts) fingerprintOf() string {
	if e.Type == ChangeDeleted {
		return deletedFingerprint
	}
	return e.Fingerprint
}

// word says what happened. An `updated` event with no row was there before the watch began, so
// nothing shows what changed and the line says nothing.
func (e EventFacts) word() ChangeWord {
	switch {
	case e.Cancelled || e.Type == ChangeDeleted:
		return WordCancelled
	case e.Seen:
		return WordMoved
	case e.Type == ChangeAdded:
		return WordNew
	default:
		return ""
	}
}

// DecideEvent reads a calendar change as a structural fact (spec §5.3): a new invitation, a moved
// event and a cancelled one are `now` when the event starts within 24 hours, otherwise `today`.
// told is false when the fingerprint did not move, which is a new description or someone else's
// reply, and the change says nothing.
//
// A `deleted` change with no row and no start says nothing either: it names only an id, and the
// person may well have made it. Google writes every cancellation as `deleted`, and documents only
// the id as guaranteed, so Start is what the change says or else what the row kept, and a delete
// is read as the cancellation it is: a missed meeting costs more than one alert for a delete the
// person made of a meeting they did not organize. The person's own events are `later`: they made
// the change, and the row remembers who organized the event, so a bare delete of one stays `later`
// too. A delete of a row already told as cancelled says nothing and leaves the row as it is. An
// `updated` event with no row was there before the watch began, so nothing shows that it moved,
// and doubt goes to the digest. A cancellation is no such doubt: the status is the fact.
//
// An event with a row is `now` when its new start or its kept start is within 24 hours: a
// meeting moved away from tomorrow is one the person may still turn up to. With no row no old time
// is known, so only the new start counts. The person's own events stay `later` either way.
func DecideEvent(now time.Time, e EventFacts) (decision Decision, told bool) {
	if e.Seen && e.Prior == e.fingerprintOf() {
		return Decision{}, false
	}
	if e.Type == ChangeDeleted && e.PriorWord == WordCancelled {
		return Decision{}, false
	}
	if e.Type == ChangeDeleted && !e.Seen && e.Start.IsZero() {
		return Decision{}, false
	}
	decision = Decision{By: ByCalendar, Change: e.word()}
	switch {
	case e.OrganizerIsSelf:
		decision.Tier = TierLater
	case e.Type == ChangeUpdated && !e.Seen && !e.Cancelled:
		decision.Tier = TierToday
	case startsWithin(e.Start, now, eventNowWindow), startsWithin(e.KeptStart, now, eventNowWindow):
		decision.Tier = TierNow
	default:
		decision.Tier = TierToday
	}
	return decision, true
}

func startsWithin(start, now time.Time, window time.Duration) bool {
	return !start.IsZero() && !start.Before(now) && start.Sub(now) <= window
}
