package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/cron"
)

// deliveryPlan is where an accepted task delivers: the line the reply carries, and the Telegram
// accounts a notify=telegram task stores.
type deliveryPlan struct {
	Note       string
	Recipients []TelegramAccount
}

// resolveDelivery fills a.Notify (explicit, else the default) and runs the route, destination
// and Telegram-recipient checks. A non-nil *ToolResult is a directive the model relays
// (delivery_choice_required, telegram_recipients_required); nothing is persisted.
func (t *TaskTool) resolveDelivery(ctx context.Context, a *taskArgs) (deliveryPlan, *ToolResult, error) {
	fromDefault := false
	if strings.TrimSpace(a.Notify) == "" {
		a.Notify = t.defaultNotify(ctx)
		if a.Notify == "" {
			directive := taskNotifyChoiceRequiredResult()
			return deliveryPlan{}, &directive, nil
		}
		fromDefault = true
	}
	// The JSON-schema enum is advisory: an OpenAI-compat provider may pass anything
	// through. Reject it here, where the model can still correct itself.
	if !cron.ValidNotifyRoute(a.Notify) {
		return deliveryPlan{}, nil, fmt.Errorf("task schedule: notify %q is not a delivery route (want none|whatsapp|email|stdout|telegram)", a.Notify)
	}
	// Measured 2026-10-05: a whatsapp reminder with no recipient was accepted here, fired
	// and reached nobody, while the model promised it "on your work number". A route that
	// cannot deliver is refused now, and an accepted one names whom it reaches. A default
	// route gets the same check: the operator set it, but the link may have gone since.
	recipient, err := t.destination(ctx, a.Notify)
	if err != nil {
		return deliveryPlan{}, nil, fmt.Errorf("task schedule: notify=%s cannot deliver: %w. No task was created: tell the operator why, or ask which other notify route to use", a.Notify, err)
	}
	recipients, directive, err := t.telegramRecipients(ctx, *a)
	if err != nil || directive != nil {
		return deliveryPlan{}, directive, err
	}
	if len(recipients) > 0 {
		recipient = accountNames(recipients)
	}
	return deliveryPlan{Note: deliveryNote(a.Notify, recipient, fromDefault), Recipients: recipients}, nil, nil
}

func (t *TaskTool) destination(ctx context.Context, route string) (string, error) {
	if t.Destinations == nil {
		return "", nil
	}
	return t.Destinations.Destination(ctx, cron.NotifyRoute(route))
}
