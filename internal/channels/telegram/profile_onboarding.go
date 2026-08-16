package telegram

import (
	"context"
	"log/slog"

	profileflow "github.com/chetto1983/aura/internal/onboarding"
)

// seedFormNudge is the Italian pointer at the cockpit's profile seed form. It names what
// is actually produced (memory the agent keeps updating), never an Agent.md artifact —
// Amendment #87 deleted that file and Amendment #95 deleted the interview that promised it.
const seedFormNudge = "Non ho ancora il tuo profilo: aprilo dal pannello web (Impostazioni → Profilo) e compila il modulo — bastano nome, lingua e dove sei. Poi tengo la memoria aggiornata da sola mentre lavoriamo."

type profileAccountResolver interface {
	GetAccountByTelegramID(ctx context.Context, telegramUserID int64) (Account, error)
}

// profileOnboarding is the channel's ONLY remaining onboarding behaviour (Amendment #95):
// a one-shot nudge toward the cockpit's seed form. It exists for the identity an admin
// provisioned with a blank seed — no gate is set for those, so they would otherwise link
// Telegram from the admin's QR and never learn the form exists.
//
// It holds no state. It used to carry a mutex and a per-chat latch map, because the gate
// read was an agent-memory MCP session (connect, handshake, call, close) on the inbound
// hot path and an onboarded operator could not be made to pay it per message. The gate is
// one indexed Postgres row now, so the cache is gone and with it the bug it hid: an
// in-process latch made "once" mean "once per daemon restart", and every restart nudged
// again someone who had already been told. The channel asks; the store remembers.
type profileOnboarding struct {
	store    profileflow.Store
	accounts profileAccountResolver
}

func newProfileOnboarding(store profileflow.Store, accounts profileAccountResolver) *profileOnboarding {
	return &profileOnboarding{store: store, accounts: accounts}
}

// nudge returns the seed-form pointer the first time an operator is seen without an
// onboarding gate, and nothing thereafter. It never consumes the turn — the caller sends
// the text and still runs the user's message.
//
// A read that FAILS says nothing, so it stays silent and self-heals on the next message.
// A failed MarkNudged only costs a repeated nudge, so it is logged and the text still goes
// out: staying quiet because the bookkeeping failed would lose the one message that tells
// the operator the form exists.
func (p *profileOnboarding) nudge(ctx context.Context, _, telegramUserID int64) (string, bool) {
	if p == nil || p.store == nil || p.accounts == nil {
		return "", false
	}
	acct, err := p.accounts.GetAccountByTelegramID(ctx, telegramUserID)
	if err != nil {
		return "", false
	}
	st, err := p.store.Status(ctx, acct.IdentityID)
	if err != nil {
		slog.Warn("telegram profile nudge: read status", "identity", acct.IdentityID, "err", err)
		return "", false
	}
	if st.Completed || st.Skipped || st.Nudged {
		return "", false
	}
	if err := p.store.MarkNudged(ctx, acct.IdentityID); err != nil {
		slog.Warn("telegram profile nudge: mark nudged", "identity", acct.IdentityID, "err", err)
	}
	return seedFormNudge, true
}
