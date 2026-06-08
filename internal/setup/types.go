// Package setup is the setup-wizard backend (Phase 13 / Slice 9a, UX-03): an
// isolated loopback HTTP server (:9081, distinct from the AG-UI gateway :9080)
// that issues the single-use onboarding token the Telegram channel's /start
// consumes (plan 13-06) and surfaces onboarding completion over SSE. The API is
// backend-only this phase (the HTML frontend is deferred, D-03); the real
// onboarding path is a terminal ASCII QR of the deep-link (qrterminal), with the
// qr_svg JSON field kept empty for forward-compat (OQ4).
//
// Every /setup/* route is gated by a one-time in-memory token (requireSetupToken,
// amendment #10): generated as a random UUIDv4 + printed once to stdout on first
// boot when AURA_SETUP_TOKEN is empty, held in memory only (no disk), and
// invalidated after onboarding completes. The bot token supplied to /setup/token
// is validated via telebot getMe and is NEVER logged (T-13-07-BotTokenLeak).
package setup

// TokenRequest is the POST /setup/token body: the Telegram Bot API token to
// validate via getMe. It is a secret — it is never logged and never echoed back.
type TokenRequest struct {
	Token string `json:"token"`
}

// TokenResponse is the POST /setup/token result: getMe succeeded and the bot
// username (the only non-secret getMe field surfaced) is returned so the wizard
// can build the t.me/<bot> deep-link.
type TokenResponse struct {
	OK          bool   `json:"ok"`
	BotUsername string `json:"bot_username"`
}

// OnboardLinkResponse is the POST /setup/onboard-link result: the deep-link a
// user taps to onboard plus the qr_svg body. qr_svg is DEFERRED this phase (OQ4)
// — it stays in the contract for the future frontend but is always the empty
// string; the real onboarding QR is the terminal ASCII one printed to stdout.
type OnboardLinkResponse struct {
	DeepLink string `json:"deep_link"`
	QRSVG    string `json:"qr_svg"`
}

// StatusResponse is the GET /setup/status result: whether a bot token is
// configured, how many accounts have onboarded, and the last-activity marker.
type StatusResponse struct {
	BotConfigured bool   `json:"bot_configured"`
	AccountCount  int64  `json:"account_count"`
	LastActivity  string `json:"last_activity"`
}

// SetupEvent is one Server-Sent Event on GET /setup/events. The only event type
// emitted this phase is "onboarding_completed", carrying the Telegram user id +
// username of the account that just consumed its onboarding token.
type SetupEvent struct {
	Type           string `json:"type"`
	TelegramUserID int64  `json:"telegram_user_id,omitempty"`
	Username       string `json:"username,omitempty"`
}

// onboardingCompletedEvent is the SSE event type emitted when a pending row's
// consumed_at flips (the channel persisted the account).
const onboardingCompletedEvent = "onboarding_completed"
