package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mdp/qrterminal/v3"
)

// onboardingTTL is the lifetime of a minted onboarding token (D-03: 1h
// single-use). The Store enforces consumed_at IS NULL AND expires_at > now() on
// consume; this is the mint-side expiry the channel's /start races.
const onboardingTTL = time.Hour

// handleToken (POST /setup/token) validates the supplied bot token via the
// BotProbe (telebot getMe seam) and, on success, records that a bot is
// configured + returns its username. The token is a secret: it is read from the
// body, passed to the probe, and NEVER logged or echoed (T-13-07-BotTokenLeak).
// An invalid/unreachable token is a 400 with a non-leaky message (the probe
// error is not surfaced verbatim — it could embed the token).
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	var req TokenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}
	if s.probe == nil {
		http.Error(w, "bot validation unavailable", http.StatusServiceUnavailable)
		return
	}
	username, err := s.probe(r.Context(), req.Token)
	if err != nil {
		// Do NOT surface err.Error() — a telebot/transport error can echo the
		// token back. A fixed message keeps the secret off the wire and the log.
		slog.Warn("setup: bot token validation failed")
		http.Error(w, "bot token validation failed", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.botUsername = username
	s.botConfigured = true
	s.mu.Unlock()
	writeJSON(w, TokenResponse{OK: true, BotUsername: username})
}

// handleOnboardLink (POST /setup/onboard-link) mints a single-use onboarding
// UUID, INSERTs a telegram_setup_pending row (1h TTL) via the Store FK'd to the
// local identity, returns {deep_link, qr_svg:""} (qr_svg deferred OQ4), and
// prints a terminal ASCII QR of the deep-link (qrterminal) — the real onboarding
// path this phase. A missing bot username (no /setup/token yet) is a 409.
func (s *Server) handleOnboardLink(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	bot, configured := s.botUsername, s.botConfigured
	s.mu.RUnlock()
	if !configured || bot == "" {
		http.Error(w, "bot not configured: POST /setup/token first", http.StatusConflict)
		return
	}
	onboardingToken := uuid.NewString()
	err := s.store.InsertPending(r.Context(), InsertPendingParams{
		OnboardingToken: onboardingToken,
		IdentityID:      s.identityID,
		GeneratedBy:     "setup-wizard",
		ExpiresAt:       time.Now().UTC().Add(onboardingTTL),
	})
	if err != nil {
		slog.Error("setup: insert onboarding pending", "err", err)
		http.Error(w, "failed to mint onboarding link", http.StatusInternalServerError)
		return
	}
	deepLink := deepLink(bot, onboardingToken)
	// The terminal ASCII QR is the live onboarding path (D-04): the operator
	// shows it; the user scans it to open the deep-link in Telegram. Low
	// redundancy (qrterminal.L) keeps the block terminal-sized for a short, high-
	// contrast deep-link URL. qr_svg stays empty (OQ4 — the SVG body is deferred
	// to the future frontend; the field remains for forward-compat).
	qrterminal.Generate(deepLink, qrterminal.L, s.qrOut)
	writeJSON(w, OnboardLinkResponse{DeepLink: deepLink, QRSVG: qrSVG(deepLink)})
}

// handleStatus (GET /setup/status) reports whether a bot is configured + the
// onboarded account count (Store.CountAccounts). last_activity is the server
// clock (the wizard polls it as a liveness marker); a richer per-account marker
// is a future enhancement.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	configured := s.botConfigured
	s.mu.RUnlock()
	count, err := s.store.CountAccounts(r.Context())
	if err != nil {
		slog.Error("setup: count accounts", "err", err)
		http.Error(w, "failed to read status", http.StatusInternalServerError)
		return
	}
	writeJSON(w, StatusResponse{
		BotConfigured: configured,
		AccountCount:  count,
		LastActivity:  time.Now().UTC().Format(time.RFC3339),
	})
}

// handleEvents (GET /setup/events) is the SSE completion stream: it sets the
// event-stream headers then polls telegram_setup_pending.consumed_at every
// pollInterval (2s default, D-03) for the token in ?onboarding= and, when the
// row flips to consumed, emits one onboarding_completed event carrying the
// telegram_user_id + username. The poll goroutine MUST exit on r.Context().Done()
// — client disconnect AND server-shutdown both cancel the request ctx
// (goleak-clean, T-13-07-SSELeak / Pitfall 5). Without ?onboarding= the stream
// is a no-op heartbeat that still terminates on disconnect.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	onboardingToken := r.URL.Query().Get("onboarding")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	ctx := r.Context()
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Client disconnect OR server shutdown — the request ctx is cancelled
			// either way. The pump unwinds here (goleak-clean, Pitfall 5).
			return
		case <-ticker.C:
			done, ev := s.pollCompletion(ctx, onboardingToken)
			if !done {
				continue
			}
			// Onboarding completed: burn the one-time setup token BEFORE signaling
			// completion (the wizard's single-use contract — a second navigation 401s).
			// Invalidating first closes the race where a client that has just read the
			// event fires a follow-up /setup/* request before the burn lands — observed
			// only on the slower Windows CI runner (locally + Linux were fast enough).
			s.InvalidateToken()
			writeSSE(w, ev)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
	}
}

// pollCompletion checks one consumed_at poll: it returns (true, event) once the
// pending row for onboardingToken is consumed, (false, _) otherwise. An empty
// onboardingToken (no token to watch) or a transient Store error is a non-fatal
// (false, _) — the pump keeps polling until the client leaves. The event carries
// only the type: the schema has no token→account link, so telegram_user_id /
// username stay unset (omitempty) rather than inventing a row that does not exist.
func (s *Server) pollCompletion(ctx context.Context, onboardingToken string) (bool, SetupEvent) {
	if onboardingToken == "" {
		return false, SetupEvent{}
	}
	consumed, err := s.store.PendingConsumed(ctx, onboardingToken)
	if err != nil {
		// Unknown/transient — keep polling, do not tear the stream down.
		return false, SetupEvent{}
	}
	if !consumed {
		return false, SetupEvent{}
	}
	return true, SetupEvent{Type: onboardingCompletedEvent}
}

// deepLink builds the t.me onboarding deep-link the user taps/scans: opening it
// starts a chat with the bot and sends `/start <token>`, which the channel's
// onboarding handler (13-06) consumes single-use.
func deepLink(botUsername, onboardingToken string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s", botUsername, onboardingToken)
}

// writeJSON encodes v as a JSON response body with the application/json content
// type. An encode failure is logged, not surfaced (the header is already set).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("setup: encode json response", "err", err)
	}
}

// writeSSE writes one SSE `data:` frame carrying the JSON-encoded event. A
// marshal failure drops the frame (the next poll retries) rather than corrupting
// the stream.
func writeSSE(w http.ResponseWriter, ev SetupEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		slog.Warn("setup: marshal sse event", "err", err)
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}
