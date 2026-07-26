package telegram

import (
	"context"
	"log/slog"
	"sync"

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
// provisioned with a blank seed — no sentinel is written for those, so they would
// otherwise link Telegram from the admin's QR and never learn the form exists.
type profileOnboarding struct {
	store    profileflow.ProfileMemoryStore
	accounts profileAccountResolver

	mu     sync.Mutex
	nudged map[int64]bool
}

func newProfileOnboarding(store profileflow.ProfileMemoryStore, accounts profileAccountResolver) *profileOnboarding {
	return &profileOnboarding{
		store:    store,
		accounts: accounts,
		nudged:   make(map[int64]bool),
	}
}

// nudge returns the seed-form pointer the FIRST time a chat is seen without an onboarding
// sentinel, and nothing thereafter. It never consumes the turn — the caller sends the text
// and still runs the user's message.
//
// The latch is checked BEFORE the Status read and set on EVERY definitive answer, so a chat
// costs at most one memory call per process. That bound is the point: nudge runs
// synchronously on the inbound-message hot path, and Status opens a fresh memory-MCP
// session (connect + handshake + call + close, budgeted in tens of seconds), so latching
// only the emit path would make an already-onboarded operator pay that on every message.
// A read that FAILS is not definitive and stays unlatched, so a sidecar blip self-heals.
func (p *profileOnboarding) nudge(ctx context.Context, chatID, telegramUserID int64) (string, bool) {
	if p == nil || p.store == nil || p.accounts == nil {
		return "", false
	}
	p.mu.Lock()
	already := p.nudged[chatID]
	p.mu.Unlock()
	if already {
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
	p.mu.Lock()
	p.nudged[chatID] = true
	p.mu.Unlock()
	if st.Completed || st.Skipped {
		return "", false
	}
	return seedFormNudge, true
}
