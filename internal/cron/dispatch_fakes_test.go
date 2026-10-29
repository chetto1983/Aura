package cron

import "context"

type fakeNotificationStore struct {
	calls []CompleteRunParams

	inserted []InsertPendingNotificationParams

	sweepRows         []PendingNotification
	sweepAttemptBound int
	sweepLimit        int

	delivered     []string
	failed        []struct{ id, err string }
	undeliverable []struct{ id, err string }
}

func (s *fakeNotificationStore) CompleteRun(_ context.Context, p CompleteRunParams) error {
	s.calls = append(s.calls, p)
	return nil
}

func (s *fakeNotificationStore) InsertPendingNotification(_ context.Context, p InsertPendingNotificationParams) (PendingNotification, error) {
	s.inserted = append(s.inserted, p)
	return PendingNotification{ID: "pending-id", RunID: p.RunID, NotifyRoute: p.NotifyRoute, Body: p.Body, NotifyAfter: p.NotifyAfter, Attempts: p.Attempts, LastError: p.LastError, Status: p.Status}, nil
}

func (s *fakeNotificationStore) SweepDueNotifications(_ context.Context, attemptBound, limit int) ([]PendingNotification, error) {
	s.sweepAttemptBound = attemptBound
	s.sweepLimit = limit
	return s.sweepRows, nil
}

func (s *fakeNotificationStore) MarkNotificationDelivered(_ context.Context, id string) error {
	s.delivered = append(s.delivered, id)
	return nil
}

func (s *fakeNotificationStore) MarkNotificationFailed(_ context.Context, id, lastErr string) error {
	s.failed = append(s.failed, struct{ id, err string }{id: id, err: lastErr})
	return nil
}

func (s *fakeNotificationStore) MarkNotificationUndeliverable(_ context.Context, id, lastErr string) error {
	s.undeliverable = append(s.undeliverable, struct{ id, err string }{id: id, err: lastErr})
	return nil
}

// noTelegramAccounts gives a delivery fake the account pushes it never makes: no channel can
// push to a named Telegram account, so the caller falls back as with no channel.
type noTelegramAccounts struct{}

func (noTelegramAccounts) DeliverToTelegramAccount(context.Context, string, int64, string) (bool, error) {
	return false, nil
}

func (noTelegramAccounts) DeliverApprovalToTelegramAccount(context.Context, string, int64, string, string, string) (bool, error) {
	return false, nil
}
