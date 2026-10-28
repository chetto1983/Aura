package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type fakeTelegramAccounts struct {
	linked []TelegramAccount
	err    error
	asked  int
}

func (f *fakeTelegramAccounts) LinkedTelegramAccounts(context.Context) ([]TelegramAccount, error) {
	f.asked++
	return f.linked, f.err
}

func scheduleTelegram(t *testing.T, accounts *fakeTelegramAccounts, extra string) (*fakeTaskStore, ToolResult, error) {
	t.Helper()
	store := &fakeTaskStore{}
	tool := &TaskTool{Store: store, Destinations: &fakeDestinations{}, TelegramAccounts: accounts}
	args := `{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"drink water"},"notify":"telegram"` + extra + `}`
	res, err := tool.Execute(context.Background(), json.RawMessage(args))
	return store, res, err
}

// Spec 2026-10-28-telegram-push-recipients, R2 and acceptance 5: a Telegram task names the
// accounts it reaches, and the operator chooses when the identity links more than one.
func TestTaskScheduleNamesItsTelegramRecipients(t *testing.T) {
	phone := TelegramAccount{TelegramUserID: 4242, Username: "davide", FirstName: "Davide"}
	work := TelegramAccount{TelegramUserID: 4343, FirstName: "Lavoro"}
	two := func() *fakeTelegramAccounts { return &fakeTelegramAccounts{linked: []TelegramAccount{phone, work}} }

	t.Run("two links and none named asks, listing both and all, and persists nothing", func(t *testing.T) {
		store, res, err := scheduleTelegram(t, two(), "")
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		var directive struct {
			Status  string              `json:"status"`
			Options []map[string]string `json:"options"`
			Message string              `json:"message"`
		}
		if err := json.Unmarshal([]byte(res.Preview), &directive); err != nil {
			t.Fatalf("the directive is not JSON: %v (%s)", err, res.Preview)
		}
		var values, labels []string
		for _, o := range directive.Options {
			values, labels = append(values, o["value"]), append(labels, o["label"])
		}
		if directive.Status != "telegram_recipients_required" || !slices.Equal(values, []string{"4242", "4343", "4242,4343"}) {
			t.Fatalf("directive = %+v, want a choice of 4242, 4343 and all", directive)
		}
		if !strings.Contains(labels[0], "@davide") || !strings.Contains(labels[1], "Lavoro") || !strings.Contains(directive.Message, "telegram_recipients") {
			t.Fatalf("directive labels %v / message %q do not name the accounts and the field", labels, directive.Message)
		}
		if store.created.Kind != "" {
			t.Fatalf("persisted %+v, want nothing before the operator chooses", store.created)
		}
	})

	t.Run("named recipients are persisted once each, in the order given, and named back", func(t *testing.T) {
		store, res, err := scheduleTelegram(t, two(), `,"telegram_recipients":[4343,4343,4242,4343]`)
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if !slices.Equal(store.created.TelegramRecipients, []int64{4343, 4242}) {
			t.Fatalf("persisted recipients %v, want [4343 4242]", store.created.TelegramRecipients)
		}
		if !strings.Contains(res.Preview, "Delivers via telegram to Lavoro (4343), @davide (4242).") {
			t.Fatalf("preview %q does not name the recipients", res.Preview)
		}
	})

	t.Run("one link and none named stores that account", func(t *testing.T) {
		store, _, err := scheduleTelegram(t, &fakeTelegramAccounts{linked: []TelegramAccount{phone}}, "")
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if !slices.Equal(store.created.TelegramRecipients, []int64{4242}) {
			t.Fatalf("persisted recipients %v, want [4242]", store.created.TelegramRecipients)
		}
	})

	for name, c := range map[string]struct {
		accounts *fakeTelegramAccounts
		extra    string
		want     []string
	}{
		"an unlinked id is refused":    {two(), `,"telegram_recipients":[4242,9999]`, []string{"9999 is not linked", "No task was created"}},
		"no link is refused":           {&fakeTelegramAccounts{}, "", []string{"notify=telegram cannot deliver", "no Telegram account is linked", "No task was created"}},
		"a failing lookup is an error": {&fakeTelegramAccounts{err: errors.New("connection refused")}, "", []string{"connection refused"}},
	} {
		t.Run(name, func(t *testing.T) {
			store, _, err := scheduleTelegram(t, c.accounts, c.extra)
			if err == nil {
				t.Fatal("schedule succeeded, want a refusal")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if store.created.Kind != "" {
				t.Fatalf("a refused schedule persisted %+v", store.created)
			}
		})
	}

	t.Run("recipients belong to notify=telegram only", func(t *testing.T) {
		accounts := two()
		store := &fakeTaskStore{}
		tool := &TaskTool{Store: store, Destinations: &fakeDestinations{}, TelegramAccounts: accounts}
		args := `{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"x"},"notify":"stdout","telegram_recipients":[4242]}`
		if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "only with notify=telegram") {
			t.Fatalf("err = %v, want a refusal naming notify=telegram", err)
		}
		if store.created.Kind != "" || accounts.asked != 0 {
			t.Fatalf("persisted %+v after %d lookups, want nothing", store.created, accounts.asked)
		}
	})

	t.Run("another route never asks for the accounts and names none", func(t *testing.T) {
		accounts := two()
		store := &fakeTaskStore{}
		tool := &TaskTool{Store: store, Destinations: &fakeDestinations{}, TelegramAccounts: accounts}
		args := `{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"x"},"notify":"stdout"}`
		if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err != nil || accounts.asked != 0 {
			t.Fatalf("stdout schedule = %v after %d lookups, want success and none", err, accounts.asked)
		}
		// nil, never an empty list: migration 0145's CHECK refuses an empty one.
		if store.created.TelegramRecipients != nil {
			t.Fatalf("persisted recipients %#v, want nil", store.created.TelegramRecipients)
		}
	})
}

func TestTelegramAccountNameFallsBackToTheID(t *testing.T) {
	if got := (TelegramAccount{TelegramUserID: 9_000_145_301}).name(); got != "9000145301" {
		t.Fatalf("an account with no name is called %q, want its id", got)
	}
}

func TestTaskListNamesTelegramRecipients(t *testing.T) {
	got := renderTaskList([]ScheduledTask{{ID: "t1", Kind: "reminder", ScheduleKind: "at", Status: "active", NotifyRoute: "telegram", TelegramRecipients: []int64{4242, 4343}}})
	if !strings.Contains(got, "notify=telegram  telegram_recipients=4242,4343") {
		t.Fatalf("list %q does not name the recipients", got)
	}
}
