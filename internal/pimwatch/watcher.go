package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	// failingAlarmAfter is how long a resource may fail before one alarm says so (spec §5.6).
	failingAlarmAfter = time.Hour
	// maxPagesPerResource keeps a large backlog of one resource from starving the other.
	maxPagesPerResource = 20
	// PageReserve is the time a run must have left to start another page. A scheduler bound below
	// it would never let a page finish.
	PageReserve = pimRequestTimeout + 5*time.Second
)

// TickInput names one account's run.
type TickInput struct {
	IdentityID string
	AccountID  string
	RunID      string
}

// PIMReader is the part of Client the runs read through.
type PIMReader interface {
	Changes(ctx context.Context, accountID, resource, cursor string, limit int) (ChangesPage, error)
	Items(ctx context.Context, accountID, resource string, refs []ItemRef) (ItemsResult, error)
}

// RunLedger answers whether a scheduler run completed. The dispatcher completes a run before it
// notifies, so a run that did not complete told nobody.
type RunLedger interface {
	RunCompleted(ctx context.Context, runID string) (bool, error)
}

// Locale gives an identity's language and zone, read at use.
type Locale interface {
	Language(ctx context.Context, identityID string) Lang
	Location(ctx context.Context, identityID string) *time.Location
}

// WatchStore is what a tick reads and writes. *Store satisfies it.
type WatchStore interface {
	Settings(ctx context.Context, identityID string) (Settings, error)
	Book(ctx context.Context, identityID string) (Book, error)
	Cursor(ctx context.Context, identityID, accountID, resource string) (Cursor, error)
	StartCursor(ctx context.Context, identityID, accountID, resource, value, status string) error
	PriorEvents(ctx context.Context, identityID, accountID, resource string, ids []string) (map[string]PriorEvent, error)
	CommitPage(ctx context.Context, identityID string, p PageCommit) (PageResult, error)
	MarkFailing(ctx context.Context, identityID, accountID, resource, code string, at time.Time) (Cursor, error)
	MarkUnsupported(ctx context.Context, identityID, accountID, resource string) error
	RaiseFailingAlarm(ctx context.Context, identityID, accountID, resource, runID string) error
	RetellAlarm(ctx context.Context, identityID, accountID, resource, runID string) error
	ConfirmAlarm(ctx context.Context, identityID, accountID, resource string) error
	OwedAlerts(ctx context.Context, identityID, accountID string) ([]Verdict, error)
	ConfirmAlerts(ctx context.Context, identityID, accountID, runID string) error
	RetellAlerts(ctx context.Context, identityID, accountID, oldRun, newRun string) error
}

var _ WatchStore = (*Store)(nil)

// HardFailure is a failure that needs the person: a revoked grant, a lost permission. It pauses
// the account's task after AURA_SCHEDULER_PAUSE_AFTER_FAILURES runs. Each run before the pausing
// one stays silent, and the dispatcher reports the pausing one (spec §5.6). Overdue is a failure
// that outlived the alarm's hour without pausing, which only a disabled auto-pause allows: it
// speaks up once, so the person still finds out.
type HardFailure struct {
	AccountID string
	Resource  string
	Code      string
	Overdue   bool
}

func (e *HardFailure) Error() string {
	return fmt.Sprintf("cannot read %s of account %s: %s", e.Resource, accountName(e.AccountID), e.Code)
}

// SuppressSchedulerNotification keeps every failure but the reported ones silent.
func (e *HardFailure) SuppressSchedulerNotification() bool { return !e.Overdue }

// Watcher runs one account's tick: read what changed, decide, commit, say what needs saying.
type Watcher struct {
	Store  WatchStore
	PIM    PIMReader
	Ledger RunLedger
	Locale Locale
	// Now overrides the clock in tests; nil is time.Now.
	Now func() time.Time
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// tick is one run's accumulated state.
type tick struct {
	TickInput
	settings Settings
	book     Book
	lang     Lang
	loc      *time.Location
	decided  int
	capped   int
	lines    []Line
	alarms   []string
	failing  []string
	backlog  bool
	hard     *HardFailure
}

func (t *tick) carry(p phrases, kind, resource string, since time.Time) {
	format := p.failing
	if kind == AlarmGap {
		format = p.gap
	}
	t.alarms = append(t.alarms, fmt.Sprintf(format, p.resource(resource), accountName(t.AccountID), since.In(t.loc).Format(p.timeLayout)))
}

// Tick runs once. The notification goes out after it returns, so everything a run tells is
// committed first and stays owed until the ledger shows the run completed: the next run tells
// again what a dead run committed and never told, and never what a finished one did.
//
// Only a store failure and a hard failure with nothing to tell return an error. A transient
// failure completes the run, because the next tick is the retry and three of them would be a
// 15-minute outage.
func (w *Watcher) Tick(ctx context.Context, in TickInput) (Outcome, error) {
	t, err := w.open(ctx, in)
	if err != nil {
		return Outcome{}, err
	}
	if err := w.recoverAlerts(ctx, t); err != nil {
		return Outcome{}, err
	}
	for _, resource := range Resources {
		if err := w.watchResource(ctx, t, resource); err != nil {
			return Outcome{}, err
		}
		if t.hard != nil {
			break
		}
	}
	return t.outcome()
}

func (w *Watcher) open(ctx context.Context, in TickInput) (*tick, error) {
	settings, err := w.Store.Settings(ctx, in.IdentityID)
	if err != nil {
		return nil, err
	}
	book, err := w.Store.Book(ctx, in.IdentityID)
	if err != nil {
		return nil, err
	}
	return &tick{
		TickInput: in, settings: settings, book: book,
		lang: w.Locale.Language(ctx, in.IdentityID), loc: w.Locale.Location(ctx, in.IdentityID),
	}, nil
}

// recoverAlerts settles what earlier runs told. A completed run's alerts went out. Any other run
// committed them and died before the dispatcher could notify, so this run tells them instead,
// reading sender and subject live. A read that fails leaves them owed for the next run.
func (w *Watcher) recoverAlerts(ctx context.Context, t *tick) error {
	owed, err := w.Store.OwedAlerts(ctx, t.IdentityID, t.AccountID)
	if err != nil {
		return err
	}
	byRun := map[string][]Verdict{}
	var runs []string
	for _, verdict := range owed {
		if _, ok := byRun[verdict.AlertRunID]; !ok {
			runs = append(runs, verdict.AlertRunID)
		}
		byRun[verdict.AlertRunID] = append(byRun[verdict.AlertRunID], verdict)
	}
	for _, run := range runs {
		completed, err := w.Ledger.RunCompleted(ctx, run)
		if err != nil {
			return err
		}
		if completed {
			if err := w.Store.ConfirmAlerts(ctx, t.IdentityID, t.AccountID, run); err != nil {
				return err
			}
			continue
		}
		lines, err := readLines(ctx, w.PIM, t.AccountID, byRun[run])
		if err != nil {
			continue
		}
		if err := w.Store.RetellAlerts(ctx, t.IdentityID, t.AccountID, run, t.RunID); err != nil {
			return err
		}
		for _, line := range lines {
			if !line.Gone {
				t.lines = append(t.lines, line)
			}
		}
	}
	return nil
}

// watchResource settles the resource's alarm and walks its pages. The cursor moves only with the
// page it covers, so a run cut short leaves the rest for the next tick.
func (w *Watcher) watchResource(ctx context.Context, t *tick, resource string) error {
	cursor, err := w.Store.Cursor(ctx, t.IdentityID, t.AccountID, resource)
	if errors.Is(err, ErrNotFound) {
		return w.startResource(ctx, t, resource)
	}
	if err != nil {
		return err
	}
	if cursor.Status == CursorUnsupported {
		return nil
	}
	if err := w.settleAlarm(ctx, t, cursor); err != nil {
		return err
	}
	value := cursor.Value
	for pages := 0; ; pages++ {
		if !w.hasTime(ctx) || pages == maxPagesPerResource {
			t.backlog = true
			return nil
		}
		page, err := w.PIM.Changes(ctx, t.AccountID, resource, value, PageLimit)
		if err != nil {
			return w.fail(ctx, t, resource, err)
		}
		if err := w.commit(ctx, t, resource, cursor, page); err != nil {
			return err
		}
		if page.ResetRequired || !page.HasMore {
			return nil
		}
		value = page.NextCursor
	}
}

// startResource begins a resource with no row, which a task created before its cursor would have.
func (w *Watcher) startResource(ctx context.Context, t *tick, resource string) error {
	err := StartResource(ctx, w.PIM, w.Store, t.IdentityID, t.AccountID, resource)
	if err != nil {
		class, code := failureClass(err)
		if class == FailureHard {
			t.hard = &HardFailure{AccountID: t.AccountID, Resource: resource, Code: code}
			return nil
		}
		t.failing = append(t.failing, resource+" "+code)
	}
	return nil
}

// StartResource sets a resource's cursor from the empty-cursor call, so old mail is never read
// back. A resource the provider lacks is recorded and skipped from then on. Any other failure is
// the caller's.
func StartResource(ctx context.Context, pim PIMReader, store CursorStarter, identityID, accountID, resource string) error {
	page, err := pim.Changes(ctx, accountID, resource, "", PageLimit)
	if err == nil {
		return store.StartCursor(ctx, identityID, accountID, resource, page.NextCursor, CursorOK)
	}
	if class, _ := failureClass(err); class == FailureUnsupported {
		return store.StartCursor(ctx, identityID, accountID, resource, "", CursorUnsupported)
	}
	return err
}

// CursorStarter is the write StartResource needs.
type CursorStarter interface {
	StartCursor(ctx context.Context, identityID, accountID, resource, value, status string) error
}

// settleAlarm does for an alarm what recoverAlerts does for an alert.
func (w *Watcher) settleAlarm(ctx context.Context, t *tick, cursor Cursor) error {
	if cursor.AlarmKind == "" || cursor.AlarmRunID == "" {
		return nil
	}
	completed, err := w.Ledger.RunCompleted(ctx, cursor.AlarmRunID)
	if err != nil {
		return err
	}
	if completed {
		return w.Store.ConfirmAlarm(ctx, t.IdentityID, t.AccountID, cursor.Resource)
	}
	if err := w.Store.RetellAlarm(ctx, t.IdentityID, t.AccountID, cursor.Resource, t.RunID); err != nil {
		return err
	}
	t.carry(phrasesFor(t.lang), cursor.AlarmKind, cursor.Resource, cursor.AlarmSince)
	return nil
}

// hasTime reports whether another page fits before the handler's deadline.
func (w *Watcher) hasTime(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return !ok || w.now().Add(PageReserve).Before(deadline)
}

// fail keeps a failed read on the cursor row. A transient failure raises one alarm after an hour,
// and a hard one is for the dispatcher's pause.
func (w *Watcher) fail(ctx context.Context, t *tick, resource string, err error) error {
	class, code := failureClass(err)
	if class == FailureUnsupported {
		return w.Store.MarkUnsupported(ctx, t.IdentityID, t.AccountID, resource)
	}
	cursor, err := w.Store.MarkFailing(ctx, t.IdentityID, t.AccountID, resource, code, w.now())
	if err != nil {
		return err
	}
	overdue := cursor.AlarmKind == "" && w.now().Sub(cursor.FailingSince) >= failingAlarmAfter
	if class == FailureHard {
		t.hard = &HardFailure{AccountID: t.AccountID, Resource: resource, Code: code, Overdue: overdue}
		if overdue {
			return w.Store.RaiseFailingAlarm(ctx, t.IdentityID, t.AccountID, resource, "")
		}
		return nil
	}
	t.failing = append(t.failing, resource+" "+code)
	if !overdue {
		return nil
	}
	if err := w.Store.RaiseFailingAlarm(ctx, t.IdentityID, t.AccountID, resource, t.RunID); err != nil {
		return err
	}
	t.carry(phrasesFor(t.lang), AlarmFailing, resource, cursor.FailingSince)
	return nil
}

// commit decides a page and writes it. resetRequired moves the cursor and raises the alarm that
// names the gap: it must never read as "nothing new".
func (w *Watcher) commit(ctx context.Context, t *tick, resource string, cursor Cursor, page ChangesPage) error {
	now := w.now()
	commit := PageCommit{
		AccountID: t.AccountID, Resource: resource, Cursor: page.NextCursor, RunID: t.RunID,
		Reset: page.ResetRequired, DayStart: dayStart(now, t.loc), Now: now, Cap: t.settings.DailyAlertCap,
	}
	lines := map[string]Line{}
	if !page.ResetRequired {
		drafts, drafted, err := w.draft(ctx, t, resource, page.Changes)
		if err != nil {
			return err
		}
		commit.Drafts, lines = drafts, drafted
	}
	result, err := w.Store.CommitPage(ctx, t.IdentityID, commit)
	if err != nil {
		return err
	}
	if page.ResetRequired {
		t.carry(phrasesFor(t.lang), AlarmGap, resource, cursor.AdvancedAt)
	}
	t.decided += result.Decided
	t.capped += result.Capped
	for _, id := range result.Alerted {
		t.lines = append(t.lines, lines[id])
	}
	return nil
}

func dayStart(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// draft decides a page's changes. Each id is decided once per page, on its last change.
func (w *Watcher) draft(ctx context.Context, t *tick, resource string, changes []Change) ([]VerdictDraft, map[string]Line, error) {
	lines := make(map[string]Line, len(changes))
	if resource == ResourceCalendar {
		return w.draftEvents(ctx, t, changes)
	}
	drafts := make([]VerdictDraft, 0, len(changes))
	for _, change := range changes {
		if change.Type != ChangeAdded {
			continue
		}
		decision := DecideMail(t.book, change.From, change.Signals)
		drafts = append(drafts, VerdictDraft{ItemID: change.ID, Decision: decision})
		lines[change.ID] = Line{
			AccountID: t.AccountID, Resource: resource, ItemID: change.ID, From: change.From, FromName: change.FromName,
			Subject: change.Subject, AuthFailed: decision.AuthFailed, Tier: decision.Tier,
			invitation: change.Signals.Invitation,
		}
	}
	return drafts, lines, nil
}

func (w *Watcher) draftEvents(ctx context.Context, t *tick, changes []Change) ([]VerdictDraft, map[string]Line, error) {
	latest := make(map[string]Change, len(changes))
	order := make([]string, 0, len(changes))
	for _, change := range changes {
		if _, seen := latest[change.ID]; !seen {
			order = append(order, change.ID)
		}
		latest[change.ID] = change
	}
	priors, err := w.Store.PriorEvents(ctx, t.IdentityID, t.AccountID, ResourceCalendar, order)
	if err != nil {
		return nil, nil, err
	}
	now := w.now()
	drafts := make([]VerdictDraft, 0, len(order))
	lines := make(map[string]Line, len(order))
	for _, id := range order {
		change := latest[id]
		facts, start := EventFacts{Type: change.Type}, EventTime{}
		if event := change.Event; event != nil {
			start = event.Start
			facts.OrganizerIsSelf, facts.Fingerprint = event.OrganizerIsSelf, event.Fingerprint
			facts.Cancelled = event.Status == EventCancelled
		} else {
			facts.OrganizerIsSelf = priors[id].OrganizerIsSelf
		}
		prior, seen := priors[id]
		facts.Prior, facts.PriorWord, facts.Seen = prior.Fingerprint, prior.Word, seen
		facts.KeptStart = prior.Start.In(t.loc)
		if change.Type == ChangeDeleted && start.IsZero() {
			start = prior.Start
		}
		facts.Start = start.In(t.loc)
		decision, told := DecideEvent(now, facts)
		if !told {
			continue
		}
		line := Line{
			AccountID: t.AccountID, Resource: ResourceCalendar, ItemID: id, CalendarID: change.CalendarID,
			From: change.From, Subject: change.Subject, Start: start.Time, AllDay: start.AllDay, Tier: decision.Tier,
			Change: decision.Change,
		}
		if i := t.invitationTo(change.Subject); decision.Tier == TierNow && i >= 0 {
			t.lines[i] = line
			decision.Tier = TierToday
		}
		drafts = append(drafts, VerdictDraft{
			ItemID: id, CalendarID: change.CalendarID, Decision: decision, Fingerprint: facts.fingerprintOf(), Event: true, Start: start,
			OrganizerIsSelf: facts.OrganizerIsSelf,
		})
		lines[id] = line
	}
	return drafts, lines, nil
}

// invitationTo finds this tick's alert for an invitation to the event with this title, or -1. On
// the live run of 2026-11-02 every change to a meeting alerted twice, the mail and the event in
// one tick. So the event's line, which carries the time, takes the mail's place and its row stays
// `today`: one alert of the cap, and neither is lost. A title the subject does not hold leaves the
// two apart, because a double costs less than a meeting nobody was told about.
func (t *tick) invitationTo(title string) int {
	if title == "" {
		return -1
	}
	title = strings.ToLower(title)
	return slices.IndexFunc(t.lines, func(line Line) bool {
		return line.invitation && strings.Contains(strings.ToLower(line.Subject), title)
	})
}

// outcome closes the run. A hard failure with something to tell tells it and completes: the
// dispatcher would drop the notice of a failed run, and the next tick fails on its own. The
// summary holds counts and codes only, because agent_job_runs.summary must never hold sender or
// subject.
func (t *tick) outcome() (Outcome, error) {
	out := renderAlert(t.lang, t.loc, t.AccountID, t.lines, t.capped, t.alarms)
	out.Summary = t.summary()
	if t.hard != nil && out.Notice == "" {
		return out, t.hard
	}
	return out, nil
}

func (t *tick) summary() string {
	parts := []string{fmt.Sprintf("%d decided, %d alerted, %d capped", t.decided, len(t.lines), t.capped)}
	if len(t.failing) > 0 {
		parts = append(parts, "failing: "+strings.Join(t.failing, ", "))
	}
	if t.hard != nil {
		parts = append(parts, "hard failure: "+t.hard.Resource+" "+t.hard.Code)
	}
	if t.backlog {
		parts = append(parts, "backlog continues on the next tick")
	}
	return strings.Join(parts, "; ")
}

// readLines reads verdicts' sender and subject live, one account and resource at a time. A mail
// that is gone comes back marked so.
func readLines(ctx context.Context, pim PIMReader, accountID string, verdicts []Verdict) ([]Line, error) {
	lines := make([]Line, 0, len(verdicts))
	for _, resource := range Resources {
		refs := make([]ItemRef, 0, len(verdicts))
		for _, verdict := range verdicts {
			if verdict.Resource == resource {
				refs = append(refs, ItemRef{ID: verdict.ItemID, CalendarID: verdict.CalendarID})
			}
		}
		if len(refs) == 0 {
			continue
		}
		result, err := pim.Items(ctx, accountID, resource, refs)
		if err != nil {
			return nil, err
		}
		items := make(map[string]Item, len(result.Items))
		for _, item := range result.Items {
			items[item.ID] = item
		}
		for _, verdict := range verdicts {
			if verdict.Resource == resource {
				lines = append(lines, lineOf(verdict, items))
			}
		}
	}
	return lines, nil
}

func lineOf(verdict Verdict, items map[string]Item) Line {
	line := Line{
		AccountID: verdict.AccountID, Resource: verdict.Resource, ItemID: verdict.ItemID,
		CalendarID: verdict.CalendarID, AuthFailed: verdict.AuthFailed, Tier: verdict.Tier, LabelID: verdict.LabelID,
		Start: verdict.Start.Time, AllDay: verdict.Start.AllDay, Change: verdict.Change,
	}
	item, ok := items[verdict.ItemID]
	if !ok {
		// A cancelled event being gone is the news, and the row still says when it was.
		line.Gone = verdict.Change != WordCancelled
		return line
	}
	line.From, line.FromName, line.Subject = item.From, item.FromName, item.Subject
	if item.Event != nil {
		line.Start, line.AllDay = item.Event.Start.Time, item.Event.Start.AllDay
		if item.Event.Status == EventCancelled {
			line.Change = WordCancelled
		}
	}
	return line
}
