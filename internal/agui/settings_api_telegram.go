package agui

// settings_api_telegram.go is the Telegram slice of the Settings page backend: the
// bot-token availability probe and the web half of the D-24 linking flow. Split out
// of settings_api.go when the hot-profile work (amendment #188) pushed that file
// past the 600-LOC cap; the handlers are byte-identical.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

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
		token, err = s.effectiveSettingValue(r.Context(), "TELEGRAM_BOT_TOKEN")
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
	requiresRestart := token != os.Getenv("TELEGRAM_BOT_TOKEN")
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
