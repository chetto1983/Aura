package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/conversation"
	tele "gopkg.in/telebot.v4"
)

// onStatus handles the /status command, returning budget and context info.
func (b *Bot) onStatus(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Aura Status\n\n")

	// Budget info
	if b.budget != nil {
		status := b.budget.Status()
		fmt.Fprintf(&sb, "Tokens used: %d\n", status.TotalTokens)
		fmt.Fprintf(&sb, "Estimated cost: $%.4f\n", status.TotalCost)
		if status.SoftBudget > 0 {
			fmt.Fprintf(&sb, "Soft budget: $%.2f\n", status.SoftBudget)
		}
		if status.HardBudget > 0 {
			fmt.Fprintf(&sb, "Hard budget: $%.2f\n", status.HardBudget)
		}
		if status.SoftBudgetExceeded && !status.BudgetExceeded {
			sb.WriteString("Status: SOFT BUDGET REACHED\n")
		}
		if status.BudgetExceeded {
			sb.WriteString("Status: HARD BUDGET EXCEEDED\n")
		}
	} else {
		sb.WriteString("Budget: not configured\n")
	}

	// Per-conversation context info
	ctxVal, ok := b.ctxMap.Load(userID)
	if ok {
		convCtx := ctxVal.(*conversation.Context)
		fmt.Fprintf(&sb, "\nContext tokens: %d / %d\n", convCtx.EstimatedTokens(), convCtx.MaxTokens())
		fmt.Fprintf(&sb, "Conversation tokens used: %d\n", convCtx.TotalTokensUsed())
		if b.budget != nil {
			predictedCost := b.budget.PredictCost(convCtx.EstimatedTokens(), 500)
			fmt.Fprintf(&sb, "Next call est. cost: $%.4f\n", predictedCost)
		}
	}

	return c.Send(sb.String())
}

// notifySoftBudget sends a one-time warning to the user when soft budget
// is first exceeded. userID is kept on the signature so future per-user
// throttling has a hook without changing call sites.
func (b *Bot) notifySoftBudget(c tele.Context, _ string) {
	if b.budget != nil && b.budget.ShouldNotifySoftBudget() {
		status := b.budget.Status()
		c.Send(fmt.Sprintf("Soft budget reached ($%.2f / $%.2f). LLM calls continue until hard budget is hit.", status.TotalCost, status.SoftBudget))
	}
}

// BudgetStatus returns the current budget status for external consumers.
func (b *Bot) BudgetStatus() budget.Status {
	if b.budget == nil {
		return budget.Status{}
	}
	return b.budget.Status()
}
