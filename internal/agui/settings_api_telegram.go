package agui

// settings_api_telegram.go is the Telegram slice of the Settings page backend: the
// bot-token availability probe, the hot swap a token write triggers on the running
// channel, and the web half of the D-24 linking flow. Split out of settings_api.go
// when the hot-profile work (amendment #188) pushed that file past the 600-LOC cap.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// telegramTokenKey is the settings row the Telegram channel polls with.
const telegramTokenKey = "TELEGRAM_BOT_TOKEN" // #nosec G101 -- a settings key name, not a credential.

// TelegramActivator hot-swaps the daemon's Telegram channel onto token; an empty token
// stops it. Its error text reaches the operator verbatim as channel_error, so it must
// be short and never carry the token (T-13-07-BotTokenLeak).
type TelegramActivator func(ctx context.Context, token string) error

// telegramChannelPorts is the daemon's running Telegram channel as the Settings routes
// see it: the swap a token write triggers, and whether the live channel polls a token.
type telegramChannelPorts struct {
	activate TelegramActivator
	running  func(token string) bool
}

// SetTelegramActivator wires the hot swap the token PUT/DELETE call, and running, which
// reports whether the live channel polls with a token; it decides requiresRestart and
// the row's applied state. Until set, a token write only persists, and the availability
// check asks for a restart.
func (s *Server) SetTelegramActivator(activate TelegramActivator, running func(token string) bool) {
	s.telegram = &telegramChannelPorts{activate: activate, running: running}
}

// telegramActivation is what a token write reports about the channel.
type telegramActivation struct {
	RestartRequired bool   `json:"restart_required"`
	ChannelActive   bool   `json:"channel_active"`
	ChannelError    string `json:"channel_error,omitempty"`
}

type telegramTokenPutDTO struct {
	settingItemDTO
	telegramActivation
}

type telegramTokenDeleteDTO struct {
	Key     string `json:"key"`
	Deleted bool   `json:"deleted"`
	telegramActivation
}

// activateTelegram hands a just-written token to the running channel. The row is
// already saved, so a failed start is reported in the body rather than as an HTTP
// error: the value stays, and a restart or the next write retries it.
func (s *Server) activateTelegram(ctx context.Context, token string) telegramActivation {
	token = strings.TrimSpace(token)
	if err := s.telegram.activate(ctx, token); err != nil {
		return telegramActivation{RestartRequired: true, ChannelError: err.Error()}
	}
	return telegramActivation{ChannelActive: token != ""}
}

// telegramRuns reports whether the live Telegram channel polls with token.
func (s *Server) telegramRuns(token string) bool {
	return s.telegram != nil && s.telegram.running(strings.TrimSpace(token))
}

type telegramAvailabilityRequest struct {
	Token string `json:"token,omitempty"`
}

type telegramAvailabilityDTO struct {
	Configured      bool   `json:"configured"`
	Available       bool   `json:"available"`
	BotUsername     string `json:"botUsername,omitempty"`
	RequiresRestart bool   `json:"requiresRestart"`
	Error           string `json:"error,omitempty"`
}

func (s *Server) handleCheckTelegramAvailability(w http.ResponseWriter, r *http.Request) {
	if s.telegramProbe == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "telegram validation not configured"})
		return
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var req telegramAvailabilityRequest
	if strings.TrimSpace(string(raw)) != "" {
		if err := json.Unmarshal(raw, &req); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		var err error
		token, err = s.effectiveSettingValue(r.Context(), telegramTokenKey)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
			return
		}
	}
	if token == "" {
		writeJSON(w, telegramAvailabilityDTO{Configured: false, Available: false})
		return
	}
	username, err := s.telegramProbe(r.Context(), token)
	requiresRestart := !s.telegramRuns(token)
	if err != nil {
		writeJSON(w, telegramAvailabilityDTO{
			Configured:      true,
			Available:       false,
			RequiresRestart: requiresRestart,
			Error:           "bot token validation failed",
		})
		return
	}
	writeJSON(w, telegramAvailabilityDTO{
		Configured:      true,
		Available:       true,
		BotUsername:     username,
		RequiresRestart: requiresRestart,
	})
}

// handleCreateSettingsTelegramLink mints the one-time Telegram linking code for the
// AUTHENTICATED caller (D-02: a normal self-scoped USER action, each user links their
// own Telegram to their OWN identity — NEVER operator-pinned). It is the web half of the
// D-24 web-initiated linking flow: CreateTelegramLink scopes to `requester` (the bound
// principal, never the seeded local admin), and the returned deep-link carries the code
// ONLY on the <=1h `?start=` setup-bootstrap URL — no long-lived session token ever
// crosses a URL/query string (MUSR-06). The bot then binds this sender's chat-id to
// `requester`'s identity when the code arrives via /start (telegram onboarding consume).
func (s *Server) handleCreateSettingsTelegramLink(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		http.Error(w, "onboarding service not configured", http.StatusServiceUnavailable)
		return
	}
	requester, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	link, err := s.onboarding.CreateTelegramLink(r.Context(), requester)
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, link)
}

func (s *Server) handleSettingsTelegramStatus(w http.ResponseWriter, r *http.Request) {
	handleOnboardingSessionRequest(s, w, r, func(ctx context.Context, requester, token string) (OnboardingTelegramStatus, error) {
		return s.onboarding.TelegramStatus(ctx, requester, token)
	})
}
