package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrUnavailable says this deployment cannot watch mail: it has no PIM sidecar, or no way to
// fetch an identity's grant to it.
var ErrUnavailable = errors.New("the mail watch needs the PIM sidecar and its authorization, and this deployment lacks them")

// decisionsLimit bounds the decisions view of one day.
const decisionsLimit = 200

// TaskPayload is a watch or digest task's payload. Text titles the conversation the task opens.
type TaskPayload struct {
	AccountID string `json:"account_id,omitempty"`
	Text      string `json:"text"`
}

// Delivery is where a task reports: a route and the conversation that keeps the record.
type Delivery struct {
	Route                string
	OriginConversationID string
	TelegramRecipients   []int64
}

// WatchTask is a live scheduler task of the watch.
type WatchTask struct {
	ID           string
	AccountID    string
	Paused       bool
	PausedReason string
	NextRunAt    time.Time
}

// WatchSpec describes a new account watch. Zone is the identity's: quiet hours read it.
type WatchSpec struct {
	AccountID    string
	EveryMinutes int
	Zone         string
	Title        string
	Delivery     Delivery
}

// DigestSpec describes an identity's daily digest.
type DigestSpec struct {
	Hour     int
	Zone     string
	Title    string
	Delivery Delivery
}

// Tasks is the scheduler seen from the watch. CreateWatch and CreateDigest treat a task that
// already exists as done: the database keeps one live task per account and one digest per identity.
type Tasks interface {
	Watches(ctx context.Context, identityID string) ([]WatchTask, error)
	Digests(ctx context.Context, identityID string) ([]WatchTask, error)
	CreateWatch(ctx context.Context, identityID string, spec WatchSpec) error
	CreateDigest(ctx context.Context, identityID string, spec DigestSpec) error
	RescheduleDigest(ctx context.Context, taskID string, spec DigestSpec) error
	Resume(ctx context.Context, taskID string) error
	Cancel(ctx context.Context, taskID string) error
}

// Routes finds where an identity's watch reports.
type Routes interface {
	DeliveryFor(ctx context.Context, identityID string) (Delivery, error)
}

// Service is what the cockpit and the mail_watch tool share, so a switch, a correction and a rule
// mean the same thing in both. PIM is nil when the deployment cannot reach the sidecar: reads of
// what is stored and turning a watch off still work.
type Service struct {
	Store  *Store
	PIM    PIMReader
	Tasks  Tasks
	Routes Routes
	Locale Locale
	// Now overrides the clock in tests; nil is time.Now.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Configured reports whether a watch can be enabled here.
func (s *Service) Configured() bool { return s.PIM != nil }

// Enable turns an account's watch on, and is safe to repeat. A running watch stays as it is, a
// paused one resumes, and a new one starts from now: nothing already in the mailbox is read. The
// watch goes live only when every resource is settled, so a call that fails leaves nothing running
// and can simply be repeated.
func (s *Service) Enable(ctx context.Context, identityID, accountID string) error {
	if s.PIM == nil {
		return ErrUnavailable
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("%w: an account id is required", ErrInvalid)
	}
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil {
		return err
	}
	settings, err := s.Store.Settings(ctx, identityID)
	if err != nil {
		return err
	}
	if existing, ok := findWatch(watches, accountID); ok {
		if existing.Paused {
			if err := s.Tasks.Resume(ctx, existing.ID); err != nil {
				return err
			}
		}
		return s.ensureDigest(ctx, identityID, settings)
	}
	lang := s.Locale.Language(ctx, identityID)
	if err := s.Store.SeedLabels(ctx, identityID, DefaultLabels(lang)); err != nil {
		return err
	}
	if err := s.startCursors(ctx, identityID, accountID); err != nil {
		return err
	}
	delivery, err := s.Routes.DeliveryFor(ctx, identityID)
	if err != nil {
		return err
	}
	spec := WatchSpec{
		AccountID: accountID, EveryMinutes: settings.IntervalMinutes, Zone: s.Locale.Location(ctx, identityID).String(),
		Title: phrasesFor(lang).watchTitle(accountID), Delivery: delivery,
	}
	if err := s.Tasks.CreateWatch(ctx, identityID, spec); err != nil {
		return err
	}
	return s.ensureDigest(ctx, identityID, settings)
}

// startCursors starts what the account lets the watch read. A resource that refuses the permission
// is left out like one the provider lacks, so an account granted for mail alone is still watched,
// and the refusal stands when nothing else started. Any other failure stops the enable.
func (s *Service) startCursors(ctx context.Context, identityID, accountID string) error {
	var refused error
	for _, resource := range Resources {
		err := StartResource(ctx, s.PIM, s.Store, identityID, accountID, resource)
		if _, code := failureClass(err); code == "permission_denied" {
			refused = fmt.Errorf("start the %s cursor: %w", resource, err)
			err = s.Store.StartCursor(ctx, identityID, accountID, resource, "", CursorUnsupported)
		}
		if err != nil {
			return fmt.Errorf("start the %s cursor: %w", resource, err)
		}
	}
	if refused == nil {
		return nil
	}
	cursors, err := s.Store.Cursors(ctx, identityID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(cursors, func(c Cursor) bool { return c.AccountID == accountID && c.Status == CursorOK }) {
		return nil
	}
	return refused
}

func findWatch(watches []WatchTask, accountID string) (WatchTask, bool) {
	for _, watch := range watches {
		if watch.AccountID == accountID {
			return watch, true
		}
	}
	return WatchTask{}, false
}

// ensureDigest makes sure the identity's one digest is running at its hour, in its zone. Turning a
// watch on is asking for the digest too, so a paused one resumes.
func (s *Service) ensureDigest(ctx context.Context, identityID string, settings Settings) error {
	spec, err := s.digestSpec(ctx, identityID, settings)
	if err != nil {
		return err
	}
	digests, err := s.Tasks.Digests(ctx, identityID)
	if err != nil {
		return err
	}
	if len(digests) == 0 {
		return s.Tasks.CreateDigest(ctx, identityID, spec)
	}
	if digests[0].Paused {
		if err := s.Tasks.Resume(ctx, digests[0].ID); err != nil {
			return err
		}
	}
	return s.Tasks.RescheduleDigest(ctx, digests[0].ID, spec)
}

func (s *Service) digestSpec(ctx context.Context, identityID string, settings Settings) (DigestSpec, error) {
	delivery, err := s.Routes.DeliveryFor(ctx, identityID)
	if err != nil {
		return DigestSpec{}, err
	}
	return DigestSpec{
		Hour: settings.DigestHour, Zone: s.Locale.Location(ctx, identityID).String(),
		Title: phrasesFor(s.Locale.Language(ctx, identityID)).digestTask, Delivery: delivery,
	}, nil
}

// Disable turns an account's watch off and forgets where it stood, so a later enable starts from
// then. Its verdicts stay until they expire, because the day's digest still lists them. The digest
// goes with the last watch.
func (s *Service) Disable(ctx context.Context, identityID, accountID string) error {
	if err := s.stopWatch(ctx, identityID, accountID); err != nil {
		return err
	}
	if err := s.Store.DeleteCursors(ctx, identityID, accountID); err != nil {
		return err
	}
	return s.stopDigestIfIdle(ctx, identityID)
}

// RemoveAccount forgets everything the watch holds for an account that left. The sender rules stay:
// they are the person's words about senders, not about this account.
func (s *Service) RemoveAccount(ctx context.Context, identityID, accountID string) error {
	if err := s.stopWatch(ctx, identityID, accountID); err != nil {
		return err
	}
	if err := s.Store.DeleteAccount(ctx, identityID, accountID); err != nil {
		return err
	}
	return s.stopDigestIfIdle(ctx, identityID)
}

func (s *Service) stopWatch(ctx context.Context, identityID, accountID string) error {
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil {
		return err
	}
	if watch, ok := findWatch(watches, accountID); ok {
		return s.Tasks.Cancel(ctx, watch.ID)
	}
	return nil
}

func (s *Service) stopDigestIfIdle(ctx context.Context, identityID string) error {
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil || len(watches) > 0 {
		return err
	}
	digests, err := s.Tasks.Digests(ctx, identityID)
	if err != nil {
		return err
	}
	var failures []error
	for _, digest := range digests {
		if err := s.Tasks.Cancel(ctx, digest.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// ResourceStatus is one cursor as a person should read it.
type ResourceStatus struct {
	Resource     string
	Status       string
	LastError    string
	FailingSince time.Time
	AdvancedAt   time.Time
}

// AccountStatus is one watched account.
type AccountStatus struct {
	AccountID    string
	Paused       bool
	PausedReason string
	NextRunAt    time.Time
	AlertsToday  int
	Resources    []ResourceStatus
}

// Status is everything a person needs to see about their watch.
type Status struct {
	Configured bool
	Settings   Settings
	Accounts   []AccountStatus
}

// Status reads the identity's watch: its settings, and for each live task how far it got.
func (s *Service) Status(ctx context.Context, identityID string) (Status, error) {
	settings, err := s.Store.Settings(ctx, identityID)
	if err != nil {
		return Status{}, err
	}
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil {
		return Status{}, err
	}
	cursors, err := s.Store.Cursors(ctx, identityID)
	if err != nil {
		return Status{}, err
	}
	today := dayStart(s.now(), s.Locale.Location(ctx, identityID))
	out := Status{Configured: s.Configured(), Settings: settings, Accounts: make([]AccountStatus, 0, len(watches))}
	for _, watch := range watches {
		alerts, err := s.Store.AlertsToday(ctx, identityID, watch.AccountID, today)
		if err != nil {
			return Status{}, err
		}
		account := AccountStatus{
			AccountID: watch.AccountID, Paused: watch.Paused, PausedReason: watch.PausedReason,
			NextRunAt: watch.NextRunAt, AlertsToday: alerts,
		}
		for _, cursor := range cursors {
			if cursor.AccountID == watch.AccountID {
				account.Resources = append(account.Resources, ResourceStatus{
					Resource: cursor.Resource, Status: cursor.Status, LastError: cursor.LastError,
					FailingSince: cursor.FailingSince, AdvancedAt: cursor.AdvancedAt,
				})
			}
		}
		out.Accounts = append(out.Accounts, account)
	}
	return out, nil
}

// DecisionLine is one verdict with its sender and subject read live.
type DecisionLine struct {
	Line
	Label     string
	By        DecidedBy
	DecidedAt time.Time
}

// Decisions lists an account's verdicts of one local day, newest first, read live by id. day is
// YYYY-MM-DD in the identity's zone; empty means today.
func (s *Service) Decisions(ctx context.Context, identityID, accountID, day string) ([]DecisionLine, error) {
	if s.PIM == nil {
		return nil, ErrUnavailable
	}
	loc := s.Locale.Location(ctx, identityID)
	start := dayStart(s.now(), loc)
	if day != "" {
		parsed, err := time.ParseInLocation(time.DateOnly, day, loc)
		if err != nil {
			return nil, fmt.Errorf("%w: the day is YYYY-MM-DD", ErrInvalid)
		}
		start = parsed
	}
	verdicts, err := s.Store.AccountVerdicts(ctx, identityID, accountID, start, start.AddDate(0, 0, 1), decisionsLimit)
	if err != nil {
		return nil, err
	}
	book, err := s.Store.Book(ctx, identityID)
	if err != nil {
		return nil, err
	}
	lines, err := readLines(ctx, s.PIM, accountID, verdicts)
	if err != nil {
		return nil, err
	}
	read := make(map[string]Line, len(lines))
	for _, line := range lines {
		read[verdictKey(line.AccountID, line.Resource, line.ItemID)] = line
	}
	out := make([]DecisionLine, 0, len(verdicts))
	for _, verdict := range verdicts {
		out = append(out, DecisionLine{
			Line:  read[verdictKey(verdict.AccountID, verdict.Resource, verdict.ItemID)],
			Label: book.LabelName(verdict.LabelID), By: verdict.By, DecidedAt: verdict.DecidedAt,
		})
	}
	return out, nil
}

// SaveSettings changes the digest hour and the daily cap, and moves the digest to the new hour. A
// paused digest cannot be rescheduled, so it stays paused and takes the hour from the next enable.
func (s *Service) SaveSettings(ctx context.Context, identityID string, digestHour, dailyCap int) (Settings, error) {
	settings, err := s.Store.SaveSettings(ctx, identityID, digestHour, dailyCap)
	if err != nil {
		return Settings{}, err
	}
	digests, err := s.Tasks.Digests(ctx, identityID)
	if err != nil {
		return Settings{}, err
	}
	if len(digests) == 0 || digests[0].Paused {
		return settings, nil
	}
	spec, err := s.digestSpec(ctx, identityID, settings)
	if err != nil {
		return Settings{}, err
	}
	return settings, s.Tasks.RescheduleDigest(ctx, digests[0].ID, spec)
}
