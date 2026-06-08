// Package telegram — this file is the onboarding writer (the channel side of
// UX-03). A Telegram deep-link "/start <token>" carries a single-use onboarding
// token the setup wizard minted (plan 13-07); this file consumes it through the
// Store's atomic ConsumeOnboarding (one db.WithTx consume-pending-then-INSERT-
// account, plan 13-01) and greets the now-onboarded user. A consumed / expired /
// unknown token writes NO account and replies with a clear "link expired or
// invalid" message (T-13-06-TokenReplay — the single-use chokepoint lives in the
// Store, this file just renders the outcome).
package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// onboardingStore is the consumer-side seam over the Store's single-use consume
// (telegram.Store.ConsumeOnboarding). Declaring it here keeps the handler
// unit-testable with a double and makes the single-write contract explicit: the
// channel calls THIS, never an ad-hoc INSERT.
type onboardingStore interface {
	ConsumeOnboarding(ctx context.Context, p ConsumeParams) (Account, error)
}

// startMsg is the parsed inbound "/start <token>" the handler resolves: the deep-
// link token plus the Telegram sender identity persisted on the account.
type startMsg struct {
	Token          string
	TelegramUserID int64
	Username       string
	FirstName      string
}

// onboarding handles the /start onboarding path over the Store.
type onboarding struct {
	store onboardingStore
}

// newOnboarding builds the onboarding handler over the Store.
func newOnboarding(s onboardingStore) *onboarding {
	return &onboarding{store: s}
}

// handleStart resolves an inbound /start. An empty token is an ordinary greeting
// (NOT an onboarding attempt — no store call). A present token is consumed through
// the Store: success greets the onboarded user (onboarded=true); a consumed /
// expired / unknown token writes no account and returns the invalid-link message
// (onboarded=false).
func (o *onboarding) handleStart(ctx context.Context, m startMsg) (reply string, onboarded bool) {
	token := strings.TrimSpace(m.Token)
	if token == "" {
		return "Ciao! Sono Aura. Scrivimi pure quello che ti serve.", false
	}
	if o.store == nil {
		return "Onboarding non disponibile.", false
	}
	acct, err := o.store.ConsumeOnboarding(ctx, ConsumeParams{
		OnboardingToken: token,
		TelegramUserID:  m.TelegramUserID,
		Username:        m.Username,
		FirstName:       m.FirstName,
	})
	if err != nil {
		if errors.Is(err, ErrTokenConsumed) || errors.Is(err, ErrTokenNotFound) {
			return "Questo link di attivazione è scaduto o non valido. Richiedine uno nuovo dal pannello di setup.", false
		}
		if errors.Is(err, ErrAccountExists) {
			return "Sei già collegato a questa istanza di Aura. Scrivimi pure quello che ti serve.", false
		}
		slog.Warn("telegram onboarding: consume failed", "err", err)
		return "Non sono riuscita a completare l'attivazione. Riprova più tardi.", false
	}
	_ = acct
	return "Attivazione completata. Benvenuto in Aura — scrivimi pure quello che ti serve.", true
}

// parseStartPayload extracts the deep-link token from a raw "/start <token>"
// message (the Telegram deep-link convention: the payload is the first whitespace-
// separated argument after the command, with an optional "@botname" mention
// stripped). A bare /start (or a non-/start message) yields "".
func parseStartPayload(text string) string {
	trimmed := strings.TrimSpace(text)
	head, rest, _ := strings.Cut(trimmed, " ")
	if at := strings.IndexByte(head, '@'); at >= 0 {
		head = head[:at]
	}
	if strings.ToLower(head) != "/start" {
		return ""
	}
	return strings.TrimSpace(rest)
}
