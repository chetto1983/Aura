package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// fakeDefaults is the operator's default channel by identity, the Profile panel's setting.
type fakeDefaults map[string]string

func (f fakeDefaults) DefaultNotify(_ context.Context, identityID string) string {
	return f[identityID]
}

const defaultsIdentity = "identity-op"

const reminderWithoutNotify = `{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"drink water"}`

func scheduleWithDefault(t *testing.T, tool *TaskTool, args string) (*fakeTaskStore, ToolResult, error) {
	t.Helper()
	store := &fakeTaskStore{}
	tool.Store = store
	ctx := identityctx.WithIdentityID(context.Background(), defaultsIdentity)
	res, err := tool.Execute(ctx, json.RawMessage(args))
	return store, res, err
}

func directiveStatus(t *testing.T, res ToolResult) (string, string) {
	t.Helper()
	var got struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(res.Preview), &got); err != nil {
		t.Fatalf("the result is not a directive: %v (%s)", err, res.Preview)
	}
	return got.Status, got.Message
}

// Spec 2026-11-05 Part 1: a request that names no channel goes to the operator's default, and
// the reply says that is where it goes; an explicit channel always wins.
func TestAnEmptyNotifyUsesTheOperatorsDefault(t *testing.T) {
	t.Parallel()
	op := TelegramAccount{TelegramUserID: 1, Username: "op"}
	cases := map[string]struct {
		def       string
		args      string
		wantRoute string
		wantNote  string
	}{
		"telegram to the one linked account": {"telegram", reminderWithoutNotify + `}`, "telegram",
			"Delivers via telegram to @op (1) (your default channel)."},
		"stdout":                  {"stdout", reminderWithoutNotify + `}`, "stdout", "Delivers via stdout (your default channel)."},
		"an explicit notify wins": {"telegram", reminderWithoutNotify + `,"notify":"none"}`, "none", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool := &TaskTool{
				Defaults:         fakeDefaults{defaultsIdentity: tc.def},
				Destinations:     &fakeDestinations{},
				TelegramAccounts: &fakeTelegramAccounts{linked: []TelegramAccount{op}},
			}
			store, res, err := scheduleWithDefault(t, tool, tc.args)
			if err != nil {
				t.Fatalf("schedule: %v", err)
			}
			if store.created.NotifyRoute != tc.wantRoute {
				t.Fatalf("NotifyRoute = %q, want %q", store.created.NotifyRoute, tc.wantRoute)
			}
			if tc.wantNote != "" && !strings.Contains(res.Preview, tc.wantNote) {
				t.Errorf("preview %q lacks %q", res.Preview, tc.wantNote)
			}
			if tc.wantNote == "" && strings.Contains(res.Preview, "(your default channel)") {
				t.Errorf("an explicit notify was credited to the default: %q", res.Preview)
			}
		})
	}
}

func TestNoDefaultStillAsksAndPointsToTheProfilePanel(t *testing.T) {
	t.Parallel()
	store, res, err := scheduleWithDefault(t, &TaskTool{Defaults: fakeDefaults{}}, reminderWithoutNotify+`}`)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	status, message := directiveStatus(t, res)
	if status != "delivery_choice_required" || !strings.Contains(message, "Profile panel") {
		t.Fatalf("directive = %q / %q, want the choice and the Profile panel", status, message)
	}
	if store.created.Kind != "" {
		t.Fatalf("persisted %+v before the operator chose", store.created)
	}
}

func TestADefaultTelegramWithTwoAccountsStillAsksWhichAccount(t *testing.T) {
	t.Parallel()
	tool := &TaskTool{
		Defaults:     fakeDefaults{defaultsIdentity: "telegram"},
		Destinations: &fakeDestinations{},
		TelegramAccounts: &fakeTelegramAccounts{linked: []TelegramAccount{
			{TelegramUserID: 1, Username: "op"}, {TelegramUserID: 2, FirstName: "Work"},
		}},
	}
	store, res, err := scheduleWithDefault(t, tool, reminderWithoutNotify+`}`)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if status, _ := directiveStatus(t, res); status != "telegram_recipients_required" {
		t.Fatalf("status = %q, want telegram_recipients_required", status)
	}
	if store.created.Kind != "" {
		t.Fatalf("persisted %+v before the operator chose", store.created)
	}
}

func TestADefaultThatCannotDeliverIsRefusedLikeAnExplicitOne(t *testing.T) {
	t.Parallel()
	tool := &TaskTool{
		Defaults:     fakeDefaults{defaultsIdentity: "whatsapp"},
		Destinations: &fakeDestinations{err: errors.New("no WhatsApp account is linked")},
	}
	store, _, err := scheduleWithDefault(t, tool, reminderWithoutNotify+`}`)
	if err == nil || !strings.Contains(err.Error(), "notify=whatsapp cannot deliver") {
		t.Fatalf("err = %v, want the cannot-deliver refusal", err)
	}
	if store.created.Kind != "" {
		t.Fatalf("persisted %+v on a default that cannot deliver", store.created)
	}
}

// A value the database CHECK would refuse can still reach the seam from a fake or a future
// column: it is "not set", never a route.
func TestAnInvalidStoredDefaultIsIgnored(t *testing.T) {
	t.Parallel()
	store, res, err := scheduleWithDefault(t, &TaskTool{Defaults: fakeDefaults{defaultsIdentity: "sms"}}, reminderWithoutNotify+`}`)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if status, _ := directiveStatus(t, res); status != "delivery_choice_required" {
		t.Fatalf("status = %q, want delivery_choice_required", status)
	}
	if store.created.Kind != "" {
		t.Fatalf("persisted %+v on an invalid default", store.created)
	}
}

func TestAGatedJobOnTheDefaultNamesItInTheApprovalDirective(t *testing.T) {
	t.Parallel()
	tool := &TaskTool{Defaults: fakeDefaults{defaultsIdentity: "stdout"}, Destinations: &fakeDestinations{}}
	_, res, err := scheduleWithDefault(t, tool,
		`{"action":"schedule","schedule_kind":"every","every_minutes":60,"kind":"agent_job","payload":{"goal":"check the inbox"}}`)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if !strings.Contains(res.Preview, "status=pending_approval") || !strings.HasSuffix(res.Preview, "\nDelivers via stdout (your default channel).") {
		t.Fatalf("preview = %q, want pending_approval naming the default", res.Preview)
	}
}

func TestTheToolTextNoLongerSaysAskFirst(t *testing.T) {
	t.Parallel()
	description := (&TaskTool{}).Spec().Description
	for _, gone := range []string{"call ask_user(kind=choice) before scheduling", "where (the channel)"} {
		if strings.Contains(description, gone) {
			t.Errorf("description still holds %q", gone)
		}
	}
	for _, want := range []string{"If the request names no channel, omit notify", "else the operator's default channel"} {
		if !strings.Contains(description, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}
