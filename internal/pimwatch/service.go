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

// correctionWindow is how far apart two corrections must be to teach a rule, so that one burst of
// sorting is one opinion (spec §5.4).
const correctionWindow = 15 * time.Minute

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

// WatchSpec describes a new account watch.
type WatchSpec struct {
	AccountID    string
	EveryMinutes int
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
// watch goes live only when both resources have their cursor, so a call that fails leaves nothing
// running and can simply be repeated.
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
	for _, resource := range Resources {
		if err := StartResource(ctx, s.PIM, s.Store, identityID, accountID, resource); err != nil {
			return fmt.Errorf("start the %s cursor: %w", resource, err)
		}
	}
	delivery, err := s.Routes.DeliveryFor(ctx, identityID)
	if err != nil {
		return err
	}
	spec := WatchSpec{
		AccountID: accountID, EveryMinutes: settings.IntervalMinutes,
		Title: fmt.Sprintf(phrasesFor(lang).watchTask, accountID), Delivery: delivery,
	}
	if err := s.Tasks.CreateWatch(ctx, identityID, spec); err != nil {
		return err
	}
	return s.ensureDigest(ctx, identityID, settings)
}

func findWatch(watches []WatchTask, accountID string) (WatchTask, bool) {
	for _, watch := range watches {
		if watch.AccountID == accountID {
			return watch, true
		}
	}
	return WatchTask{}, false
}

// ensureDigest makes sure the identity's one digest exists at its hour, in its zone.
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

// StopIdentity cancels every task of an identity that is going away, leaving nothing to fire.
func (s *Service) StopIdentity(ctx context.Context, identityID string) error {
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil {
		return err
	}
	digests, err := s.Tasks.Digests(ctx, identityID)
	if err != nil {
		return err
	}
	var failures []error
	for _, task := range append(watches, digests...) {
		if err := s.Tasks.Cancel(ctx, task.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
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
	since := dayStart(s.now(), s.Locale.Location(ctx, identityID))
	out := Status{Configured: s.Configured(), Settings: settings, Accounts: make([]AccountStatus, 0, len(watches))}
	for _, watch := range watches {
		alerts, err := s.Store.AlertsSince(ctx, identityID, watch.AccountID, since)
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

// Correct changes a verdict's tier or label and learns from it. A sender corrected the same way
// twice, outside one burst, gets a rule, and tier and label are learned apart. The sender is read
// live by id, because no verdict keeps one, and a mail that cannot be read changes its verdict and
// teaches nothing. A calendar item teaches nothing: rules do not apply to events.
func (s *Service) Correct(ctx context.Context, identityID string, c Correction) (Verdict, *Rule, error) {
	if err := s.checkCorrection(ctx, identityID, c); err != nil {
		return Verdict{}, nil, err
	}
	sender := s.senderOf(ctx, c)
	var past []PastCorrection
	if sender != "" {
		var err error
		past, err = s.Store.CorrectionsBefore(ctx, identityID, sender, s.now().Add(-correctionWindow))
		if err != nil {
			return Verdict{}, nil, err
		}
	}
	verdict, err := s.Store.ApplyCorrection(ctx, identityID, sender, c)
	if err != nil {
		return Verdict{}, nil, err
	}
	tier, label := learned(c, past)
	if tier == "" && label == "" {
		return verdict, nil, nil
	}
	rule, err := s.Store.LearnRule(ctx, identityID, sender, tier, label)
	if err != nil {
		return verdict, nil, err
	}
	return verdict, &rule, nil
}

func (s *Service) checkCorrection(ctx context.Context, identityID string, c Correction) error {
	if c.Tier == "" && c.LabelID == nil {
		return fmt.Errorf("%w: a correction changes a tier or a label", ErrInvalid)
	}
	if c.Tier != "" && !c.Tier.Valid() {
		return fmt.Errorf("%w: the tier is now, today or later", ErrInvalid)
	}
	if c.LabelID == nil || *c.LabelID == "" {
		return nil
	}
	return s.checkLabel(ctx, identityID, *c.LabelID)
}

func (s *Service) checkLabel(ctx context.Context, identityID, labelID string) error {
	labels, err := s.Store.Labels(ctx, identityID)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(labels, func(l Label) bool { return l.ID == labelID }) {
		return fmt.Errorf("%w: no such label", ErrInvalid)
	}
	return nil
}

// senderOf reads a mail's sender and returns its key, or "" when it cannot.
func (s *Service) senderOf(ctx context.Context, c Correction) string {
	if s.PIM == nil || c.Resource != ResourceMail {
		return ""
	}
	result, err := s.PIM.Items(ctx, c.AccountID, c.Resource, []ItemRef{{ID: c.ItemID}})
	if err != nil || len(result.Items) == 0 {
		return ""
	}
	address, _ := SenderKey(result.Items[0].From)
	return address
}

// learned picks what an earlier correction agrees with. A correction clearing a label teaches
// nothing, because a rule cannot say "no label".
func learned(c Correction, past []PastCorrection) (tier Tier, labelID string) {
	for _, earlier := range past {
		if c.Tier != "" && earlier.Tier == c.Tier {
			tier = c.Tier
		}
		if c.LabelID != nil && *c.LabelID != "" && earlier.LabelID == *c.LabelID {
			labelID = *c.LabelID
		}
	}
	return tier, labelID
}

// RuleInput is a rule as a person gives it: a sender or a domain, then a tier and/or a label.
type RuleInput struct {
	Key     string
	Tier    Tier
	LabelID string
}

// AddRule writes a person's rule.
func (s *Service) AddRule(ctx context.Context, identityID string, in RuleInput) (Rule, error) {
	key, kind, err := parseRuleKey(in.Key)
	if err != nil {
		return Rule{}, err
	}
	if in.Tier == "" && in.LabelID == "" {
		return Rule{}, fmt.Errorf("%w: a rule gives a tier or a label", ErrInvalid)
	}
	if in.Tier != "" && !in.Tier.Valid() {
		return Rule{}, fmt.Errorf("%w: the tier is now, today or later", ErrInvalid)
	}
	if in.LabelID != "" {
		if err := s.checkLabel(ctx, identityID, in.LabelID); err != nil {
			return Rule{}, err
		}
	}
	return s.Store.PutRule(ctx, identityID, Rule{Key: key, Kind: kind, Tier: in.Tier, LabelID: in.LabelID})
}

// parseRuleKey reads a whole address as a sender and anything else as a domain.
func parseRuleKey(raw string) (string, KeyKind, error) {
	key := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
	if strings.Contains(key, "@") {
		if address, _ := SenderKey(key); address != "" {
			return address, KeySender, nil
		}
		return "", "", fmt.Errorf("%w: %q is not an address", ErrInvalid, raw)
	}
	if !strings.Contains(key, ".") || strings.ContainsAny(key, " \t\r\n") {
		return "", "", fmt.Errorf("%w: a rule names an address or a domain", ErrInvalid)
	}
	return key, KeyDomain, nil
}

// SaveSettings changes the digest hour and the daily cap, and moves the digest to the new hour. A
// paused digest stays paused and takes the hour from the next enable.
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

// LabelID finds a label by its name, the way a person says it.
func (s *Service) LabelID(ctx context.Context, identityID, name string) (string, error) {
	labels, err := s.Store.Labels(ctx, identityID)
	if err != nil {
		return "", err
	}
	for _, label := range labels {
		if strings.EqualFold(label.Name, strings.TrimSpace(name)) {
			return label.ID, nil
		}
	}
	return "", fmt.Errorf("%w: no label is called %q", ErrInvalid, name)
}

// AddLabel appends a label to the list.
func (s *Service) AddLabel(ctx context.Context, identityID, name string, tier Tier) (Label, error) {
	if err := checkLabelInput(name, tier); err != nil {
		return Label{}, err
	}
	labels, err := s.Store.Labels(ctx, identityID)
	if err != nil {
		return Label{}, err
	}
	position := 0
	for _, label := range labels {
		position = max(position, label.Position+1)
	}
	return s.Store.AddLabel(ctx, identityID, strings.TrimSpace(name), tier, position)
}

// UpdateLabel renames a label and sets its tier.
func (s *Service) UpdateLabel(ctx context.Context, identityID, labelID, name string, tier Tier) (Label, error) {
	if err := checkLabelInput(name, tier); err != nil {
		return Label{}, err
	}
	return s.Store.UpdateLabel(ctx, identityID, labelID, strings.TrimSpace(name), tier)
}

// labelNameRunes is what the labels table's CHECK allows.
const labelNameRunes = 60

func checkLabelInput(name string, tier Tier) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > labelNameRunes {
		return fmt.Errorf("%w: a label's name is 1 to %d characters", ErrInvalid, labelNameRunes)
	}
	if tier != "" && !tier.Valid() {
		return fmt.Errorf("%w: the tier is now, today or later", ErrInvalid)
	}
	return nil
}
