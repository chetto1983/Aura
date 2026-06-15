package setup

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// setupReadHeaderTimeout bounds the request-header read to defang slow-loris on
// the loopback setup endpoint (mirrors aguiReadHeaderTimeout in serve.go).
const setupReadHeaderTimeout = 10 * time.Second

// setupHeaderName is the alternate token-presentation header (the query param
// ?token= is the other). A canonical header name avoids a case-mismatch miss.
const setupHeaderName = "X-Aura-Setup-Token"

// Store is the narrow DB seam the setup handlers consume (D-A2-02, declared
// consumer-side so the server depends only on the methods it calls and tests can
// pass a fake). *telegram.Store satisfies it implicitly: InsertPending mints an
// onboarding row, PendingConsumed polls the SSE completion signal (consumed_at),
// CountAccounts backs the status footer.
//
// NOTE: the 0012 telegram_setup_pending schema records consumed_at but NOT which
// telegram_user_id consumed the token — there is no token→account link to follow
// (a consumed token shares only the identity_id, which is the single `local`
// identity here). So the onboarding_completed SSE event is keyed purely on the
// consumed_at signal; its telegram_user_id/username fields stay omitempty rather
// than inventing a schema column (that would be a migration = Rule 4).
type Store interface {
	InsertPending(ctx context.Context, p InsertPendingParams) error
	PendingConsumed(ctx context.Context, onboardingToken string) (bool, error)
	CountAccounts(ctx context.Context) (int64, error)
}

// InsertPendingParams is the plain projection the setup server passes to the
// Store to mint one onboarding token. It mirrors telegram.InsertPendingParams
// field-for-field (declared here so the setup package needs no telegram import;
// the composition root adapts one onto the other in 13-09).
type InsertPendingParams struct {
	OnboardingToken string
	IdentityID      string
	GeneratedBy     string
	ExpiresAt       time.Time
}

// BotProbe validates a Telegram bot token and returns the bot username. In
// production it is a telebot getMe (tele.NewBot does a live getMe; the username
// is bot.Me.Username); in tests it is a mock. It MUST NOT log the token — the
// caller passes a secret. An invalid token returns a non-nil error.
type BotProbe func(ctx context.Context, token string) (username string, err error)

// Deps carries everything NewServer needs. Bind defaults to the loopback :9081
// (config resolves AURA_SETUP_BIND); Token is the configured AURA_SETUP_TOKEN
// (empty → boot-generated, printed to TokenOut); IdentityID is the local
// identity an onboarding token FKs to (the composition root resolves the `local`
// seed in 13-09).
type Deps struct {
	Store      Store
	Probe      BotProbe
	Bind       string
	Token      string
	IdentityID string
	TokenOut   io.Writer // os.Stdout in prod; a buffer in tests (nil → os.Stdout)
	// QROut is where the onboarding ASCII QR is rendered (os.Stdout in prod; a
	// discard/buffer in tests to keep output quiet). nil → os.Stdout.
	QROut io.Writer
	// PollInterval is the SSE consumed_at poll period (default 2s, D-03). A test
	// shortens it so the SSE assertion does not wait 2s for a tick.
	PollInterval time.Duration
}

// Server is the isolated loopback setup-wizard HTTP gateway (:9081). It is the
// thinnest glue over the narrow Store + the BotProbe seam + the one-time Token;
// the bind is loopback by default (T-13-07-SetupExposure) and the token gate is
// mandatory on every route (T-13-07-SetupToken). botConfigured tracks whether a
// bot token has been supplied (via /setup/token) for the status footer.
type Server struct {
	store        Store
	probe        BotProbe
	token        *Token
	identityID   string
	pollInterval time.Duration

	qrOut io.Writer

	mu            sync.RWMutex
	botUsername   string
	botConfigured bool
}

// defaultPollInterval is the SSE consumed_at poll period (D-03, open-question-3
// default; LISTEN/NOTIFY deferred).
const defaultPollInterval = 2 * time.Second

// NewServer builds the setup gateway over the supplied deps. An empty
// Deps.Token generates a one-time UUIDv4 printed to TokenOut (os.Stdout when
// nil). A non-positive PollInterval falls back to the 2s default.
func NewServer(deps Deps) *Server {
	out := deps.TokenOut
	if out == nil {
		out = os.Stdout
	}
	qrOut := deps.QROut
	if qrOut == nil {
		qrOut = os.Stdout
	}
	poll := deps.PollInterval
	if poll <= 0 {
		poll = defaultPollInterval
	}
	return &Server{
		store:        deps.Store,
		probe:        deps.Probe,
		token:        NewToken(deps.Token, out),
		identityID:   deps.IdentityID,
		pollInterval: poll,
		qrOut:        qrOut,
	}
}

// Mux registers the four routes using Go 1.22+ method-pattern routing (no
// chi/gorilla — matches the no-router codebase posture, mirrors agui.Server.Mux)
// each wrapped by requireSetupToken (T-13-07-SetupToken: the gate is mandatory
// on every /setup/* route, not a subset).
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /setup", s.requireSetupToken(http.HandlerFunc(s.handleRoot)))
	mux.Handle("GET /setup/{$}", s.requireSetupToken(http.HandlerFunc(s.handleRoot)))
	mux.Handle("POST /setup/token", s.requireSetupToken(http.HandlerFunc(s.handleToken)))
	mux.Handle("POST /setup/onboard-link", s.requireSetupToken(http.HandlerFunc(s.handleOnboardLink)))
	mux.Handle("GET /setup/status", s.requireSetupToken(http.HandlerFunc(s.handleStatus)))
	mux.Handle("GET /setup/events", s.requireSetupToken(http.HandlerFunc(s.handleEvents)))
	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	target := "/setup/status"
	if token := r.URL.Query().Get("token"); token != "" {
		target += "?token=" + url.QueryEscape(token)
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// HTTPServer builds the *http.Server the daemon runs (loopback bind + a
// slow-loris-defanging ReadHeaderTimeout, mirrors serve.go). The composition
// root (13-09) owns ListenAndServe + the graceful Shutdown call.
func (s *Server) HTTPServer(bind string) *http.Server {
	return &http.Server{
		Addr:              bind,
		Handler:           s.Mux(),
		ReadHeaderTimeout: setupReadHeaderTimeout,
	}
}

// InvalidateToken burns the one-time token after onboarding completes (the
// channel persisted the account). A subsequent /setup/* call 401s. Exposed so
// the SSE pump (handleEvents) and the composition root can fire it.
func (s *Server) InvalidateToken() { s.token.Invalidate() }

// requireSetupToken is the mandatory gate (T-13-07-SetupToken, amendment #10):
// 401 unless the `?token=` query param OR the X-Aura-Setup-Token header equals
// the live one-time token (constant-time compare in Token.Valid). After
// onboarding the token is invalidated, so a previously-valid token 401s.
func (s *Server) requireSetupToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := r.URL.Query().Get("token")
		if presented == "" {
			presented = r.Header.Get(setupHeaderName)
		}
		if !s.token.Valid(presented) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// The four route handlers (handleToken/handleOnboardLink/handleStatus/
// handleEvents) live in handlers.go alongside qr.go. The gate (requireSetupToken)
// and lifecycle (NewServer/Mux/HTTPServer) own this file; the endpoint bodies
// own handlers.go (the token getMe validate, the onboard-link mint + ASCII QR,
// the status footer, the SSE consumed_at pump).
