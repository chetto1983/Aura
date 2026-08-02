package agui

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// onboarding_api.go is the thin REST adapter over the server-held OnboardingService
// (ONBD-01/02). Five routes under the /api/ carve-out:
//
//   GET  /api/onboarding/status                         (RequireAuth)
//   POST /api/onboarding/profile                        (RequireAuth — the seed form)
//   POST /api/onboarding/start                          (RequireCapability identity.create)
//   POST /api/onboarding/{sessionToken}/provision       (RequireCapability identity.create)
//   GET  /api/onboarding/{sessionToken}/telegram-status (RequireAuth)
//
// The parent-mux mount (the capability gate on start+provision, the whole-origin
// RequireAuth on all five) lives in cmd/aura/serve_webui.go. There is NO business logic
// here: every handler size-caps the body (MaxBytesReader), decodes→400, validates field
// lengths→sanitized-400, reads the authenticated principal as the creating operator, then
// makes ONE service call. The cross-store saga + compensation, the no-escalation
// re-validation, the QR render, and the no-secret-leak logging all live in the service
// (onboarding_session.go / onboarding_provision.go), mirroring graph_api.go's "handler
// parses, the seam does the work" split.

// onboarding* body/field caps bound the untrusted wizard input before it reaches the
// service (V5 length-cap). Email/password/capability-name lengths are generous but finite
// so a crafted body is a clean 400, not an unbounded allocation. The capability-name
// grammar is re-checked at the identity store (GrantCapability), so these are defense-in-
// depth, not the sole control.
const (
	onboardingEmailMaxLen            = 320 // RFC 5321 max email length
	onboardingPasswordMaxLen         = 1024
	onboardingSecurityQuestionMaxLen = 256
	onboardingSecurityAnswerMaxLen   = 512
	onboardingCapNameMaxLen          = 64 // identity.capNameRe upper bound
	onboardingMaxCaps                = 64
	onboardingSeedFieldMaxLen        = 256
	onboardingLangMaxLen             = 32
	onboardingTimezoneMaxLen         = 64
)

// OnboardingStart is the POST /start response: the opaque session token and the D-06
// capability picker options (the creator's grants with '*' excluded).
type OnboardingStart struct {
	SessionToken      string   `json:"sessionToken"`
	CapabilityOptions []string `json:"capabilityOptions"`
}

// OnboardingSeed is the typed first-run profile form (Amendment #95). It is ONE wire type
// serving both flows — the self-service POST /api/onboarding/profile body and the
// `seed` object provisioning carries for the identity it creates — so there is one
// validator and one mapper. Every field is optional; an entirely blank seed is a skip.
type OnboardingSeed struct {
	Name     string `json:"name,omitempty"`
	Lang     string `json:"lang,omitempty"`
	Location string `json:"location,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Role     string `json:"role,omitempty"`
	Company  string `json:"company,omitempty"`
}

// blank reports whether every field is empty after trimming — the derived skip signal
// (there is no `skip` boolean, so "skip" and "a filled name" cannot both be encoded).
func (s OnboardingSeed) blank() bool {
	return strings.TrimSpace(s.Name) == "" &&
		strings.TrimSpace(s.Lang) == "" &&
		strings.TrimSpace(s.Location) == "" &&
		strings.TrimSpace(s.Timezone) == "" &&
		strings.TrimSpace(s.Role) == "" &&
		strings.TrimSpace(s.Company) == ""
}

// OnboardingProvisionRequest is the POST /{token}/provision body: the new login email +
// the write-only initial password, the requested capability set (re-validated server-side
// as a subset of the creator's grants with no '*'), whether to mint a Telegram link, and
// the optional profile seed written into the NEW identity's graph.
type OnboardingProvisionRequest struct {
	Email            string         `json:"email"`
	Password         string         `json:"password"`
	SecurityQuestion string         `json:"securityQuestion"`
	SecurityAnswer   string         `json:"securityAnswer"`
	Capabilities     []string       `json:"capabilities"`
	LinkTelegram     bool           `json:"linkTelegram"`
	Seed             OnboardingSeed `json:"seed"`
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

// OnboardingTelegramLink is the current-identity Telegram recovery link contract used
// outside the full onboarding wizard, for example from Settings when the operator
// skipped Telegram setup the first time.
type OnboardingTelegramLink struct {
	SessionToken string `json:"sessionToken"`
	DeepLink     string `json:"deepLink,omitempty"`
	QRSVG        string `json:"qrSvg,omitempty"`
}

// OnboardingStatus reports whether the signed-in operator still needs to finish
// the profile onboarding flow.
type OnboardingStatus struct {
	Required  bool `json:"required"`
	Completed bool `json:"completed"`
	Skipped   bool `json:"skipped"`
}

// OnboardingProfileComplete is returned after the seed form is submitted or skipped.
// SessionToken is the freshly-minted Telegram poll session the completion screen feeds to
// GET /api/onboarding/{sessionToken}/telegram-status, so no pre-mint round-trip is needed.
type OnboardingProfileComplete struct {
	Completed    bool   `json:"completed"`
	Skipped      bool   `json:"skipped"`
	SessionToken string `json:"sessionToken,omitempty"`
	DeepLink     string `json:"deepLink,omitempty"`
	QRSVG        string `json:"qrSvg,omitempty"`
}

// OnboardingStatusSource exposes persisted onboarding completion state to the
// authenticated web shell.
type OnboardingStatusSource interface {
	OnboardingStatus(ctx context.Context, identityID string) (OnboardingStatus, error)
}

// SetOnboardingStatusSource wires the onboarding status read model into the
// AG-UI HTTP server.
func (s *Server) SetOnboardingStatusSource(source OnboardingStatusSource) {
	s.onboardingStatus = source
}

// registerOnboardingRoutes mounts the five onboarding routes on the supplied mux using Go
// 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/ carve-out,
// never a bare /api/ (which would shadow /api/integrations/). The parent-mux mount (the
// capability gate on start+provision) lives in cmd/aura/serve_webui.go.
func (s *Server) registerOnboardingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/onboarding/status", s.handleOnboardingStatus)
	mux.HandleFunc("POST /api/onboarding/profile", s.handleProfileSubmit)
	mux.HandleFunc("POST /api/onboarding/start", s.handleOnboardingStart)
	mux.HandleFunc("POST /api/onboarding/{sessionToken}/provision", s.handleOnboardingProvision)
	mux.HandleFunc("GET /api/onboarding/{sessionToken}/telegram-status", s.handleTelegramStatus)
}

func (s *Server) handleOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	if s.onboardingStatus == nil {
		http.Error(w, "onboarding status not configured", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	status, err := s.onboardingStatus.OnboardingStatus(r.Context(), identityID)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, status)
}

// handleProfileSubmit serves POST /api/onboarding/profile (Amendment #95): the whole seed
// form arrives in ONE stateless body — there is no session token, no accumulated state and
// no LLM call. An entirely blank seed is a skip (derived in the service, never a flag).
func (s *Server) handleProfileSubmit(w http.ResponseWriter, r *http.Request) {
	handleOnboardingMutation(s, w, r, validateOnboardingSeed, func(ctx context.Context, requester, _ string, req OnboardingSeed) (OnboardingProfileComplete, error) {
		return s.onboarding.SubmitProfile(ctx, requester, req)
	})
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
		if err := decodeOnboardingBody(w, r, &req); err != nil {
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

func decodeOnboardingBody[Req any](w http.ResponseWriter, r *http.Request, req *Req) error {
	return strictDecodeJSON(w, r, req, decodeOpts{})
}

func (s *Server) prepareOnboardingMutation(w http.ResponseWriter, r *http.Request, decodeAndValidate func() error) (string, string, bool) {
	if s.onboarding == nil {
		http.Error(w, "onboarding service not configured", http.StatusServiceUnavailable)
		return "", "", false
	}
	token := r.PathValue("sessionToken")
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
	handleOnboardingSessionRequest(s, w, r, func(ctx context.Context, requester, token string) (OnboardingTelegramStatus, error) {
		return s.onboarding.TelegramStatus(ctx, requester, token)
	})
}

func handleOnboardingSessionRequest[Resp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	call func(context.Context, string, string) (Resp, error),
) {
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
	resp, err := call(r.Context(), requester, token)
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, resp)
}

// validateOnboardingSeed bounds every seed field before it reaches the mapper, and enforces
// the ONE conditional requirement: a seed that says anything must say WHO. MapProfile keys
// every fact off the name and falls back to the placeholder subject "operator" when it is
// empty, so a nameless-but-filled seed would write `operator works_for PmSync` with no
// PERSON entity for it to resolve against — the orphan/duplicate shape Amendment #95 exists
// to end. An entirely blank seed stays legitimate: it is the skip.
func validateOnboardingSeed(seed OnboardingSeed) error {
	for _, f := range []struct {
		value string
		max   int
	}{
		{seed.Name, onboardingSeedFieldMaxLen},
		{seed.Role, onboardingSeedFieldMaxLen},
		{seed.Company, onboardingSeedFieldMaxLen},
		{seed.Location, onboardingSeedFieldMaxLen},
		{seed.Lang, onboardingLangMaxLen},
		{seed.Timezone, onboardingTimezoneMaxLen},
	} {
		if len(f.value) > f.max {
			return errors.New("onboarding: profile field too long")
		}
	}
	if !seed.blank() && strings.TrimSpace(seed.Name) == "" {
		return errors.New("onboarding: a name is required once any other profile field is filled")
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
	return validateOnboardingSeed(req.Seed)
}

// writeOnboardingError maps a service error onto the right HTTP status with a sanitized
// body (never echoing a secret-bearing internal error verbatim): an unknown/expired
// session → 404, a no-escalation rejection → 400, a duplicate identity/email OR a
// single-identity deployment (AURA_MUSR_ISOLATION off) → 409, a missing-capability re-check
// → 403, anything else → a sanitized 502 (a backend/saga failure is not the client's fault).
// The typed sentinels are declared in onboarding_service.go alongside the service that
// returns them.
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
	case errors.Is(err, errIsolationDisabled):
		// Provisioning is refused on a single-identity deployment. The message is a fixed,
		// secret-free operator guidance string (admins hold identity.create), so surfacing it
		// is safe and actionable — 409 Conflict (the action conflicts with current config).
		http.Error(w, sanitizeErr(err), http.StatusConflict)
	default:
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
	}
}
