// Package chatgptplan keeps ChatGPT OAuth sessions isolated by Aura identity.
package chatgptplan

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/secret"
)

const (
	issuer        = "https://auth.openai.com"
	resource      = "https://api.openai.com/v1"
	dynamicClient = "dynamic_agent_client"
	planScope     = "chatgpt.tokens.use.direct"
	flowTTL       = 10 * time.Minute
)

var (
	// ErrIdentityRequired prevents a credential lookup without its owning principal.
	ErrIdentityRequired = errors.New("chatgpt plan: an Aura identity is required")
	// ErrAuthorizationRequired directs expired or revoked sessions back through login.
	ErrAuthorizationRequired = errors.New("chatgpt plan: sign in with ChatGPT to continue")
	// ErrPlanDisabled distinguishes an identity login from permission to use its plan.
	ErrPlanDisabled = errors.New("chatgpt plan: ChatGPT plan usage was not granted")
	// ErrInvalidCallback deliberately does not reveal whether a state ever existed.
	ErrInvalidCallback = errors.New("chatgpt plan: invalid or expired authorization callback")
	// ErrRevocationUnconfirmed means local tokens were cleared despite remote failure.
	ErrRevocationUnconfirmed = errors.New("chatgpt plan: disconnected locally; remote revocation was not confirmed, disconnect Aura in ChatGPT Settings")
)

// Flow carries only the transient browser navigation and its progress.
type Flow struct {
	AuthURL string `json:"auth_url"`
	Status  string `json:"status"`
}

// Status separates successful identity login from granted ChatGPT plan permission.
type Status struct {
	Connected   bool   `json:"connected"`
	Email       string `json:"email,omitempty"`
	PlanEnabled bool   `json:"plan_enabled"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

type pending struct {
	owner, state, nonce, verifier, redirectURI, clientID, subject string
	flow                                                          Flow
	authorizationURL                                              string
	expires                                                       time.Time
	err                                                           string
}

// Service binds callbacks to their initiating owner and serializes rotating credentials.
type Service struct {
	mu                        sync.Mutex
	dir, masterSecret, hostID string
	endpoint                  endpoints
	client                    *http.Client
	now                       func() time.Time
	flows                     map[string]*pending
}

// New creates protected storage beneath the supplied Aura data directory.
func New(dir, authulaSecret string) (*Service, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("chatgpt plan: credential directory is required")
	}
	if _, err := secret.NewSealer(authulaSecret, "aura-chatgpt-plan-v1"); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(filepath.Join(dir, "chatgpt-plan"))
	if err != nil {
		return nil, err
	}
	if err = privateDirectory(dir); err != nil {
		return nil, err
	}
	s := &Service{dir: dir, masterSecret: authulaSecret, endpoint: productionEndpoints(), client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now, flows: make(map[string]*pending)}
	release, err := s.lockStore(context.Background())
	if err != nil {
		return nil, err
	}
	defer release()
	if s.hostID, err = loadHostID(dir); err != nil {
		return nil, err
	}
	return s, nil
}

// Start retains a pending flow across request boundaries and reuses saved registrations.
func (s *Service) Start(ctx context.Context, owner, redirectURI string) (Flow, error) {
	if err := checkOwner(ctx, owner); err != nil {
		return Flow{}, err
	}
	if !validRedirect(redirectURI) {
		return Flow{}, errors.New("chatgpt plan: callback must be http://127.0.0.1:<port>/auth/callback")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireFlows()
	if p := s.flows[owner]; p != nil && p.flow.Status == "authorization_required" && p.redirectURI == redirectURI {
		return p.flow, nil
	}
	release, err := s.lockStore(ctx)
	if err != nil {
		return Flow{}, err
	}
	defer release()
	c, err := s.load(owner)
	if err != nil {
		return Flow{}, err
	}
	p := &pending{owner: owner, state: rand.Text(), nonce: rand.Text(), verifier: oauth2.GenerateVerifier(), redirectURI: redirectURI, clientID: dynamicClient, expires: s.now().Add(flowTTL)}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(p.verifier), oauth2.SetAuthURLParam("nonce", p.nonce), oauth2.SetAuthURLParam("resource", resource), oauth2.SetAuthURLParam("ext_agent_host_id", s.hostID)}
	if c != nil {
		p.clientID, p.subject = c.ClientID, c.Subject
		if !slices.Contains(c.Scopes, planScope) {
			// Re-enabling a declined plan grant must show consent; routine login
			// with an already-authorized plan must not force the consent screen.
			// https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery
			opts = append(opts, oauth2.SetAuthURLParam("prompt", "consent"))
		}
		if c.IDToken != "" {
			opts = append(opts, oauth2.SetAuthURLParam("id_token_hint", c.IDToken))
		}
		if c.Email != "" {
			opts = append(opts, oauth2.SetAuthURLParam("login_hint", c.Email))
		}
	} else if previous := s.flows[owner]; previous != nil && previous.clientID != dynamicClient && previous.clientID != "" {
		// A failed code exchange consumes the code, but its issued client is still
		// needed for a fresh attempt rather than a second dynamic registration.
		// https://developers.openai.com/siwc/token-sharing-open-source/sign-in
		p.clientID = previous.clientID
	} else {
		opts = append(opts, oauth2.SetAuthURLParam("agent_name_hint", "Aura"))
	}
	cfg := s.oauthConfig(p.clientID, redirectURI)
	p.flow = Flow{AuthURL: cfg.AuthCodeURL(p.state, opts...), Status: "authorization_required"}
	p.authorizationURL = p.flow.AuthURL
	s.flows[owner] = p
	return p.flow, nil
}

// Status never returns credentials or infers plan permission from account identity.
func (s *Service) Status(ctx context.Context, owner string) (Status, error) {
	if err := checkOwner(ctx, owner); err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireFlows()
	release, err := s.lockStore(ctx)
	if err != nil {
		return Status{}, err
	}
	defer release()
	c, err := s.load(owner)
	if err != nil {
		return Status{}, err
	}
	status := Status{Status: "disconnected"}
	if c != nil {
		status.Email = c.Email
		status.Connected = c.AccessToken != "" && (c.ExpiresAt.After(s.now()) || c.RefreshToken != "")
		status.PlanEnabled = status.Connected && slices.Contains(c.Scopes, planScope)
		if status.Connected {
			status.Status = "approved"
		}
	}
	if p := s.flows[owner]; p != nil {
		status.Status, status.Error = p.flow.Status, p.err
	}
	return status, nil
}

// AccessToken requires the request identity and persists refresh rotation before use.
func (s *Service) AccessToken(ctx context.Context) (string, error) {
	owner := identityctx.IdentityID(ctx)
	if err := checkOwner(ctx, owner); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.lockStore(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	c, err := s.load(owner)
	if err != nil {
		return "", err
	}
	if c == nil || c.AccessToken == "" {
		return "", ErrAuthorizationRequired
	}
	if !slices.Contains(c.Scopes, planScope) {
		return "", ErrPlanDisabled
	}
	if !c.ExpiresAt.After(s.now().Add(time.Minute)) {
		if err = s.refresh(ctx, c); err != nil {
			return "", err
		}
	}
	return c.AccessToken, nil
}

func (s *Service) expireFlows() {
	for owner, p := range s.flows {
		if !p.expires.After(s.now()) {
			delete(s.flows, owner)
		}
	}
}

func checkOwner(ctx context.Context, owner string) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(owner) != owner {
		return ErrIdentityRequired
	}
	if scoped := identityctx.IdentityID(ctx); scoped != "" && scoped != owner {
		return ErrIdentityRequired
	}
	return ctx.Err()
}

func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/auth/callback" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535
}

func validHostID(id string) bool {
	parsed, err := uuid.Parse(strings.TrimPrefix(id, "urn:uuid:"))
	return strings.HasPrefix(id, "urn:uuid:") && err == nil && parsed != uuid.Nil
}
