package cron

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type permanentErr struct{ msg string }

func (e permanentErr) Error() string { return e.msg }
func (permanentErr) Permanent() bool { return true }

// notPermanentErr answers the question and says no.
type notPermanentErr struct{}

func (notPermanentErr) Error() string   { return "rate limited" }
func (notPermanentErr) Permanent() bool { return false }

// errUnowned scripts an account push no channel can make: (false, nil).
var errUnowned = errors.New("no channel")

// accountChannel scripts a push per Telegram account; an account it has no result for is
// delivered. It records every push, identity-wide ones included, in order.
type accountChannel struct {
	results map[int64]error
	calls   []string
}

func (c *accountChannel) push(call string, account int64) (bool, error) {
	c.calls = append(c.calls, fmt.Sprintf("%s %d", call, account))
	switch err := c.results[account]; {
	case errors.Is(err, errUnowned):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

func (c *accountChannel) DeliverToTelegramAccount(_ context.Context, _ string, account int64, _ string) (bool, error) {
	return c.push("text", account)
}

func (c *accountChannel) DeliverApprovalToTelegramAccount(_ context.Context, _ string, account int64, _, _, _ string) (bool, error) {
	return c.push("approval", account)
}

func (c *accountChannel) DeliverToIdentity(context.Context, string, string) (bool, error) {
	return c.push("identity", 0)
}

func (c *accountChannel) DeliverToConversation(context.Context, string, string, string) (bool, error) {
	return c.push("conversation", 0)
}

func (c *accountChannel) DeliverApproval(context.Context, string, string, string, string) (bool, error) {
	return c.push("identity approval", 0)
}

type recipientsRun struct {
	store    *fakeNotificationStore
	notifier *captureNotifier
	recorder *fakeConversationRecorder
}

func dispatchToRecipients(t *testing.T, channel *accountChannel, notifier *captureNotifier, kind TaskKind, deps DispatchDeps, recipients ...int64) recipientsRun {
	t.Helper()
	run := recipientsRun{store: &fakeNotificationStore{}, notifier: notifier, recorder: &fakeConversationRecorder{}}
	deps.Store, deps.Notifier, deps.ChannelDeliverer, deps.ConversationRecorder = run.store, notifier, channel, run.recorder
	payload := []byte(`{"text":"drink water"}`)
	if kind == KindAgentJob {
		payload = []byte(`{"goal":"summarize the news"}`)
	}
	d, c := newDispatchFor(t, &fakeHandler{meta: HandlerMeta{Kind: kind}, summary: "drink water"}, kind, deps)
	task := Task{
		ID: "t-recipients", Kind: kind, Payload: payload, NotifyRoute: string(RouteTelegram),
		IdentityID: "id-R", OriginConversationID: "conv-R", TelegramRecipients: recipients,
	}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return run
}

// queued names the account and status of every row the push queued, in order.
func (r recipientsRun) queued() []string {
	var rows []string
	for _, p := range r.store.inserted {
		rows = append(rows, fmt.Sprintf("%s %d", p.Status, p.TelegramUserID))
	}
	return rows
}

func (r recipientsRun) record() string {
	if len(r.recorder.appended) != 1 {
		return fmt.Sprintf("%d records", len(r.recorder.appended))
	}
	return r.recorder.appended[0].text
}

// Spec 2026-10-28-telegram-push-recipients, acceptance 4: each recipient is sent to on its own,
// and only the one a retry can help is queued again.
func TestTaskPushReachesEachTelegramRecipient(t *testing.T) {
	t.Parallel()

	t.Run("each recipient once; a failure queues a retry for that account only", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{results: map[int64]error{2: errors.New("telegram: network down")}}
		run := dispatchToRecipients(t, channel, &captureNotifier{}, KindReminder, DispatchDeps{}, 1, 2, 1)
		if want := []string{"text 1", "text 2"}; !slices.Equal(channel.calls, want) {
			t.Fatalf("pushes = %v, want %v", channel.calls, want)
		}
		if want := []string{"failed 2"}; !slices.Equal(run.queued(), want) {
			t.Fatalf("queued = %v, want %v", run.queued(), want)
		}
		if len(run.notifier.texts) != 0 {
			t.Fatalf("the route notifier was asked %v, want nothing", run.notifier.texts)
		}
		if got := run.record(); !strings.Contains(got, "Not delivered via telegram: telegram: network down") {
			t.Fatalf("record = %q, want it to name the failure", got)
		}
	})

	t.Run("a permanent failure and an unlinked account queue nothing and the record names both", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{results: map[int64]error{
			2: permanentErr{"telegram account 2 is no longer linked"},
			3: permanentErr{"send to 3: bot was blocked by the user"},
		}}
		run := dispatchToRecipients(t, channel, &captureNotifier{}, KindReminder, DispatchDeps{}, 1, 2, 3)
		if want := []string{"text 1", "text 2", "text 3"}; !slices.Equal(channel.calls, want) {
			t.Fatalf("pushes = %v, want %v", channel.calls, want)
		}
		if len(run.store.inserted) != 0 {
			t.Fatalf("queued %v, want nothing: no retry can deliver these", run.queued())
		}
		for _, want := range []string{"telegram account 2 is no longer linked", "send to 3: bot was blocked by the user"} {
			if !strings.Contains(run.record(), want) {
				t.Fatalf("record = %q, want it to name %q", run.record(), want)
			}
		}
	})

	t.Run("a push deferred to the end of quiet hours is queued once per recipient", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{}
		run := dispatchToRecipients(t, channel, &captureNotifier{}, KindAgentJob, DispatchDeps{QuietHours: func(string) bool { return true }}, 1, 2, 2)
		if len(channel.calls) != 0 {
			t.Fatalf("pushes = %v, want none before quiet hours end", channel.calls)
		}
		if want := []string{"pending 1", "pending 2"}; !slices.Equal(run.queued(), want) {
			t.Fatalf("queued = %v, want %v", run.queued(), want)
		}
	})

	t.Run("with no channel to push it the route chain queues one retry per recipient", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{results: map[int64]error{1: errUnowned, 2: errUnowned}}
		run := dispatchToRecipients(t, channel, &captureNotifier{err: errors.New("telegram is a channel route")}, KindReminder, DispatchDeps{}, 1, 2)
		if len(run.notifier.texts) != 1 {
			t.Fatalf("the route notifier was asked %v, want once", run.notifier.texts)
		}
		if want := []string{"failed 1", "failed 2"}; !slices.Equal(run.queued(), want) {
			t.Fatalf("queued = %v, want %v", run.queued(), want)
		}
	})

	t.Run("a channel that stops part way retries only the accounts it did not reach", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{results: map[int64]error{2: errUnowned}}
		run := dispatchToRecipients(t, channel, &captureNotifier{}, KindReminder, DispatchDeps{}, 1, 2)
		if want := []string{"failed 2"}; !slices.Equal(run.queued(), want) {
			t.Fatalf("queued = %v, want %v", run.queued(), want)
		}
		if len(run.notifier.texts) != 0 {
			t.Fatalf("the route notifier was asked %v, want nothing", run.notifier.texts)
		}
		if !strings.Contains(run.record(), "telegram account 2: no channel could push") {
			t.Fatalf("record = %q, want it to name account 2", run.record())
		}
	})

	t.Run("a task with no recipients queues its retry for the identity, naming no account", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{results: map[int64]error{0: errors.New("telegram: network down")}}
		run := dispatchToRecipients(t, channel, &captureNotifier{}, KindReminder, DispatchDeps{})
		if want := []string{"failed 0"}; !slices.Equal(run.queued(), want) {
			t.Fatalf("queued = %v, want %v", run.queued(), want)
		}
	})

	t.Run("a task with no recipients keeps the identity push, and a permanent failure queues nothing", func(t *testing.T) {
		t.Parallel()
		channel := &accountChannel{results: map[int64]error{0: permanentErr{"bot was blocked by the user"}}}
		run := dispatchToRecipients(t, channel, &captureNotifier{}, KindReminder, DispatchDeps{})
		if want := []string{"identity 0"}; !slices.Equal(channel.calls, want) {
			t.Fatalf("pushes = %v, want %v", channel.calls, want)
		}
		if len(run.store.inserted) != 0 {
			t.Fatalf("queued %v, want nothing", run.queued())
		}
	})
}

// A swept Telegram row that names an account goes to that account only, and a row no retry can
// deliver leaves the sweep (spec R4, R5).
func TestSweptTelegramRowReachesItsAccount(t *testing.T) {
	t.Parallel()
	channel := &accountChannel{results: map[int64]error{
		2: errors.New("telegram: network down"),
		3: permanentErr{"telegram account 3 is no longer linked"},
	}}
	row := func(id string, account int64) PendingNotification {
		return PendingNotification{ID: id, NotifyRoute: string(RouteTelegram), Body: "drink water", IdentityID: "id-R", TelegramUserID: account}
	}
	store := &fakeNotificationStore{sweepRows: []PendingNotification{row("n1", 1), row("n2", 2), row("n3", 3), row("n0", 0)}}
	notifier := &captureNotifier{}
	d := NewDispatch(nil, DispatchDeps{Store: store, Notifier: notifier, ChannelDeliverer: channel})
	if err := d.sweepNotifications(context.Background()); err != nil {
		t.Fatalf("sweepNotifications: %v", err)
	}
	if want := []string{"text 1", "text 2", "text 3", "identity 0"}; !slices.Equal(channel.calls, want) {
		t.Fatalf("pushes = %v, want %v", channel.calls, want)
	}
	if want := []string{"n1", "n0"}; !slices.Equal(store.delivered, want) {
		t.Fatalf("delivered = %v, want %v", store.delivered, want)
	}
	if len(store.failed) != 1 || store.failed[0].id != "n2" || store.failed[0].err != "telegram: network down" {
		t.Fatalf("failed = %+v, want n2 with the channel's error", store.failed)
	}
	if len(store.undeliverable) != 1 || store.undeliverable[0].id != "n3" || store.undeliverable[0].err != "telegram account 3 is no longer linked" {
		t.Fatalf("undeliverable = %+v, want n3 with the channel's error", store.undeliverable)
	}
	if len(notifier.texts) != 0 {
		t.Fatalf("the route notifier was asked %v, want nothing", notifier.texts)
	}
}

// The approval prompt the sweep mints goes to each recipient once; a failed one is not retried
// and the reminder is stamped (spec §4).
func TestApprovalPromptReachesEachTelegramRecipient(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	t.Setenv("AURA_SCHEDULER_APPROVAL_GRACE_SEC", "0")
	const taskID = "aaaaaaaa-1111-1111-1111-111111111111"
	for name, c := range map[string]struct {
		route string
		want  []string
	}{
		"telegram":      {string(RouteTelegram), []string{"approval 1", "approval 2"}},
		"another route": {string(RouteStdout), []string{"identity approval 0"}},
	} {
		channel := &accountChannel{results: map[int64]error{1: errors.New("telegram: network down")}}
		store := &fakeApprovalReminderStore{tasks: []Task{{
			ID: taskID, Kind: KindAgentJob, IdentityID: "id-R", OriginConversationID: "conv-R",
			NotifyRoute: c.route, TelegramRecipients: []int64{1, 2, 1},
		}}}
		d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ApprovalPauseEnsurer: &fakeEnsurer{}, ApprovalChannel: channel})
		if err := d.sweepApprovalReminders(context.Background()); err != nil {
			t.Fatalf("%s: sweepApprovalReminders: %v", name, err)
		}
		if !slices.Equal(channel.calls, c.want) {
			t.Fatalf("%s: pushes = %v, want %v", name, channel.calls, c.want)
		}
		if !slices.Equal(store.reminded, []string{taskID}) {
			t.Fatalf("%s: stamped %v, want the task", name, store.reminded)
		}
	}
}

func TestTelegramRecipientsAreTheTelegramRoutesOnlyEachOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		route string
		named []int64
		want  []int64
	}{
		{string(RouteTelegram), []int64{3, 1, 3, 2, 1}, []int64{3, 1, 2}},
		{string(RouteTelegram), nil, nil},
		{string(RouteStdout), []int64{1, 2}, nil},
	} {
		if got := telegramRecipients(Task{NotifyRoute: c.route, TelegramRecipients: c.named}); !slices.Equal(got, c.want) {
			t.Fatalf("telegramRecipients(%s, %v) = %v, want %v", c.route, c.named, got, c.want)
		}
	}
	if permanent(nil) || permanent(errors.New("down")) || permanent(notPermanentErr{}) || !permanent(fmt.Errorf("wrapped: %w", permanentErr{"gone"})) {
		t.Fatal("permanent must answer only an error that says so, through any wrapping")
	}
}
