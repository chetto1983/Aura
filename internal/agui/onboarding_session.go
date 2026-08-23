package agui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/onboarding"
)

// wildcardCapability is the system-managed match-all grant ('*', identity.Wildcard). The
// D-06 capability picker NEVER offers it and the server NEVER lets it be requested (no-
// escalation). Declared locally so this file does not import internal/identity just for the
// constant string.
const wildcardCapability = "*"

// onboarding_session.go is the server-held onboarding session store (ONBD-01): the
// identity-provisioning wizard spans several stateless POSTs (start → provision →
// telegram-status poll), so the creator id, the capability options offered at start and
// the minted Telegram token MUST live server-side, keyed by an opaque sessionToken.
//
// The store mirrors internal/skills/loader.go's goroutine-free TTL discipline
// (loader.go:17,113-127): a mutex-guarded map with a per-entry idle TTL swept LAZILY on
// access — NO background goroutine, so goleak stays clean ([[feedback_minipc_cpu_budget]]:
// never a busy-loop on the shared mini-PC). Every get/put refreshes the accessed entry's
// deadline and opportunistically drops expired siblings.
//
// The session never holds a secret: the Authula password arrives in the provision
// request body (hashed immediately, never persisted), so the entry carries only the
// creator id + the offered capability options + the mint token.

// defaultOnboardingSessionTTL is the onboarding session idle window (RESEARCH §Hard Problem 4): an
// abandoned wizard's session expires and is swept on the next access, and — because the
// cross-store saga only runs at the final provision confirm — an expired session means
// the saga never ran, so an abandoned wizard leaves ZERO rows (the orphan-free property).
const defaultOnboardingSessionTTL = 15 * time.Minute

// sessionTokenBytes is the entropy of the opaque sessionToken (256 bits, hex-encoded).
// The token is unguessable so a session cannot be hijacked by token enumeration; it is
// distinct from (and never derived from) the Authula session cookie.
const sessionTokenBytes = 32

// sessionEntry is one live wizard's server-held state: the creating operator's identity
// id (the D-06 subset-of-creator re-validation + the audit actor), the capability options
// offered at start (the creator's grants minus '*'), the provisioned flag, Telegram
// onboarding token, and the idle-expiry deadline.
type sessionEntry struct {
	mu                sync.Mutex
	creatorIdentityID string
	capabilityOptions []string
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

// ---------------------------------------------------------------------------
// OnboardingService implementation (the seed side: StartSession + the seed mapper).
// The provisioning saga (Provision/TelegramStatus) and SubmitProfile live in
// onboarding_provision.go.
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

// RecoverySetupWriter persists the recovery challenge into aura.identity_recovery during
// provisioning, after the identity exists and before Telegram minting.
type RecoverySetupWriter interface {
	UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error
}

// onboardingService is the concrete OnboardingService: the goroutine-free TTL session
// store + the seed write (SubmitProfile) + the provisioning saga (Provision/
// TelegramStatus, onboarding_provision.go). It is built at the composition root
// (cmd/aura/serve.go) over the daemon's existing seams and wired via SetOnboardingService.
// Each dependency is a narrow consumer-side port so the service is unit-testable with
// fakes and the agui package stays free of the telegram import (which would cycle, since
// internal/channels/telegram imports internal/agui).
type onboardingService struct {
	sessions *sessionStore
	caps     CapabilitySource
	profiles onboarding.Store

	// provisioning ports (onboarding_provision.go): the Authula core, the atomic aura-leg
	// writer + its compensation, recovery challenge writer, Telegram mint/poll/compensation,
	// and the deep-link bot username.
	authula  AuthulaCore
	auraLeg  AuraLegWriter
	telegram TelegramMint
	botName  string
	// botNameResolver, when set, resolves the CURRENT bot username from the effective
	// TELEGRAM_BOT_TOKEN (settings store → env, via getMe) so an operator who saves a token
	// mid-session gets a working deep-link WITHOUT a daemon restart (the sentinel/refresh
	// pattern). A nil resolver or an empty result falls back to the boot-frozen botName.
	botNameResolver func(context.Context) string
	recovery        RecoverySetupWriter

	// Phase-36 resource legs + journaling (onboarding_provision_resources.go /
	// saga_journal.go): the forward-recovery journal, the per-identity Garage bucket/key
	// leg, and the per-identity filesystem-roots leg. All OPTIONAL (nil disables that leg /
	// journaling) so the pre-cutover + seed-only + unit-test paths are unchanged.
	journal     SagaJournal
	objectStore ObjectStoreProvisioner
	filesystem  FilesystemProvisioner

	// musrIsolation is the deployment's declaration that it is fit to host more than one
	// identity (AURA_MUSR_ISOLATION). The saga ONLY ever creates ADDITIONAL, non-local
	// identities (the operator/local is bootstrap-seeded, migration 0004). When false,
	// Provision refuses (errIsolationDisabled) BEFORE any cross-store write, because the
	// planes listed there are shared deployment-wide.
	musrIsolation bool
}

// OnboardingDeps bundles the narrow ports the composition root (cmd/aura/serve.go) wires
// into the service via NewOnboardingService. The provisioning side requires Authula,
// AuraLeg, Recovery, Telegram, and BotUsername before it writes; the seed side
// (StartSession/SubmitProfile) needs only Capabilities + Profiles.
type OnboardingDeps struct {
	TTL          time.Duration
	Capabilities CapabilitySource
	Profiles     onboarding.Store
	Authula      AuthulaCore
	AuraLeg      AuraLegWriter
	Telegram     TelegramMint
	BotUsername  string
	// BotUsernameResolver resolves the CURRENT bot username live (settings-store token →
	// getMe), so a token saved mid-session yields a working deep-link without a restart. When
	// nil or returning "", the service falls back to the boot-resolved BotUsername.
	BotUsernameResolver func(context.Context) string
	Recovery            RecoverySetupWriter
	// Phase-36 provisioning saga extensions (all optional): the forward-recovery journal
	// (D-14/D-27), the per-identity Garage bucket/key leg (D-08), and the per-identity
	// filesystem-roots leg (D-20/D-21).
	Journal     SagaJournal
	ObjectStore ObjectStoreProvisioner
	Filesystem  FilesystemProvisioner
	// MUSRIsolation declares the deployment fit to host more than one identity
	// (AURA_MUSR_ISOLATION). Provision REFUSES while it is false. The composition root
	// wires it from config.MUSRIsolation.
	MUSRIsolation bool
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
		sessions:        newSessionStore(d.TTL),
		caps:            d.Capabilities,
		profiles:        d.Profiles,
		authula:         d.Authula,
		auraLeg:         d.AuraLeg,
		telegram:        d.Telegram,
		botName:         d.BotUsername,
		botNameResolver: d.BotUsernameResolver,
		recovery:        d.Recovery,
		journal:         d.Journal,
		objectStore:     d.ObjectStore,
		filesystem:      d.Filesystem,
		musrIsolation:   d.MUSRIsolation,
	}
}

// StartSession mints a server-held onboarding session for the creating operator and
// returns the D-06 capability picker options: the creator's OWN grants with the '*'
// wildcard excluded (ONBD-01a). A wildcard creator may grant any named capability through
// the service backstop, but never '*' itself; the picker still omits '*' because it is
// system-managed. The capability gate (identity.create) is enforced on the route mount, so
// reaching here means the creator is authorized to create identities.
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
	// (the saga creates the row). Until then the entry carries only the creator id and the
	// offered options — the profile seed arrives with the provision body.
	token, err := s.sessions.put(&sessionEntry{
		creatorIdentityID: creatorIdentityID,
		capabilityOptions: options,
	})
	if err != nil {
		return OnboardingStart{}, err
	}
	return OnboardingStart{SessionToken: token, CapabilityOptions: options}, nil
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

// toAnswers maps the typed seed form onto onboarding.Answers. It is a straight field
// copy with no normalization: the name stored must be byte-identical to the name typed
// (Amendment #95's whole reason for existing), so nothing here trims, folds or rewrites.
func (s OnboardingSeed) toAnswers() onboarding.Answers {
	return onboarding.Answers{
		Name:     s.Name,
		Role:     s.Role,
		Company:  s.Company,
		Location: s.Location,
		Lang:     s.Lang,
		Timezone: s.Timezone,
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
