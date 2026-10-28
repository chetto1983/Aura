package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/cron"
)

// linkedTelegramAccounts is the seam over the accounts the identity on ctx links;
// taskTelegramAccounts satisfies it, as it does for the task tool.
type linkedTelegramAccounts interface {
	LinkedTelegramAccounts(ctx context.Context) ([]tools.TelegramAccount, error)
}

// cliTelegramRecipients applies the task tool's rule (spec 2026-10-28-telegram-push-recipients,
// R2) to `aura task schedule`: flag is a comma-separated list of Telegram user ids, or "all" for
// every account linked now. The CLI cannot ask, so two or more links with none named is refused
// with the accounts to choose from.
func cliTelegramRecipients(ctx context.Context, accounts linkedTelegramAccounts, notify, flag string) ([]int64, error) {
	if notify != string(cron.RouteTelegram) {
		if flag != "" {
			return nil, fmt.Errorf("--telegram-recipients is used only with --notify=telegram, not --notify=%s", notify)
		}
		return nil, nil
	}
	linked, err := accounts.LinkedTelegramAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the linked Telegram accounts: %w", err)
	}
	named, err := parseTelegramRecipients(flag, linked)
	if err != nil {
		return nil, err
	}
	picked, err := tools.PickTelegramRecipients(linked, named)
	if errors.Is(err, tools.ErrTelegramRecipientsRequired) {
		return nil, fmt.Errorf("%w: pass --telegram-recipients with their ids, or all", err)
	}
	if err != nil {
		return nil, err
	}
	return tools.TelegramRecipientIDs(picked), nil
}

func parseTelegramRecipients(flag string, linked []tools.TelegramAccount) ([]int64, error) {
	if strings.TrimSpace(flag) == "all" {
		return tools.TelegramRecipientIDs(linked), nil
	}
	var ids []int64
	for field := range strings.SplitSeq(flag, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("--telegram-recipients: %q is not a Telegram user id", field)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
