package agui

// settings_api_telegram_accounts.go lists and unlinks the caller's linked Telegram accounts
// (docs/superpowers/specs/2026-10-27-telegram-account-links-design.md, D4). The identity is
// always the principal's, never the path's: another identity's link answers 404, which does
// not tell the caller the link exists.

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// TelegramAccount is one linked account as Settings shows it. A Telegram user id "has at most
// 52 significant bits" (Bot API, User.id), so a JSON number carries it exactly. LastSeenAt is
// nil until the account sends its first update.
type TelegramAccount struct {
	TelegramUserID int64      `json:"telegramUserId"`
	Username       string     `json:"username"`
	FirstName      string     `json:"firstName"`
	AddedAt        time.Time  `json:"addedAt"`
	LastSeenAt     *time.Time `json:"lastSeenAt"`
}

// TelegramAccountLinks is the store seam: an identity's links, oldest first, and the unlink
// of one of them, which reports false when that identity has no such link.
type TelegramAccountLinks interface {
	LinkedAccounts(ctx context.Context, identityID string) ([]TelegramAccount, error)
	UnlinkAccount(ctx context.Context, identityID string, telegramUserID int64) (bool, error)
}

// SetTelegramAccountLinks wires the store the two routes read. Until set, they answer 503.
func (s *Server) SetTelegramAccountLinks(links TelegramAccountLinks) { s.telegramLinks = links }

func (s *Server) registerTelegramAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/telegram/accounts", s.handleListTelegramAccounts)
	mux.HandleFunc("DELETE /api/settings/telegram/accounts/{telegramUserId}", s.handleUnlinkTelegramAccount)
}

func (s *Server) handleListTelegramAccounts(w http.ResponseWriter, r *http.Request) {
	requester, ok := s.telegramAccountsRequester(w, r)
	if !ok {
		return
	}
	accounts, err := s.telegramLinks.LinkedAccounts(r.Context(), requester)
	if err != nil {
		slog.WarnContext(r.Context(), "agui: list telegram accounts failed", "err", err)
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "telegram accounts unavailable"})
		return
	}
	if accounts == nil {
		accounts = []TelegramAccount{}
	}
	writeJSON(w, accounts)
}

func (s *Server) handleUnlinkTelegramAccount(w http.ResponseWriter, r *http.Request) {
	requester, ok := s.telegramAccountsRequester(w, r)
	if !ok {
		return
	}
	telegramUserID, err := strconv.ParseInt(r.PathValue("telegramUserId"), 10, 64)
	if err != nil || telegramUserID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "telegramUserId must be a positive integer"})
		return
	}
	unlinked, err := s.telegramLinks.UnlinkAccount(r.Context(), requester, telegramUserID)
	if err != nil {
		slog.WarnContext(r.Context(), "agui: unlink telegram account failed", "err", err)
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "telegram accounts unavailable"})
		return
	}
	if !unlinked {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "telegram account not linked"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// telegramAccountsRequester answers 503 while the seam is unset and 401 with no principal.
func (s *Server) telegramAccountsRequester(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.telegramLinks == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "telegram accounts not configured"})
		return "", false
	}
	requester, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	return requester, true
}
