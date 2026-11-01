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

// The cursor's status.
const (
	CursorOK          = "ok"
	CursorFailing     = "failing"
	CursorUnsupported = "unsupported"
)

// CodePermissionDenied is the failure code of an account that refuses a resource.
const CodePermissionDenied = "permission_denied"

// The two alarms a cursor can owe: a gap the PIM reported, and a failing episode.
const (
	AlarmGap     = "gap"
	AlarmFailing = "failing"
)

// Cursor is the watermark of one resource of one account, with what the run needs to tell the
// person when it broke. An alarm is owed while AlarmRunID is set: the run that told it has not
// been seen to complete.
type Cursor struct {
	AccountID    string
	Resource     string
	Value        string
	Status       string
	LastError    string
	FailingSince time.Time
	AlarmKind    string
	AlarmSince   time.Time
	AlarmRunID   string
	AdvancedAt   time.Time
}

func cursorFromRow(r sqlc.AuraPimWatchCursors) Cursor {
	return Cursor{
		AccountID: r.AccountID, Resource: r.Resource, Value: r.Cursor, Status: r.Status,
		LastError: r.LastError.String, FailingSince: r.FailingSince.Time,
		AlarmKind: r.AlarmKind.String, AlarmSince: r.AlarmSince.Time,
		AlarmRunID: r.AlarmRunID.String(), AdvancedAt: r.AdvancedAt.Time,
	}
}

// Cursor reads one cursor. A missing row is ErrNotFound.
func (s *Store) Cursor(ctx context.Context, identityID, accountID, resource string) (Cursor, error) {
	var out Cursor
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.GetPIMWatchCursor(ctx, sqlc.GetPIMWatchCursorParams{IdentityID: id, AccountID: accountID, Resource: resource})
		if err != nil {
			return err
		}
		out = cursorFromRow(row)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Cursor{}, fmt.Errorf("pim watch cursor: %w", ErrNotFound)
	}
	if err != nil {
		return Cursor{}, fmt.Errorf("read pim watch cursor: %w", err)
	}
	return out, nil
}

// Cursors lists every cursor of the identity.
func (s *Store) Cursors(ctx context.Context, identityID string) ([]Cursor, error) {
	cursors, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchCursors, error) {
		return q.ListPIMWatchCursors(ctx, id)
	}, cursorFromRow)
	if err != nil {
		return nil, fmt.Errorf("list pim watch cursors: %w", err)
	}
	return cursors, nil
}

// StartCursor sets a resource's cursor from the empty-cursor call, or records that the provider
// lacks it. Nothing from a past watch of the account survives.
func (s *Store) StartCursor(ctx context.Context, identityID, accountID, resource, value, status string) error {
	return s.startCursor(ctx, identityID, accountID, resource, value, status, "")
}

// RefuseCursor records that the account refuses the permission for a resource. It reads like a
// resource the provider lacks, but keeps the code so the person is told to reconnect the account.
func (s *Store) RefuseCursor(ctx context.Context, identityID, accountID, resource string) error {
	return s.startCursor(ctx, identityID, accountID, resource, "", CursorUnsupported, CodePermissionDenied)
}

func (s *Store) startCursor(ctx context.Context, identityID, accountID, resource, value, status, code string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.StartPIMWatchCursor(ctx, sqlc.StartPIMWatchCursorParams{
			IdentityID: id, AccountID: accountID, Resource: resource, Cursor: value, Status: status,
			LastError: optionalText(code),
		})
	})
	if err != nil {
		return fmt.Errorf("start pim watch cursor: %w", err)
	}
	return nil
}

// MarkFailing records a failed read on the cursor row and returns it. The episode's start stays
// put until a success clears it. code is a fixed code, never the PIM's text, and at is the caller's
// clock, because the alarm's hour is measured against that same clock.
func (s *Store) MarkFailing(ctx context.Context, identityID, accountID, resource, code string, at time.Time) (Cursor, error) {
	var out Cursor
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.MarkPIMWatchFailing(ctx, sqlc.MarkPIMWatchFailingParams{
			IdentityID: id, AccountID: accountID, Resource: resource, LastError: optionalText(code), At: timestamp(at),
		})
		if err != nil {
			return err
		}
		out = cursorFromRow(row)
		return nil
	})
	if err != nil {
		return Cursor{}, fmt.Errorf("mark pim watch failing: %w", err)
	}
	return out, nil
}

// MarkUnsupported records that the provider lacks this resource, so later runs skip it.
func (s *Store) MarkUnsupported(ctx context.Context, identityID, accountID, resource string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.MarkPIMWatchUnsupported(ctx, sqlc.MarkPIMWatchUnsupportedParams{IdentityID: id, AccountID: accountID, Resource: resource})
	})
	if err != nil {
		return fmt.Errorf("mark pim watch unsupported: %w", err)
	}
	return nil
}

// RaiseFailingAlarm records that runID tells the person of a failing episode. An empty runID
// means the failure itself reports it, so nothing stays owed.
func (s *Store) RaiseFailingAlarm(ctx context.Context, identityID, accountID, resource, runID string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.RaisePIMWatchFailingAlarm(ctx, sqlc.RaisePIMWatchFailingAlarmParams{
			IdentityID: id, AccountID: accountID, Resource: resource, AlarmRunID: optionalUUID(runID),
		})
	})
	if err != nil {
		return fmt.Errorf("raise pim watch alarm: %w", err)
	}
	return nil
}

// RetellAlarm moves an owed alarm to runID.
func (s *Store) RetellAlarm(ctx context.Context, identityID, accountID, resource, runID string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.RetellPIMWatchAlarm(ctx, sqlc.RetellPIMWatchAlarmParams{
			IdentityID: id, AccountID: accountID, Resource: resource, AlarmRunID: optionalUUID(runID),
		})
	})
	if err != nil {
		return fmt.Errorf("retell pim watch alarm: %w", err)
	}
	return nil
}

// ConfirmAlarm records that the alarm went out. A gap's alarm ends here, and a failing one stays
// until a success, which is what makes it once per episode.
func (s *Store) ConfirmAlarm(ctx context.Context, identityID, accountID, resource string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.ConfirmPIMWatchAlarm(ctx, sqlc.ConfirmPIMWatchAlarmParams{IdentityID: id, AccountID: accountID, Resource: resource})
	})
	if err != nil {
		return fmt.Errorf("confirm pim watch alarm: %w", err)
	}
	return nil
}

// DeleteCursors forgets an account's cursors, which is what disabling its watch does.
func (s *Store) DeleteCursors(ctx context.Context, identityID, accountID string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.DeletePIMWatchCursors(ctx, sqlc.DeletePIMWatchCursorsParams{IdentityID: id, AccountID: accountID})
	})
	if err != nil {
		return fmt.Errorf("delete pim watch cursors: %w", err)
	}
	return nil
}

// DeleteAccount removes everything the watch holds for an account, in one transaction.
func (s *Store) DeleteAccount(ctx context.Context, identityID, accountID string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if err := q.DeletePIMWatchCursors(ctx, sqlc.DeletePIMWatchCursorsParams{IdentityID: id, AccountID: accountID}); err != nil {
			return err
		}
		if err := q.DeletePIMWatchVerdicts(ctx, sqlc.DeletePIMWatchVerdictsParams{IdentityID: id, AccountID: accountID}); err != nil {
			return err
		}
		return q.DeletePIMWatchCorrections(ctx, sqlc.DeletePIMWatchCorrectionsParams{IdentityID: id, AccountID: accountID})
	})
	if err != nil {
		return fmt.Errorf("delete pim watch account: %w", err)
	}
	return nil
}
