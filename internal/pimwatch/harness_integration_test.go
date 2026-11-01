//go:build db_integration

// The live-Postgres tier of the watch: what only the engine can prove. Replays, the cap and the
// owed alerts are all SQL, so the scenarios run on the real store over a fake PIM.
//
//	go test -tags db_integration -p 1 ./internal/pimwatch/
package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/dbtest"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("pimwatch integration requires %s under CI — a skipped tier must not pass as green", key)
		}
		t.Skipf("pimwatch integration requires %s", key)
	}
	return v
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	if err := db.EnsureRoles(ctx, fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, "pimwatch-test-"+id[:8]); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id) })
	return id
}

// fakePIM answers from scripted pages. An unscripted cursor is an empty page that keeps the cursor.
type fakePIM struct {
	pages     map[string]ChangesPage
	changeErr map[string]error
	items     map[string]Item
	itemsErr  map[string]error
	onChanges func(resource, cursor string)
	reads     [][]ItemRef
	calls     map[string]int
}

func newFakePIM() *fakePIM {
	return &fakePIM{
		pages: map[string]ChangesPage{}, changeErr: map[string]error{}, items: map[string]Item{},
		itemsErr: map[string]error{}, calls: map[string]int{},
	}
}

func (f *fakePIM) page(resource, cursor string, page ChangesPage) {
	f.pages[resource+"|"+cursor] = page
}

func (f *fakePIM) Changes(_ context.Context, _, resource, cursor string, _ int) (ChangesPage, error) {
	f.calls[resource]++
	if f.onChanges != nil {
		f.onChanges(resource, cursor)
	}
	if err := f.changeErr[resource]; err != nil {
		return ChangesPage{}, err
	}
	if cursor == "" {
		return ChangesPage{NextCursor: "start-" + resource}, nil
	}
	if page, ok := f.pages[resource+"|"+cursor]; ok {
		return page, nil
	}
	return ChangesPage{NextCursor: cursor}, nil
}

func (f *fakePIM) Items(_ context.Context, accountID, _ string, refs []ItemRef) (ItemsResult, error) {
	if err := f.itemsErr[accountID]; err != nil {
		return ItemsResult{}, err
	}
	f.reads = append(f.reads, refs)
	var out ItemsResult
	for _, ref := range refs {
		if item, ok := f.items[ref.ID]; ok {
			out.Items = append(out.Items, item)
		} else {
			out.Gone = append(out.Gone, ref)
		}
	}
	return out, nil
}

// fakeLedger says which runs completed.
type fakeLedger struct {
	completed map[string]bool
	err       error
}

func (f *fakeLedger) RunCompleted(_ context.Context, runID string) (bool, error) {
	return f.completed[runID], f.err
}

type fakeLocale struct {
	lang Lang
	loc  *time.Location
}

func (f fakeLocale) Language(context.Context, string) Lang { return f.lang }

func (f fakeLocale) Location(context.Context, string) *time.Location { return f.loc }

// clock is a settable clock.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

// watchRig is one identity's watch on the real store.
type watchRig struct {
	t        *testing.T
	store    *Store
	pim      *fakePIM
	ledger   *fakeLedger
	clock    *clock
	watcher  *Watcher
	identity string
}

const rigAccount = "work"

func newWatchRig(t *testing.T) *watchRig {
	t.Helper()
	pool := migratedPool(t)
	store := NewStore(pool)
	pim := newFakePIM()
	ledger := &fakeLedger{completed: map[string]bool{}}
	// The future, so the rows the database stamps with now() are always in the past.
	c := &clock{at: time.Now().UTC().Add(24 * time.Hour)}
	r := &watchRig{
		t: t, store: store, pim: pim, ledger: ledger, clock: c, identity: seedIdentity(t, pool),
		watcher: &Watcher{Store: store, PIM: pim, Ledger: ledger, Locale: fakeLocale{LangEN, time.UTC}, Now: c.now},
	}
	if _, err := store.Settings(context.Background(), r.identity); err != nil {
		t.Fatalf("settings: %v", err)
	}
	for _, resource := range Resources {
		if err := StartResource(context.Background(), pim, store, r.identity, rigAccount, resource); err != nil {
			t.Fatalf("start %s: %v", resource, err)
		}
	}
	return r
}

// run ticks once under a fresh run id, which it returns with the outcome.
func (r *watchRig) run() (string, Outcome, error) {
	r.t.Helper()
	run := uuid.NewString()
	out, err := r.watcher.Tick(context.Background(), TickInput{IdentityID: r.identity, AccountID: rigAccount, RunID: run})
	return run, out, err
}

// tick is run for a tick that must not fail, and the dispatcher completes it.
func (r *watchRig) tick() Outcome {
	r.t.Helper()
	run, out, err := r.run()
	if err != nil {
		r.t.Fatalf("tick: %v", err)
	}
	r.ledger.completed[run] = true
	return out
}

func (r *watchRig) cursor(resource string) Cursor {
	r.t.Helper()
	cursor, err := r.store.Cursor(context.Background(), r.identity, rigAccount, resource)
	if err != nil {
		r.t.Fatalf("cursor: %v", err)
	}
	return cursor
}

// urgentMail is a mail the ladder sends to `now`, readable live by id.
func (r *watchRig) urgentMail(id string) Change {
	written := true
	from, subject := id+"@acme.it", "Subject of "+id
	r.pim.items[id] = Item{ID: id, From: from, Subject: subject}
	return Change{
		Type: ChangeAdded, ID: id, From: from, Subject: subject,
		Signals: Signals{ProviderImportant: true, WrittenTo: &written},
	}
}

// todayMail is a mail the ladder sends to `today`.
func (r *watchRig) todayMail(id string) Change {
	written := true
	r.pim.items[id] = Item{ID: id, From: id + "@acme.it", Subject: "Subject of " + id}
	return Change{Type: ChangeAdded, ID: id, From: id + "@acme.it", Subject: "Subject of " + id, Signals: Signals{WrittenTo: &written}}
}

func changesOf(mail func(string) Change, prefix string, n int) []Change {
	out := make([]Change, 0, n)
	for i := range n {
		out = append(out, mail(fmt.Sprintf("%s%03d", prefix, i)))
	}
	return out
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

// Verdict reads one verdict as the tests check it. A missing row is ErrNotFound.
func (s *Store) Verdict(ctx context.Context, identityID, accountID, resource, itemID string) (Verdict, error) {
	var out Verdict
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.GetPIMWatchVerdict(ctx, sqlc.GetPIMWatchVerdictParams{
			IdentityID: id, AccountID: accountID, Resource: resource, ItemID: itemID,
		})
		if err != nil {
			return err
		}
		out = verdictFromRow(row)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Verdict{}, ErrNotFound
	}
	return out, err
}

func transient() error { return &PIMError{Class: FailureTransient, Code: "provider_transient"} }

func hard() error { return &PIMError{Class: FailureHard, Code: "provider_auth"} }

func unsupported() error { return &PIMError{Class: FailureUnsupported, Code: "unsupported_resource"} }

func denied() error { return &PIMError{Class: FailureHard, Code: "permission_denied"} }

var errBroken = errors.New("broken")

// commit writes verdicts directly, with the labels a ladder would give them.
func (r *watchRig) commit(accountID string, drafts ...VerdictDraft) {
	r.t.Helper()
	_, err := r.store.CommitPage(context.Background(), r.identity, PageCommit{
		AccountID: accountID, Resource: ResourceMail, Cursor: "c-" + uuid.NewString(), RunID: uuid.NewString(),
		Drafts: drafts, DayStart: r.clock.at, Now: r.clock.at, Cap: 0,
	})
	if err != nil {
		r.t.Fatalf("commit: %v", err)
	}
}

func (r *watchRig) draft(id string, tier Tier, labelID string) VerdictDraft {
	r.pim.items[id] = Item{ID: id, From: id + "@acme.it", Subject: "Subject of " + id}
	return VerdictDraft{ItemID: id, Decision: Decision{Tier: tier, LabelID: labelID, By: ByDefault}}
}
