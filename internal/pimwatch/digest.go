package pimwatch

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// maxDigestLines caps a digest (spec §5.2).
const maxDigestLines = 20

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

// Run writes one digest. The window moves only when every line could be read, so a digest that
// names an unreadable account brings back the account's lines tomorrow, and repeats what it did
// read: nothing is lost, and a repeated line costs less than a missing one. An empty window sends
// nothing and still moves.
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
	shown := verdicts[:min(len(verdicts), maxDigestLines)]
	lines, unreadable := d.read(ctx, shown)
	out := renderDigest(
		d.Locale.Language(ctx, identityID), d.Locale.Location(ctx, identityID), book,
		lines, len(verdicts)-len(shown), unreadable,
	)
	out.Summary = fmt.Sprintf("%d in the window, %d shown", len(verdicts), len(lines))
	if len(unreadable) > 0 {
		out.Summary += ", unreadable: " + strings.Join(unreadable, ", ")
		return out, nil
	}
	return out, d.Store.SetLastDigest(ctx, identityID, until)
}

// sortDigest puts a window in the digest's order: `now` first, then each label in the person's
// order, then what has none. The order among equals stays the store's, oldest first.
func sortDigest(verdicts []Verdict, labels []Label) {
	position := make(map[string]int, len(labels))
	for i, label := range labels {
		position[label.ID] = i
	}
	rank := func(v Verdict) int {
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

// read reads the shown verdicts live, keeping their order. An account that cannot be read loses
// only its own lines, and the digest says which.
func (d *Digester) read(ctx context.Context, verdicts []Verdict) ([]Line, []string) {
	byAccount := map[string][]Verdict{}
	var accounts []string
	for _, verdict := range verdicts {
		if _, ok := byAccount[verdict.AccountID]; !ok {
			accounts = append(accounts, verdict.AccountID)
		}
		byAccount[verdict.AccountID] = append(byAccount[verdict.AccountID], verdict)
	}
	read := make(map[string]Line, len(verdicts))
	var unreadable []string
	for _, account := range accounts {
		lines, err := readLines(ctx, d.PIM, account, byAccount[account])
		if err != nil {
			unreadable = append(unreadable, account)
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
	return out, unreadable
}

func verdictKey(accountID, resource, itemID string) string {
	return accountID + "\x00" + resource + "\x00" + itemID
}
