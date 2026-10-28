package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/cron"
)

// taskTelegramAccounts is the consumer-declared seam over the Telegram accounts the identity on
// ctx links; the composition root adapts telegram.Store.LinkedAccounts onto it.
type taskTelegramAccounts interface {
	LinkedTelegramAccounts(ctx context.Context) ([]TelegramAccount, error)
}

// TelegramAccount is the tool-local projection of one linked Telegram account.
type TelegramAccount struct {
	TelegramUserID int64
	Username       string
	FirstName      string
}

// name is how the operator recognises the account: @username, else the first name, with the id.
func (a TelegramAccount) name() string {
	switch {
	case a.Username != "":
		return fmt.Sprintf("@%s (%d)", a.Username, a.TelegramUserID)
	case a.FirstName != "":
		return fmt.Sprintf("%s (%d)", a.FirstName, a.TelegramUserID)
	default:
		return strconv.FormatInt(a.TelegramUserID, 10)
	}
}

// telegramRecipients resolves the accounts a notify=telegram task reaches
// (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md, R2). It returns the
// accounts to persist, or a directive when the operator must choose among two or more, or an
// error the model can act on. A nil seam (the pool-free manifest path) checks nothing.
func (t *TaskTool) telegramRecipients(ctx context.Context, a taskArgs) ([]TelegramAccount, *ToolResult, error) {
	if a.Notify != string(cron.RouteTelegram) {
		if len(a.TelegramRecipients) > 0 {
			return nil, nil, fmt.Errorf("task schedule: telegram_recipients is used only with notify=telegram, not notify=%s. No task was created", a.Notify)
		}
		return nil, nil, nil
	}
	if t.TelegramAccounts == nil {
		return nil, nil, nil
	}
	linked, err := t.TelegramAccounts.LinkedTelegramAccounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("task schedule: read the linked Telegram accounts: %w", err)
	}
	if len(linked) == 0 {
		return nil, nil, fmt.Errorf("task schedule: notify=telegram cannot deliver: no Telegram account is linked to this identity. No task was created: tell the operator to link one in Settings, or ask which other notify route to use")
	}
	if len(a.TelegramRecipients) == 0 {
		if len(linked) == 1 {
			return linked, nil, nil
		}
		directive := telegramRecipientsChoiceResult(linked)
		return nil, &directive, nil
	}
	var named []TelegramAccount
	for _, id := range a.TelegramRecipients {
		if slices.ContainsFunc(named, func(n TelegramAccount) bool { return n.TelegramUserID == id }) {
			continue
		}
		i := slices.IndexFunc(linked, func(l TelegramAccount) bool { return l.TelegramUserID == id })
		if i < 0 {
			return nil, nil, fmt.Errorf("task schedule: Telegram account %d is not linked to this identity (linked: %s). No task was created", id, accountNames(linked))
		}
		named = append(named, linked[i])
	}
	return named, nil, nil
}

func telegramRecipientsChoiceResult(linked []TelegramAccount) ToolResult {
	options := make([]map[string]string, 0, len(linked)+1)
	ids := make([]string, 0, len(linked))
	for _, a := range linked {
		id := strconv.FormatInt(a.TelegramUserID, 10)
		options = append(options, map[string]string{"label": a.name(), "value": id})
		ids = append(ids, id)
	}
	options = append(options, map[string]string{"label": "All linked accounts", "value": strings.Join(ids, ",")})
	raw, err := json.Marshal(struct {
		Status   string              `json:"status"`
		Question string              `json:"question"`
		Options  []map[string]string `json:"options"`
		Message  string              `json:"message"`
	}{
		Status:   "telegram_recipients_required",
		Question: "Which Telegram accounts should the scheduled task reach?",
		Options:  options,
		Message:  "No task was created. Call ask_user with kind=\"choice\", the question and options above. After the operator answers, call task action=\"schedule\" again with the same arguments and telegram_recipients set to the ids in the selected option value.",
	})
	if err != nil {
		raw = []byte(`{"status":"telegram_recipients_required"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

func accountNames(accounts []TelegramAccount) string {
	names := make([]string, len(accounts))
	for i, a := range accounts {
		names[i] = a.name()
	}
	return strings.Join(names, ", ")
}

func accountIDs(accounts []TelegramAccount) []int64 {
	if len(accounts) == 0 {
		return nil
	}
	ids := make([]int64, len(accounts))
	for i, a := range accounts {
		ids[i] = a.TelegramUserID
	}
	return ids
}

// telegramRecipientsListing is the task list's view of a task's recipients.
func telegramRecipientsListing(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
