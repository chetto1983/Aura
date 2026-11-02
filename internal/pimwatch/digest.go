package pimwatch

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	// maxDigestLines caps what a digest shows (spec §5.2).
	maxDigestLines = 20
	// maxDigestRead caps what a digest reads, so a day's cost has a ceiling whatever the day was.
	maxDigestRead = 100
)

// DigestStore is what a digest reads and writes. *Store satisfies it.
type DigestStore interface {
	Settings(ctx context.Context, identityID string) (Settings, error)
	Book(ctx context.Context, identityID string) (Book, error)
	Labels(ctx context.Context, identityID string) ([]Label, error)
	DigestVerdicts(ctx context.Context, identityID string, since, until time.Time) ([]Verdict, error)
	SetLastDigest(ctx context.Context, identityID string, at time.Time) error
}

var _ DigestStore = (*Store)(nil)

// Digester writes an identity's daily digest, read live: no sender or subject was kept.
type Digester struct {
	Store  DigestStore
	PIM    PIMReader
	Locale Locale
	// Now overrides the clock in tests; nil is time.Now.
	Now func() time.Time
}

func (d *Digester) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

// unread names the accounts a digest could not read, by what that means for its window.
type unread struct {
	// held failed for now, so the window stays and their lines come back tomorrow.
	held []string
	// dropped need the person, and tomorrow would fail the same way.
	dropped []string
}

func (u *unread) add(account string, err error) {
	if class, _ := failureClass(err); class == FailureTransient {
		u.held = append(u.held, account)
		return
	}
	u.dropped = append(u.dropped, account)
}

func (u unread) none() bool { return len(u.held) == 0 && len(u.dropped) == 0 }

// Run writes one digest. A transient failure holds the window, so the account's lines come back
// tomorrow beside a repeat of what was read: nothing is lost, and a repeated line costs less than
// a missing one. A hard failure names the account and does not hold it, because it would fail the
// same way tomorrow and its old rows would fill the first lines for good, so nobody's new mail
// would be read again. Its lines leave the digest and stay in the cockpit's day. If any account
// failed transiently the window holds for all of them. An empty window sends nothing and still
// moves.
func (d *Digester) Run(ctx context.Context, identityID string) (Outcome, error) {
	settings, err := d.Store.Settings(ctx, identityID)
	if err != nil {
		return Outcome{}, err
	}
	book, err := d.Store.Book(ctx, identityID)
	if err != nil {
		return Outcome{}, err
	}
	labels, err := d.Store.Labels(ctx, identityID)
	if err != nil {
		return Outcome{}, err
	}
	since := settings.LastDigestAt
	if since.IsZero() {
		since = settings.CreatedAt
	}
	until := d.now()
	verdicts, err := d.Store.DigestVerdicts(ctx, identityID, since, until)
	if err != nil {
		return Outcome{}, err
	}
	sortDigest(verdicts, labels)
	read := verdicts[:min(len(verdicts), maxDigestRead)]
	lines, missing := d.read(ctx, read)
	news := newsIn(lines)
	shown := news[:min(len(news), maxDigestLines)]
	out := renderDigest(
		d.Locale.Language(ctx, identityID), d.Locale.Location(ctx, identityID), book,
		shown, len(news)-len(shown)+untold(verdicts[len(read):]), missing,
	)
	out.Summary = fmt.Sprintf("%d in the window, %d shown", len(verdicts), len(shown))
	if !missing.none() {
		out.Summary += ", unreadable: " + strings.Join(slices.Concat(missing.held, missing.dropped), ", ")
	}
	if len(missing.held) > 0 {
		return out, nil
	}
	return out, d.Store.SetLastDigest(ctx, identityID, until)
}

// sortDigest puts a window in the digest's order: `now` first, then each label in the person's
// order, then what has none, and last what the person was already told, so it never takes the
// place of something new. The order among equals stays the store's, oldest first.
func sortDigest(verdicts []Verdict, labels []Label) {
	position := make(map[string]int, len(labels))
	for i, label := range labels {
		position[label.ID] = i
	}
	rank := func(v Verdict) int {
		if v.Notified {
			return len(labels) + 1
		}
		if v.Tier == TierNow {
			return -1
		}
		if at, ok := position[v.LabelID]; ok {
			return at
		}
		return len(labels)
	}
	slices.SortStableFunc(verdicts, func(a, b Verdict) int { return rank(a) - rank(b) })
}

// newsIn keeps the lines worth a person's time: the phone already said a told one, and a gone one
// has nothing to say.
func newsIn(lines []Line) []Line {
	return slices.DeleteFunc(slices.Clone(lines), func(line Line) bool { return line.Told || line.Gone })
}

// untold counts the verdicts the person has not heard about.
func untold(verdicts []Verdict) int {
	n := 0
	for _, verdict := range verdicts {
		if !verdict.Notified {
			n++
		}
	}
	return n
}

// read reads the shown verdicts live, keeping their order. An account that cannot be read loses
// only its own lines, and the digest says which.
func (d *Digester) read(ctx context.Context, verdicts []Verdict) ([]Line, unread) {
	byAccount := map[string][]Verdict{}
	var accounts []string
	for _, verdict := range verdicts {
		if _, ok := byAccount[verdict.AccountID]; !ok {
			accounts = append(accounts, verdict.AccountID)
		}
		byAccount[verdict.AccountID] = append(byAccount[verdict.AccountID], verdict)
	}
	read := make(map[string]Line, len(verdicts))
	var missing unread
	for _, account := range accounts {
		lines, err := readLines(ctx, d.PIM, account, byAccount[account])
		if err != nil {
			missing.add(account, err)
			continue
		}
		for _, line := range lines {
			read[verdictKey(line.AccountID, line.Resource, line.ItemID)] = line
		}
	}
	out := make([]Line, 0, len(verdicts))
	for _, verdict := range verdicts {
		if line, ok := read[verdictKey(verdict.AccountID, verdict.Resource, verdict.ItemID)]; ok {
			out = append(out, line)
		}
	}
	return out, missing
}

func verdictKey(accountID, resource, itemID string) string {
	return accountID + "\x00" + resource + "\x00" + itemID
}
