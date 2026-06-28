package agui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/profile"
)

// wildcardCapability is the system-managed match-all grant ('*', identity.Wildcard). The
// D-06 capability picker NEVER offers it and the server NEVER lets it be requested (no-
// escalation). Declared locally so this file does not import internal/identity just for the
// constant string.
const wildcardCapability = "*"

// onboarding_session.go is the server-held onboarding session store (ONBD-02 / D-03 /
// RESEARCH §Hard Problem 4): the 5-step LoopAgent runs over REST, so the
// onboarding.Session — which carries the accumulated, LLM-extracted Answers + the
// DraftAgentMD — MUST live server-side, keyed by an opaque sessionToken, across the
// stateless step POSTs. A replayed step therefore does NOT re-invoke the LLM (the
// session has already advanced its Step pointer + flipped its prompted latch).
//
// The store mirrors internal/skills/loader.go's goroutine-free TTL discipline
// (loader.go:17,113-127): a mutex-guarded map with a per-entry idle TTL swept LAZILY on
// access — NO background goroutine, so goleak stays clean ([[feedback_minipc_cpu_budget]]:
// never a busy-loop on the shared mini-PC). Every get/put refreshes the accessed entry's
// deadline and opportunistically drops expired siblings.
//
// The session never holds a secret: the Authula password arrives in the provision
// request body (hashed immediately, never persisted), so the entry carries only the
// interview state + the creator id + the offered capability options.

// defaultOnboardingSessionTTL is the onboarding session idle window (RESEARCH §Hard Problem 4): an
// abandoned wizard's session expires and is swept on the next access, and — because the
// cross-store saga only runs at the final provision confirm — an expired session means
// the saga never ran, so an abandoned wizard leaves ZERO rows (the orphan-free property).
const defaultOnboardingSessionTTL = 15 * time.Minute

// sessionTokenBytes is the entropy of the opaque sessionToken (256 bits, hex-encoded).
// The token is unguessable so a session cannot be hijacked by token enumeration; it is
// distinct from (and never derived from) the Authula session cookie.
const sessionTokenBytes = 32

// sessionEntry is one live wizard's server-held state: the wrapped onboarding.Session
// (the interview state machine carrying Answers + DraftAgentMD), the creating operator's
// identity id (the D-06 subset-of-creator re-validation + the audit actor), the
// capability options offered at start (the creator's grants minus '*'), the
// link-Telegram intent, and the idle-expiry deadline.
type sessionEntry struct {
	mu                sync.Mutex
	session           *onboarding.Session
	creatorIdentityID string
	capabilityOptions []string
	linkTelegram      bool
	provisioned       bool
	// onboardingToken is the Telegram mint token assigned by Provision (Leg C). The
	// telegram-status poll reads it back from the same server-held entry to check
	// PendingConsumed. Empty until provisioning mints it.
	onboardingToken string
	expiresAt       time.Time
}

// sessionStore is a goroutine-free, mutex-guarded onboarding-session store with a per-
// entry idle TTL swept lazily on access (mirrors skills.Loader — NO background goroutine,
// goleak-clean). now is injectable so tests can drive expiry deterministically.
type sessionStore struct {
	mu      sync.Mutex
	entries map[string]*sessionEntry
	ttl     time.Duration
	now     func() time.Time
}

// newSessionStore builds a session store with the given idle TTL (a non-positive ttl
// falls back to defaultOnboardingSessionTTL) and the default wall clock.
func newSessionStore(ttl time.Duration) *sessionStore {
	if ttl <= 0 {
		ttl = defaultOnboardingSessionTTL
	}
	return &sessionStore{
		entries: make(map[string]*sessionEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// newSessionToken mints an opaque, unguessable session token (256 bits of crypto/rand,
// hex-encoded). A rand failure is surfaced so the caller fails the start request rather
// than minting a weak token.
func newSessionToken() (string, error) {
	var b [sessionTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// put stores a fresh entry under a newly-minted token, setting its idle deadline, and
// sweeps expired siblings while the lock is held (lazy GC, no goroutine). It returns the
// opaque token the client carries on subsequent step/provision calls.
func (st *sessionStore) put(e *sessionEntry) (string, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	e.expiresAt = st.now().Add(st.ttl)
	st.entries[token] = e
	return token, nil
}

// get returns the live entry for a token, refreshing its idle deadline (the 15-min TTL is
// per-step idle, not absolute). A missing OR expired token returns (nil, false) — and an
// expired one is dropped — so a stale/abandoned session is indistinguishable from an
// unknown token to the caller (a clean sanitized 404/expired at the handler). The lazy
// sweep also runs here so expired siblings are reclaimed without a background goroutine.
func (st *sessionStore) get(token string) (*sessionEntry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	e, ok := st.entries[token]
	if !ok {
		return nil, false
	}
	if !st.now().Before(e.expiresAt) {
		delete(st.entries, token)
		return nil, false
	}
	e.expiresAt = st.now().Add(st.ttl)
	return e, true
}

// sweepLocked drops every entry past its idle deadline. The caller holds st.mu. This is
// the lazy GC that replaces a background reaper (goleak-clean); it runs on each put/get so
// an abandoned wizard's entry is reclaimed on the next access of ANY session.
func (st *sessionStore) sweepLocked() {
	now := st.now()
	for token, e := range st.entries {
		if !now.Before(e.expiresAt) {
			delete(st.entries, token)
		}
	}
}

// len reports the number of live (un-swept) entries — a test-support accessor that also
// triggers a sweep so expiry assertions observe the reclaimed count.
func (st *sessionStore) len() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	return len(st.entries)
}

// ---------------------------------------------------------------------------
// OnboardingService implementation (the interview side: StartSession + Step).
// The provisioning saga (Provision/TelegramStatus) lives in onboarding_provision.go.
// ---------------------------------------------------------------------------

// Typed sentinels the handlers (onboarding_api.go) map onto HTTP status codes. They are
// declared here, alongside the service that returns them, so the error contract is one
// source of truth. ErrOnboardingSessionNotFound → 404 (missing/expired token);
// ErrOnboardingEscalation → 400 (no-escalation rejection); ErrOnboardingDuplicate → 409
// (duplicate identity/email); ErrOnboardingForbidden → 403 (missing-capability re-check).
// The duplicate + escalation sentinels are EXPORTED so the composition-root aura-leg
// adapter (cmd/aura/serve_onboarding.go) can return them from the tx and the saga's
// errors.Is matches; the others are package-internal.
var (
	errOnboardingSessionNotFound = errors.New("onboarding: session not found or expired")
	// ErrOnboardingEscalation is returned (and recognized via errors.Is) when a requested
	// capability would escalate privilege ('*' or a cap the creator lacks).
	ErrOnboardingEscalation = errors.New("onboarding: capability escalation rejected")
	// ErrOnboardingDuplicate is returned when the new identity name/email already exists
	// (the aura.identities NOT NULL UNIQUE name → 23505 → a clean 409, idempotent double-
	// submit yields one identity).
	ErrOnboardingDuplicate = errors.New("onboarding: identity already exists")
	errOnboardingForbidden = errors.New("onboarding: missing required capability")
)

// AnswerExtractor is the consumer-side narrow port for the one-shot per-answer LLM
// extraction (ONBD-02). *onboarding.LLMAnswerExtractor satisfies it. It is called EXACTLY
// once per inbound free-text answer; replay/edit never call it (the no-duplicate-LLM-turn
// guarantee — RESEARCH §Hard Problem 4). Declared as an interface so tests inject a
// call-counting fake and the composition root wires the real extractor.
type AnswerExtractor interface {
	Extract(ctx context.Context, step onboarding.Step, raw string) (onboarding.Answers, error)
}

// ProfileWriter is the consumer-side narrow port for persisting the confirmed Agent.md to
// ~/.aura/agents/<id>/ (ONBD-02). *profile.Store satisfies it. The write happens at
// provision time (when the new identity id exists), reading the session's accumulated
// DraftAgentMD; a skipped interview writes nothing.
type ProfileWriter interface {
	WriteProfile(identity string, p profile.Profile) error
}

type RecoverySetupWriter interface {
	UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error
}

// onboardingService is the concrete OnboardingService: the goroutine-free TTL session
// store + the interview driver (StartSession/Step) + the provisioning saga (Provision/
// TelegramStatus, onboarding_provision.go). It is built at the composition root
// (cmd/aura/serve.go) over the daemon's existing seams and wired via SetOnboardingService.
// Each dependency is a narrow consumer-side port so the service is unit-testable with
// fakes and the agui package stays free of the telegram import (which would cycle, since
// internal/channels/telegram imports internal/agui).
type onboardingService struct {
	sessions  *sessionStore
	caps      CapabilitySource
	extractor AnswerExtractor
	profiles  ProfileWriter

	// provisioning ports (onboarding_provision.go): the Authula core, the atomic aura-leg
	// writer + its compensation, the Telegram mint/poll, and the deep-link bot username.
	authula  AuthulaCore
	auraLeg  AuraLegWriter
	telegram TelegramMint
	botName  string
	recovery RecoverySetupWriter
}

// OnboardingDeps bundles the narrow ports the composition root (cmd/aura/serve.go) wires
// into the service via NewOnboardingService. Any provisioning port may be nil for an
// interview-only deployment, in which case Provision answers a sanitized error; the
// interview side (StartSession/Step) needs only Capabilities + Extractor + Profiles.
type OnboardingDeps struct {
	TTL          time.Duration
	Capabilities CapabilitySource
	Extractor    AnswerExtractor
	Profiles     ProfileWriter
	Authula      AuthulaCore
	AuraLeg      AuraLegWriter
	Telegram     TelegramMint
	BotUsername  string
	Recovery     RecoverySetupWriter
}

// NewOnboardingService assembles the OnboardingService over the supplied narrow ports.
// Exported so the daemon composition root can build it and wire it via
// SetOnboardingService; the ports keep the agui package free of the telegram import (which
// would cycle).
func NewOnboardingService(d OnboardingDeps) OnboardingService {
	return newOnboardingService(d)
}

// newOnboardingService assembles the concrete service over the supplied narrow ports.
func newOnboardingService(d OnboardingDeps) *onboardingService {
	return &onboardingService{
		sessions:  newSessionStore(d.TTL),
		caps:      d.Capabilities,
		extractor: d.Extractor,
		profiles:  d.Profiles,
		authula:   d.Authula,
		auraLeg:   d.AuraLeg,
		telegram:  d.Telegram,
		botName:   d.BotUsername,
		recovery:  d.Recovery,
	}
}

// StartSession mints a server-held onboarding session for the creating operator and
// returns the first step + the D-06 capability picker options: the creator's OWN grants
// with the '*' wildcard excluded (ONBD-01a). An operator holding only '*' offers an empty
// picker — they cannot grant a capability they do not hold by name (no-escalation). The
// capability gate (identity.create) is enforced on the route mount, so reaching here means
// the creator is authorized to create identities.
func (s *onboardingService) StartSession(ctx context.Context, creatorIdentityID string) (OnboardingStart, error) {
	if creatorIdentityID == "" {
		return OnboardingStart{}, errOnboardingForbidden
	}
	grants, err := s.caps.ListCapabilities(ctx, creatorIdentityID)
	if err != nil {
		return OnboardingStart{}, err
	}
	options := filterWildcard(grants)
	// The session is for the identity being provisioned; its id is assigned at provision
	// (the saga creates the row). Until then the session carries a neutral placeholder name
	// and accumulates the interview Answers + the creator id.
	entry := &sessionEntry{
		session:           onboarding.NewSession("", "new-identity"),
		creatorIdentityID: creatorIdentityID,
		capabilityOptions: options,
	}
	// Prime the first prompt so the wizard renders the identity step immediately. Restart
	// on a fresh session is a full reset that returns the StepIdentity question and leaves
	// the session at StepIdentity/StatusActive (session.go restart→question(StepIdentity)).
	first, err := entry.session.Apply(onboarding.Input{Intent: onboarding.IntentRestart})
	if err != nil {
		return OnboardingStart{}, err
	}
	token, err := s.sessions.put(entry)
	if err != nil {
		return OnboardingStart{}, err
	}
	return OnboardingStart{
		SessionToken:      token,
		Step:              string(entry.session.Step),
		Content:           first.Content,
		Status:            string(entry.session.Status),
		CapabilityOptions: options,
	}, nil
}

func (s *onboardingService) sessionForRequester(token, requesterIdentityID string) (*sessionEntry, error) {
	if requesterIdentityID == "" {
		return nil, errOnboardingForbidden
	}
	entry, ok := s.sessions.get(token)
	if !ok {
		return nil, errOnboardingSessionNotFound
	}
	if entry.creatorIdentityID != requesterIdentityID {
		return nil, errOnboardingForbidden
	}
	return entry, nil
}

// Step applies one onboarding intent to the server-held session and returns the per-step
// contract. On an answer carrying free text it runs the LLM extractor EXACTLY once, merges
// the structured facts into the Input, and applies it (advancing the step). On edit it
// passes the structured answer overrides straight through (NO extraction, NO re-prompt —
// the session re-renders the draft deterministically via ExtractDraft). On skip/confirm it
// applies directly. A terminal session yields a clean terminal status (ErrTerminal handled,
// not a 500). An empty answer is recorded empty without error (the session's mergeAnswers
// no-ops on empty).
func (s *onboardingService) Step(ctx context.Context, requesterIdentityID, token string, in OnboardingStepRequest) (OnboardingStepResponse, error) {
	entry, err := s.sessionForRequester(token, requesterIdentityID)
	if err != nil {
		return OnboardingStepResponse{}, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.provisioned {
		return OnboardingStepResponse{}, errOnboardingSessionNotFound
	}

	input := onboarding.Input{Intent: onboarding.Intent(in.Intent)}
	switch in.Intent {
	case "answer":
		// EXACTLY ONE LLM extraction per inbound free-text answer (RESEARCH §Hard Problem
		// 4). A blank answer is recorded empty (no extraction needed). The extractor never
		// errors (raw-text fallback), so a transport hiccup still advances the interview.
		if strings.TrimSpace(in.Text) != "" && s.extractor != nil {
			extracted, err := s.extractor.Extract(ctx, entry.session.Step, in.Text)
			if err != nil {
				return OnboardingStepResponse{}, err
			}
			input.Answers = extracted
		}
		input.Text = in.Text
	case "edit":
		// An edit restates facts: pass the structured overrides through; the session
		// re-renders the draft from the SAME accumulated Answers via ExtractDraft — no
		// per-answer LLM turn, no re-prompt.
		if in.Answers != nil {
			input.Answers = in.Answers.toOnboarding()
		}
	case "skip", "confirm":
		// Applied directly; no extraction.
	}

	transition, err := entry.session.Apply(input)
	if err != nil {
		// A terminal session is a clean terminal status, not a server error.
		if errors.Is(err, onboarding.ErrTerminal) {
			return s.projectStep(entry, transition), nil
		}
		// ErrDraftRequired / ErrInvalidIntent are client mistakes → escalation-class 400
		// is wrong; surface as a sanitized 400 via the escalation sentinel's sibling. Use
		// a plain sanitized error so the handler renders a 502-safe message; but a
		// draft-required confirm is really a 400. Map both to the escalation 400 path.
		if errors.Is(err, onboarding.ErrDraftRequired) || errors.Is(err, onboarding.ErrInvalidIntent) {
			return OnboardingStepResponse{}, ErrOnboardingEscalation
		}
		return OnboardingStepResponse{}, err
	}
	return s.projectStep(entry, transition), nil
}

// projectStep renders the per-step response from the live session + the transition. The
// draft/preferences are surfaced once a draft exists (the draft + review steps).
func (s *onboardingService) projectStep(entry *sessionEntry, t onboarding.Transition) OnboardingStepResponse {
	resp := OnboardingStepResponse{
		Content: t.Content,
		Step:    string(entry.session.Step),
		Status:  string(entry.session.Status),
	}
	if strings.TrimSpace(entry.session.DraftAgentMD) != "" {
		resp.Draft = entry.session.DraftAgentMD
		resp.Preferences = marshalPreferences(entry.session.Preferences)
	}
	return resp
}

// marshalPreferences renders the session's structured preferences as a JSON string for the
// step response (an encode failure yields "{}", never a partial body).
func marshalPreferences(p profile.Preferences) string {
	raw, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// toOnboarding maps the wire edit payload onto onboarding.Answers (empty fields ignored by
// the session's replace semantics).
func (a OnboardingAnswers) toOnboarding() onboarding.Answers {
	return onboarding.Answers{
		Name:           a.Name,
		Role:           a.Role,
		Company:        a.Company,
		Location:       a.Location,
		Lang:           a.Lang,
		Timezone:       a.Timezone,
		TonePreference: a.TonePreference,
		ResponseLength: a.ResponseLength,
		Expertise:      a.Expertise,
		Stack:          a.Stack,
		Projects:       a.Projects,
		Goals:          a.Goals,
		Interests:      a.Interests,
		People:         a.People,
	}
}

// filterWildcard returns grants with the '*' wildcard removed (the D-06 picker never
// offers '*'; the order is preserved). A nil/empty input yields a non-nil empty slice so
// the JSON picker is `[]`, never null.
func filterWildcard(grants []string) []string {
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if g == wildcardCapability {
			continue
		}
		out = append(out, g)
	}
	return out
}
