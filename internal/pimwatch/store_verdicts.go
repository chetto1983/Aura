package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Verdict is one row of the watch's state: an id and what was decided about it, never its text.
type Verdict struct {
	AccountID   string
	Resource    string
	ItemID      string
	CalendarID  string
	Tier        Tier
	LabelID     string
	By          DecidedBy
	DecidedAt   time.Time
	AuthFailed  bool
	Fingerprint string
	Start       EventTime
	Change      ChangeWord
	AlertRunID  string
	AlertedAt   time.Time
	Notified    bool
}

func verdictFromRow(r sqlc.AuraPimWatchVerdicts) Verdict {
	return Verdict{
		AccountID: r.AccountID, Resource: r.Resource, ItemID: r.ItemID, CalendarID: r.CalendarID,
		Tier: Tier(r.Tier), LabelID: r.LabelID.String(), By: DecidedBy(r.DecidedBy),
		DecidedAt: r.DecidedAt.Time, AuthFailed: r.AuthFailed, Fingerprint: r.Fingerprint,
		Start: EventTime{Time: r.EventStart.Time, AllDay: r.EventAllDay}, Change: ChangeWord(r.EventChange.String),
		AlertRunID: r.AlertRunID.String(), AlertedAt: r.AlertedAt.Time, Notified: r.Notified,
	}
}

// VerdictDraft is a decision on its way to a row. Event marks a calendar item, decided again each
// time its fingerprint moves, and Start is what its row keeps of it. A mail is decided once.
type VerdictDraft struct {
	ItemID      string
	CalendarID  string
	Decision    Decision
	Fingerprint string
	Event       bool
	Start       EventTime
}

// PageCommit is what one page of changes leaves behind, written in one transaction: "handled"
// means a verdict row, so the cursor moves only with the rows it covers (spec §5.2, §5.6).
type PageCommit struct {
	AccountID string
	Resource  string
	Cursor    string
	RunID     string
	// Reset commits a resetRequired page: a fresh cursor and an owed gap alarm.
	Reset  bool
	Drafts []VerdictDraft
	// DayStart is the local midnight the cap counts from, and Cap the identity's daily cap.
	DayStart time.Time
	Now      time.Time
	Cap      int
}

// PageResult says what the commit did. Alerted is the items this run must tell, and Capped counts
// the `now` the cap kept out of the notification.
type PageResult struct {
	Decided int
	Alerted []string
	Capped  int
}

// CommitPage writes a page's verdicts, its cursor and what it told in one transaction. A row that
// already exists tells nobody: a replayed page cannot double an alert. A `now` beyond the daily cap
// keeps its tier and is not told, so the digest still lists it. The cap counts pushes, on the
// cursor rows: a moved event is told again on the same verdict row.
func (s *Store) CommitPage(ctx context.Context, identityID string, p PageCommit) (PageResult, error) {
	var result PageResult
	run := optionalUUID(p.RunID)
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		result = PageResult{}
		told, err := q.CountPIMWatchAlertsOfDay(ctx, sqlc.CountPIMWatchAlertsOfDayParams{
			IdentityID: id, AccountID: p.AccountID, DayStart: timestamp(p.DayStart),
		})
		if err != nil {
			return fmt.Errorf("count alerts: %w", err)
		}
		for i, draft := range p.Drafts {
			// A page shares one clock and a provider's id is text, so ties would read as a shuffle:
			// IMAP's "10" sorts before "9". Each row is a microsecond after the last.
			written, err := writeVerdict(ctx, q, id, p, draft, p.Now.Add(time.Duration(i)*time.Microsecond))
			if err != nil {
				return err
			}
			if !written {
				continue
			}
			result.Decided++
			if draft.Decision.Tier != TierNow {
				continue
			}
			if int(told)+len(result.Alerted) >= p.Cap {
				result.Capped++
				continue
			}
			result.Alerted = append(result.Alerted, draft.ItemID)
			if err := q.MarkPIMWatchAlert(ctx, sqlc.MarkPIMWatchAlertParams{
				IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, ItemID: draft.ItemID,
				AlertRunID: run, AlertedAt: timestamp(p.Now),
			}); err != nil {
				return fmt.Errorf("mark alert: %w", err)
			}
		}
		if len(result.Alerted) > 0 {
			if err := q.TallyPIMWatchAlerts(ctx, sqlc.TallyPIMWatchAlertsParams{
				IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, DayStart: timestamp(p.DayStart),
				Told: int32(len(result.Alerted)), //nolint:gosec // G115: bounded by the page and the cap.
			}); err != nil {
				return fmt.Errorf("tally alerts: %w", err)
			}
		}
		return moveCursor(ctx, q, id, p, run)
	})
	if err != nil {
		return PageResult{}, fmt.Errorf("commit pim watch page: %w", err)
	}
	return result, nil
}

func writeVerdict(ctx context.Context, q *sqlc.Queries, id pgtype.UUID, p PageCommit, draft VerdictDraft, decidedAt time.Time) (bool, error) {
	var n int64
	var err error
	if draft.Event {
		n, err = q.UpsertPIMWatchEventVerdict(ctx, sqlc.UpsertPIMWatchEventVerdictParams{
			IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, ItemID: draft.ItemID,
			CalendarID: draft.CalendarID, Tier: string(draft.Decision.Tier), DecidedBy: string(draft.Decision.By),
			Fingerprint: draft.Fingerprint, DecidedAt: timestamp(decidedAt),
			EventStart: timestamp(draft.Start.Time), EventAllDay: draft.Start.AllDay,
			EventChange: optionalText(string(draft.Decision.Change)),
		})
	} else {
		n, err = q.InsertPIMWatchVerdict(ctx, sqlc.InsertPIMWatchVerdictParams{
			IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, ItemID: draft.ItemID,
			CalendarID: draft.CalendarID, Tier: string(draft.Decision.Tier), LabelID: optionalUUID(draft.Decision.LabelID),
			DecidedBy: string(draft.Decision.By), AuthFailed: draft.Decision.AuthFailed, Fingerprint: draft.Fingerprint,
			DecidedAt: timestamp(decidedAt),
		})
	}
	if err != nil {
		return false, fmt.Errorf("write verdict: %w", err)
	}
	return n == 1, nil
}

func moveCursor(ctx context.Context, q *sqlc.Queries, id pgtype.UUID, p PageCommit, run pgtype.UUID) error {
	var err error
	if p.Reset {
		err = q.ResetPIMWatchCursor(ctx, sqlc.ResetPIMWatchCursorParams{
			IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, Cursor: p.Cursor, AlarmRunID: run,
		})
	} else {
		err = q.AdvancePIMWatchCursor(ctx, sqlc.AdvancePIMWatchCursorParams{
			IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, Cursor: p.Cursor,
		})
	}
	if err != nil {
		return fmt.Errorf("move cursor: %w", err)
	}
	return nil
}

// PriorEvent is what a row remembers of an event: the fingerprint that tells a move from a new
// description, and the start a deleted event no longer names.
type PriorEvent struct {
	Fingerprint string
	Start       EventTime
}

// PriorEvents reads what the rows of these items remember. An item with no row has no key.
func (s *Store) PriorEvents(ctx context.Context, identityID, accountID, resource string, ids []string) (map[string]PriorEvent, error) {
	out := make(map[string]PriorEvent, len(ids))
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		rows, err := q.ListPIMWatchPriorEvents(ctx, sqlc.ListPIMWatchPriorEventsParams{
			IdentityID: id, AccountID: accountID, Resource: resource, ItemIds: ids,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[r.ItemID] = PriorEvent{Fingerprint: r.Fingerprint, Start: EventTime{Time: r.EventStart.Time, AllDay: r.EventAllDay}}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read pim watch prior events: %w", err)
	}
	return out, nil
}

// AlertsToday counts what an account pushed since a local midnight, the cap's own measure.
func (s *Store) AlertsToday(ctx context.Context, identityID, accountID string, dayStart time.Time) (int, error) {
	var n int32
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		n, err = q.CountPIMWatchAlertsOfDay(ctx, sqlc.CountPIMWatchAlertsOfDayParams{
			IdentityID: id, AccountID: accountID, DayStart: timestamp(dayStart),
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("count pim watch alerts: %w", err)
	}
	return int(n), nil
}

// OwedAlerts lists the verdicts told by a run not yet seen to complete, oldest first.
func (s *Store) OwedAlerts(ctx context.Context, identityID, accountID string) ([]Verdict, error) {
	verdicts, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchVerdicts, error) {
		return q.ListPIMWatchOwedAlerts(ctx, sqlc.ListPIMWatchOwedAlertsParams{IdentityID: id, AccountID: accountID})
	}, verdictFromRow)
	if err != nil {
		return nil, fmt.Errorf("list owed pim watch alerts: %w", err)
	}
	return verdicts, nil
}

// ConfirmAlerts records that runID's alerts went out.
func (s *Store) ConfirmAlerts(ctx context.Context, identityID, accountID, runID string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.ConfirmPIMWatchAlerts(ctx, sqlc.ConfirmPIMWatchAlertsParams{
			IdentityID: id, AccountID: accountID, AlertRunID: optionalUUID(runID),
		})
	})
	if err != nil {
		return fmt.Errorf("confirm pim watch alerts: %w", err)
	}
	return nil
}

// RetellAlerts hands a dead run's alerts to newRun.
func (s *Store) RetellAlerts(ctx context.Context, identityID, accountID, oldRun, newRun string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.RetellPIMWatchAlerts(ctx, sqlc.RetellPIMWatchAlertsParams{
			IdentityID: id, AccountID: accountID, NewRun: optionalUUID(newRun), OldRun: optionalUUID(oldRun),
		})
	})
	if err != nil {
		return fmt.Errorf("retell pim watch alerts: %w", err)
	}
	return nil
}

// DigestVerdicts lists every `now` and `today` decided in (since, until], oldest first.
func (s *Store) DigestVerdicts(ctx context.Context, identityID string, since, until time.Time) ([]Verdict, error) {
	verdicts, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchVerdicts, error) {
		return q.ListPIMWatchDigestVerdicts(ctx, sqlc.ListPIMWatchDigestVerdictsParams{
			IdentityID: id, Since: timestamp(since), Until: timestamp(until),
		})
	}, verdictFromRow)
	if err != nil {
		return nil, fmt.Errorf("list pim watch digest verdicts: %w", err)
	}
	return verdicts, nil
}

// AccountVerdicts lists an account's verdicts decided in [since, until), newest first.
func (s *Store) AccountVerdicts(ctx context.Context, identityID, accountID string, since, until time.Time, limit int) ([]Verdict, error) {
	verdicts, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchVerdicts, error) {
		return q.ListPIMWatchAccountVerdicts(ctx, sqlc.ListPIMWatchAccountVerdictsParams{
			IdentityID: id, AccountID: accountID, Since: timestamp(since), Until: timestamp(until),
			RowLimit: int32(limit), //nolint:gosec // G115: a caller's small bound.
		})
	}, verdictFromRow)
	if err != nil {
		return nil, fmt.Errorf("list pim watch decisions: %w", err)
	}
	return verdicts, nil
}

// ExpireVerdicts removes one bounded batch of what has outlived the identity's retention and
// reports how many rows left.
func (s *Store) ExpireVerdicts(ctx context.Context, identityID string, now time.Time, batch int) (int, error) {
	var n int64
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		n, err = q.DeleteExpiredPIMWatchVerdicts(ctx, sqlc.DeleteExpiredPIMWatchVerdictsParams{
			IdentityID: id, Now: timestamp(now), Batch: int32(batch), //nolint:gosec // G115: a constant.
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("expire pim watch verdicts: %w", err)
	}
	return int(n), nil
}

// Correction is what a person changed about one verdict. An empty Tier leaves the tier, and
// LabelID is nil when the label stays; an empty LabelID clears it.
type Correction struct {
	AccountID string
	Resource  string
	ItemID    string
	Tier      Tier
	LabelID   *string
}

// ApplyCorrection changes the verdict and, when the sender is known, stores the correction, in one
// transaction. It returns the verdict as it now stands.
func (s *Store) ApplyCorrection(ctx context.Context, identityID, senderKey string, c Correction) (Verdict, error) {
	var out Verdict
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		current, err := q.GetPIMWatchVerdict(ctx, sqlc.GetPIMWatchVerdictParams{
			IdentityID: id, AccountID: c.AccountID, Resource: c.Resource, ItemID: c.ItemID,
		})
		if err != nil {
			return err
		}
		tier, label := current.Tier, current.LabelID
		if c.Tier != "" {
			tier = string(c.Tier)
		}
		if c.LabelID != nil {
			label = optionalUUID(*c.LabelID)
		}
		row, err := q.CorrectPIMWatchVerdict(ctx, sqlc.CorrectPIMWatchVerdictParams{
			IdentityID: id, AccountID: c.AccountID, Resource: c.Resource, ItemID: c.ItemID, Tier: tier, LabelID: label,
		})
		if err != nil {
			return err
		}
		out = verdictFromRow(row)
		if senderKey == "" || current.AuthFailed {
			return nil
		}
		correction := sqlc.InsertPIMWatchCorrectionParams{
			IdentityID: id, AccountID: c.AccountID, SenderKey: senderKey, Tier: optionalText(string(c.Tier)),
		}
		if c.LabelID != nil {
			correction.LabelID = optionalUUID(*c.LabelID)
		}
		return q.InsertPIMWatchCorrection(ctx, correction)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Verdict{}, fmt.Errorf("correct verdict: %w", ErrNotFound)
	}
	if err != nil {
		return Verdict{}, fmt.Errorf("correct verdict: %w", err)
	}
	return out, nil
}

// PastCorrection is an earlier correction for a sender.
type PastCorrection struct {
	Tier    Tier
	LabelID string
}

// CorrectionsBefore lists a sender's corrections older than an instant, newest first.
func (s *Store) CorrectionsBefore(ctx context.Context, identityID, senderKey string, before time.Time) ([]PastCorrection, error) {
	past, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchCorrections, error) {
		return q.ListPIMWatchCorrectionsBefore(ctx, sqlc.ListPIMWatchCorrectionsBeforeParams{
			IdentityID: id, SenderKey: senderKey, Before: timestamp(before),
		})
	}, func(r sqlc.AuraPimWatchCorrections) PastCorrection {
		return PastCorrection{Tier: Tier(r.Tier.String), LabelID: r.LabelID.String()}
	})
	if err != nil {
		return nil, fmt.Errorf("list pim watch corrections: %w", err)
	}
	return past, nil
}

// LearnedRule is what a repeated correction taught. Created says the sender had no rule. One that
// merged into a rule on file holds the person's earlier words, so nothing can simply remove it.
type LearnedRule struct {
	Rule
	Created bool
}

// LearnRule writes what a repeated correction taught, keeping whatever the sender's rule already
// says about the field this one leaves empty.
func (s *Store) LearnRule(ctx context.Context, identityID, senderKey string, tier Tier, labelID string) (LearnedRule, error) {
	var out LearnedRule
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		onFile, err := q.PIMWatchRuleExists(ctx, sqlc.PIMWatchRuleExistsParams{IdentityID: id, KeyKind: string(KeySender), Key: senderKey})
		if err != nil {
			return err
		}
		row, err := q.MergePIMWatchRule(ctx, sqlc.MergePIMWatchRuleParams{
			IdentityID: id, Key: senderKey, KeyKind: string(KeySender), Tier: optionalText(string(tier)),
			LabelID: optionalUUID(labelID), Source: sourceCorrection,
		})
		if err != nil {
			return err
		}
		out = LearnedRule{Rule: ruleFromRow(row), Created: !onFile}
		return nil
	})
	if err != nil {
		return LearnedRule{}, fmt.Errorf("learn pim watch rule: %w", err)
	}
	return out, nil
}
