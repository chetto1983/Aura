package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// onboarding_api.go is the thin REST adapter over the server-held OnboardingService
// (ONBD-01/02). Four routes under the /api/ carve-out:
//
//   POST /api/onboarding/start                         (RequireCapability identity.create)
//   POST /api/onboarding/{sessionToken}/step           (RequireAuth — session authz'd at start)
//   POST /api/onboarding/{sessionToken}/provision      (RequireCapability identity.create)
//   GET  /api/onboarding/{sessionToken}/telegram-status (RequireAuth)
//
// The parent-mux mount (the capability gate on start+provision, the whole-origin
// RequireAuth on all four) lives in cmd/aura/serve_webui.go. There is NO business logic
// here: every handler size-caps the body (MaxBytesReader), decodes→400, validates the
// intent enum / field lengths→sanitized-400, reads the authenticated principal as the
// creating operator, then makes ONE service call. The one-LLM-extraction-per-step
// guarantee, the cross-store saga + compensation, the no-escalation re-validation, the QR
// render, and the no-secret-leak logging all live in the service (onboarding_service.go /
// onboarding_provision.go), mirroring graph_api.go's "handler parses, the seam does the
// work" split.

// onboarding* body/field caps bound the untrusted wizard input before it reaches the
// service (V5 length-cap). Email/password/capability-name lengths are generous but finite
// so a crafted body is a clean 400, not an unbounded allocation. The capability-name
// grammar is re-checked at the identity store (GrantCapability), so these are defense-in-
// depth, not the sole control.
const (
	onboardingTextMaxLen             = 8192
	onboardingEmailMaxLen            = 320 // RFC 5321 max email length
	onboardingPasswordMaxLen         = 1024
	onboardingSecurityQuestionMaxLen = 256
	onboardingSecurityAnswerMaxLen   = 512
	onboardingCapNameMaxLen          = 64 // identity.capNameRe upper bound
	onboardingMaxCaps                = 64
)

// validOnboardingIntents is the closed step-intent set the wizard drives. Anything else
// is a clean 400 (never reaches the session state machine).
var validOnboardingIntents = map[string]bool{
	"answer":  true,
	"confirm": true,
	"edit":    true,
	"skip":    true,
}

// OnboardingStart is the POST /start response: the opaque session token, the first step +
// its prompt content + status, and the D-06 capability picker options (the creator's
// grants with '*' excluded).
type OnboardingStart struct {
	SessionToken      string   `json:"sessionToken"`
	Step              string   `json:"step"`
	Content           string   `json:"content"`
	Status            string   `json:"status"`
	CapabilityOptions []string `json:"capabilityOptions"`
}

// OnboardingStepRequest is the POST /{token}/step body: an intent plus optional free text
// (for an answer) and optional structured answer overrides (for an edit).
type OnboardingStepRequest struct {
	Intent  string             `json:"intent"`
	Text    string             `json:"text,omitempty"`
	Answers *OnboardingAnswers `json:"answers,omitempty"`
}

// OnboardingAnswers is the structured edit/correction payload (an edit restates facts).
// It mirrors the subset of onboarding.Answers a wizard form edits; empty fields are
// ignored by the session's replace semantics.
type OnboardingAnswers struct {
	Name           string   `json:"name,omitempty"`
	Role           string   `json:"role,omitempty"`
	Company        string   `json:"company,omitempty"`
	Location       string   `json:"location,omitempty"`
	Lang           string   `json:"lang,omitempty"`
	Timezone       string   `json:"timezone,omitempty"`
	TonePreference string   `json:"tonePreference,omitempty"`
	ResponseLength string   `json:"responseLength,omitempty"`
	Expertise      []string `json:"expertise,omitempty"`
	Stack          []string `json:"stack,omitempty"`
	Projects       []string `json:"projects,omitempty"`
	Goals          []string `json:"goals,omitempty"`
	Interests      []string `json:"interests,omitempty"`
	People         []string `json:"people,omitempty"`
}

// OnboardingStepResponse is the per-step contract (D-03 / RESEARCH §Hard Problem 4):
// {content, step, status, draft?, preferences?}.
type OnboardingStepResponse struct {
	Content     string `json:"content"`
	Step        string `json:"step"`
	Status      string `json:"status"`
	Draft       string `json:"draft,omitempty"`
	Preferences string `json:"preferences,omitempty"`
}

// OnboardingProvisionRequest is the POST /{token}/provision body: the new login email +
// the write-only initial password, the requested capability set (re-validated server-side
// as a subset of the creator's grants with no '*'), and whether to mint a Telegram link.
type OnboardingProvisionRequest struct {
	Email            string   `json:"email"`
	Password         string   `json:"password"`
	SecurityQuestion string   `json:"securityQuestion"`
	SecurityAnswer   string   `json:"securityAnswer"`
	Capabilities     []string `json:"capabilities"`
	LinkTelegram     bool     `json:"linkTelegram"`
}

// OnboardingProvisionResponse is the saga success result: the new identity id, and (when
// LinkTelegram) the Telegram deep-link + a server-rendered scannable QR SVG. The bot
// token is NEVER in either field — only the t.me/<bot>?start=<onboarding-token> URL.
type OnboardingProvisionResponse struct {
	IdentityID string `json:"identityId"`
	DeepLink   string `json:"deepLink,omitempty"`
	QRSVG      string `json:"qrSvg,omitempty"`
}

// OnboardingTelegramStatus is the poll result: whether the minted onboarding token has
// been consumed (the user scanned the deep-link and the channel linked the identity).
type OnboardingTelegramStatus struct {
	Linked bool `json:"linked"`
}

// registerOnboardingRoutes mounts the four onboarding routes on the supplied mux using Go
// 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/ carve-out,
// never a bare /api/ (which would shadow /api/integrations/). The parent-mux mount (the
// capability gate on start+provision) lives in cmd/aura/serve_webui.go.
func (s *Server) registerOnboardingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/onboarding/start", s.handleOnboardingStart)
	mux.HandleFunc("POST /api/onboarding/{sessionToken}/step", s.handleOnboardingStep)
	mux.HandleFunc("POST /api/onboarding/{sessionToken}/provision", s.handleOnboardingProvision)
	mux.HandleFunc("GET /api/onboarding/{sessionToken}/telegram-status", s.handleTelegramStatus)
}

// handleOnboardingStart serves POST /api/onboarding/start (ONBD-01a / D-06): it reads the
// authenticated principal as the creating operator, mints a server-held session, and
// returns the first step + the capability picker options (creator grants minus '*'). The
// capability gate (identity.create) is on the parent-mux mount, so an operator without the
// cap is 403 before this runs and no session is ever created.
func (s *Server) handleOnboardingStart(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		http.Error(w, "onboarding service not configured", http.StatusServiceUnavailable)
		return
	}
	creator, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	start, err := s.onboarding.StartSession(r.Context(), creator)
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, start)
}

// handleOnboardingStep serves POST /api/onboarding/{sessionToken}/step (ONBD-02 / D-03):
// it size-caps + decodes the body, validates the intent enum + field lengths, then makes
// one service Step call. The service loads the session by token (a missing/expired token
// is a sanitized 404), runs the LLM extractor exactly once on a free-text answer, applies
// the intent, and projects {content, step, status, draft?, preferences?}. A terminal
// session is a clean terminal status, NOT a 500.
func (s *Server) handleOnboardingStep(w http.ResponseWriter, r *http.Request) {
	handleOnboardingMutation(s, w, r, validateOnboardingStep, func(ctx context.Context, requester, token string, req OnboardingStepRequest) (OnboardingStepResponse, error) {
		return s.onboarding.Step(ctx, requester, token, req)
	})
}

// handleOnboardingProvision serves POST /api/onboarding/{sessionToken}/provision
// (ONBD-01a/01b): it size-caps + decodes the body, validates email/password/capability
// lengths, then runs the cross-store saga via one service Provision call. The capability
// gate (identity.create) is on the parent-mux mount; the service additionally re-validates
// no-escalation (subset ⊆ creator-grants AND no '*') before any write. The password is
// hashed immediately by the service and never echoed in the response.
func (s *Server) handleOnboardingProvision(w http.ResponseWriter, r *http.Request) {
	handleOnboardingMutation(s, w, r, validateOnboardingProvision, func(ctx context.Context, requester, token string, req OnboardingProvisionRequest) (OnboardingProvisionResponse, error) {
		return s.onboarding.Provision(ctx, requester, token, req)
	})
}

func handleOnboardingMutation[Req any, Resp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	validate func(Req) error,
	call func(context.Context, string, string, Req) (Resp, error),
) {
	var req Req
	token, requester, ok := s.prepareOnboardingMutation(w, r, func() error {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return errors.New("invalid request body")
		}
		return validate(req)
	})
	if !ok {
		return
	}
	resp, err := call(r.Context(), requester, token, req)
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) prepareOnboardingMutation(w http.ResponseWriter, r *http.Request, decodeAndValidate func() error) (string, string, bool) {
	if s.onboarding == nil {
		http.Error(w, "onboarding service not configured", http.StatusServiceUnavailable)
		return "", "", false
	}
	token := r.PathValue("sessionToken")
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	if err := decodeAndValidate(); err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return "", "", false
	}
	requester, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", "", false
	}
	return token, requester, true
}

// handleTelegramStatus serves GET /api/onboarding/{sessionToken}/telegram-status
// (ONBD-01b / R6): a REST poll (NOT SSE) over PendingConsumed reporting whether the user
// scanned the deep-link and linked Telegram. A missing/expired session is a sanitized 404.
func (s *Server) handleTelegramStatus(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		http.Error(w, "onboarding service not configured", http.StatusServiceUnavailable)
		return
	}
	token := r.PathValue("sessionToken")
	requester, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	status, err := s.onboarding.TelegramStatus(r.Context(), requester, token)
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, status)
}

// validateOnboardingStep enforces the step body contract before it reaches the service: a
// known intent and bounded text/answer fields. It returns a sanitized error so a 400 body
// leaks nothing.
func validateOnboardingStep(req OnboardingStepRequest) error {
	if !validOnboardingIntents[req.Intent] {
		return errors.New("onboarding: unknown intent")
	}
	if len(req.Text) > onboardingTextMaxLen {
		return errors.New("onboarding: text too long")
	}
	return nil
}

// validateOnboardingProvision enforces the provision body contract: a non-empty bounded
// email + password, and a bounded capability set with bounded names. The subset-of-creator
// and no-'*' checks are the service's job (they need the creator's grants); this is the
// fail-fast length/shape front door.
func validateOnboardingProvision(req OnboardingProvisionRequest) error {
	if strings.TrimSpace(req.Email) == "" || len(req.Email) > onboardingEmailMaxLen {
		return errors.New("onboarding: email is required and must be a sane length")
	}
	if req.Password == "" || len(req.Password) > onboardingPasswordMaxLen {
		return errors.New("onboarding: password is required and must be a sane length")
	}
	if strings.TrimSpace(req.SecurityQuestion) == "" || len(req.SecurityQuestion) > onboardingSecurityQuestionMaxLen {
		return errors.New("onboarding: security question is required and must be a sane length")
	}
	if strings.TrimSpace(req.SecurityAnswer) == "" || len(req.SecurityAnswer) > onboardingSecurityAnswerMaxLen {
		return errors.New("onboarding: security answer is required and must be a sane length")
	}
	if !req.LinkTelegram {
		return errors.New("onboarding: Telegram link is required for recovery")
	}
	if len(req.Capabilities) > onboardingMaxCaps {
		return errors.New("onboarding: too many capabilities")
	}
	for _, c := range req.Capabilities {
		if len(c) > onboardingCapNameMaxLen {
			return errors.New("onboarding: capability name too long")
		}
	}
	return nil
}

// writeOnboardingError maps a service error onto the right HTTP status with a sanitized
// body (never echoing a secret-bearing internal error verbatim): an unknown/expired
// session → 404, a no-escalation rejection → 400, a duplicate identity/email → 409, a
// missing-capability re-check → 403, anything else → a sanitized 502 (a backend/saga
// failure is not the client's fault). The typed sentinels are declared in
// onboarding_service.go alongside the service that returns them.
func (s *Server) writeOnboardingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errOnboardingSessionNotFound):
		http.Error(w, "onboarding session not found", http.StatusNotFound)
	case errors.Is(err, ErrOnboardingEscalation):
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
	case errors.Is(err, ErrOnboardingDuplicate):
		http.Error(w, "identity already exists", http.StatusConflict)
	case errors.Is(err, errOnboardingForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
	}
}
