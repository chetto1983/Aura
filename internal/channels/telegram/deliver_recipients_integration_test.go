//go:build db_integration

package telegram

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/cron"
)

type reminderHandler struct{}

func (reminderHandler) Meta() cron.HandlerMeta {
	return cron.HandlerMeta{Kind: cron.KindReminder, MaxDuration: time.Second}
}

func (reminderHandler) Run(context.Context, cron.Job) (string, error) { return "promemoria", nil }

type discardRuns struct{}

func (discardRuns) CompleteRun(context.Context, cron.CompleteRunParams) error { return nil }

type silentRoute struct{}

func (silentRoute) Notify(context.Context, cron.NotifyRoute, string) error { return nil }
func (silentRoute) Destination(context.Context, cron.NotifyRoute) (string, error) {
	return "", nil
}

type originRecord struct{ texts []string }

func (r *originRecord) AppendAssistantTurn(_ context.Context, _, text string) error {
	r.texts = append(r.texts, text)
	return nil
}

// A task that names both linked accounts reaches both chats through the real wiring: the links
// in Postgres, the channel, the registry and the cron dispatcher; after one account is unlinked
// the push reaches the other and the origin conversation names the one it skipped (spec
// 2026-10-28-telegram-push-recipients, acceptance 6). The approval leg starts at the registry:
// the sweep's loop over the recipients is a cron unit test.
func TestTaskPushReachesBothLinkedChats(t *testing.T) {
	s := New(migratedPool(t))
	ctx := ownerCtx()
	identityID := userIdentity(t, s.pool, "both-phones")
	const first, second int64 = 9_000_145_101, 9_000_145_102
	linkThroughOnboarding(t, s, identityID, first)
	linkThroughOnboarding(t, s, identityID, second)

	bot := &sendRecorder{}
	tg := NewChannel(Deps{Offline: true})
	tg.deliverBot, tg.deliverResolver = bot, s
	reg := channels.NewRegistry()
	reg.Register(tg)
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	t.Cleanup(func() { _ = reg.StopAll(context.Background()) })

	record := &originRecord{}
	d := cron.NewDispatch(map[cron.TaskKind]cron.Handler{cron.KindReminder: reminderHandler{}}, cron.DispatchDeps{
		Store: discardRuns{}, Notifier: silentRoute{}, ChannelDeliverer: reg, ConversationRecorder: record,
	})
	task := cron.Task{
		ID: uuid.NewString(), Kind: cron.KindReminder, Payload: []byte(`{"text":"promemoria"}`),
		NotifyRoute: string(cron.RouteTelegram), IdentityID: identityID,
		OriginConversationID: uuid.NewString(), TelegramRecipients: []int64{first, second},
	}
	reached := func() []string {
		t.Helper()
		var chats []string
		for _, send := range bot.recorded() {
			chats = append(chats, send.to.Recipient())
		}
		bot.mu.Lock()
		bot.sends = nil
		bot.mu.Unlock()
		return chats
	}
	both := []string{fmt.Sprint(first), fmt.Sprint(second)}

	if err := d.Dispatch(ctx, task, &cron.Claim{RunID: uuid.NewString()}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got := reached(); !slices.Equal(got, both) {
		t.Fatalf("the task's push reached %v, want %v", got, both)
	}
	for _, account := range task.TelegramRecipients {
		if ok, err := reg.DeliverApprovalToTelegramAccount(ctx, identityID, account, "token", task.ID, string(task.Kind)); err != nil || !ok {
			t.Fatalf("approval to %d = (%v, %v), want delivered", account, ok, err)
		}
	}
	if got := reached(); !slices.Equal(got, both) {
		t.Fatalf("the approval prompt reached %v, want %v", got, both)
	}

	if unlinked, err := s.Unlink(ctx, identityID, second); err != nil || !unlinked {
		t.Fatalf("unlink %d = (%v, %v)", second, unlinked, err)
	}
	if err := d.Dispatch(ctx, task, &cron.Claim{RunID: uuid.NewString()}); err != nil {
		t.Fatalf("Dispatch after unlink: %v", err)
	}
	if got := reached(); !slices.Equal(got, both[:1]) {
		t.Fatalf("after the unlink the push reached %v, want only %d", got, first)
	}
	if last := record.texts[len(record.texts)-1]; !strings.Contains(last, fmt.Sprintf("telegram account %d is no longer linked", second)) {
		t.Fatalf("the origin conversation read %q, want it to name the skipped account", last)
	}
}
