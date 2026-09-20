package remotetunnel

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// Reconciliation errors are safe diagnostics and support errors.Is classification.
var (
	ErrOwnershipConflict = errors.New("remote access: resource ownership conflict")
	ErrMemberLockout     = errors.New("remote access: active administrator and nonempty membership required")
	ErrBackoff           = errors.New("remote access: retry backoff active")
	ErrConfiguration     = errors.New("remote access: invalid or incomplete configuration")
	ErrTerminal          = errors.New("remote access: save desired configuration to retry terminal failure")
	ErrRemotePresent     = errors.New("remote access: deletion not yet confirmed")
	errResourceAbsent    = errors.New("remote access: deleted resource confirmed")
)

// Reconciler serializes one appliance's remote operations and intent changes.
type Reconciler struct {
	mu              sync.Mutex
	store           StateStore
	cloud           Cloudflare
	members         Members
	projection      Projection
	credentials     TunnelCredentials
	acceptance      Acceptance
	log             *slog.Logger
	now             func() time.Time
	retryAt         time.Time
	attempts        uint
	retryGeneration int64
}

// Option binds credential authority and independently measured readiness.
type Option func(*Reconciler)

// WithCredentials is required before enabled reconciliation can publish.
func WithCredentials(c TunnelCredentials) Option { return func(r *Reconciler) { r.credentials = c } }

// WithAcceptance permits healthy only after an independently successful probe.
func WithAcceptance(a Acceptance) Option { return func(r *Reconciler) { r.acceptance = a } }

// New constructs one service; callers must share it for all intent mutations.
func New(s StateStore, c Cloudflare, m Members, p Projection, l *slog.Logger, options ...Option) *Reconciler {
	if l == nil {
		l = slog.Default()
	}
	r := &Reconciler{store: s, cloud: c, members: m, projection: p, log: l, now: time.Now}
	for _, o := range options {
		o(r)
	}
	return r
}

// Status reads persisted state without triggering external operations.
func (r *Reconciler) Status(ctx context.Context) (State, error) { return r.store.Load(ctx) }

// SaveDesired prevents concurrent intent changes between creation and ID persistence.
func (r *Reconciler) SaveDesired(ctx context.Context, generation int64, desired Desired, by string) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.SaveDesired(ctx, generation, desired, by)
}

// Reconcile resumes persisted work; terminal errors require an explicit intent save.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	if s.Phase == PhaseError {
		return ErrTerminal
	}
	if s.Generation == r.retryGeneration && r.now().Before(r.retryAt) {
		return ErrBackoff
	}
	if s.Phase == PhaseDeleting {
		return r.delete(ctx, &s)
	}
	if !s.Desired.Enabled {
		return r.disableProjection(ctx, &s)
	}
	err = r.reconcile(ctx, &s)
	if err != nil {
		return r.failed(ctx, &s, err)
	}
	r.attempts = 0
	r.retryAt = time.Time{}
	return nil
}

func (r *Reconciler) reconcile(ctx context.Context, s *State) error {
	if r.credentials == nil || r.projection == nil || r.members == nil {
		return ErrConfiguration
	}
	if err := validateDesired(s.Desired); err != nil {
		return err
	}
	emails, admins, err := r.activeMembers(ctx)
	if err != nil {
		return err
	}
	if _, err = r.cloud.VerifyToken(ctx); err != nil {
		return err
	}
	if err = r.account(ctx, s); err != nil {
		return err
	}
	active, err := r.zone(ctx, s)
	if err != nil || !active {
		return err
	}
	s.Phase = PhaseProvisioning
	s.LastError = ""
	if err = r.persist(ctx, s); err != nil {
		return err
	}
	if err = r.tunnel(ctx, s); err != nil {
		return err
	}
	if err = r.otp(ctx, s); err != nil {
		return err
	}
	if err = r.verifyOwned(ctx, s); err != nil {
		return err
	}
	if err = r.checkAdministrators(ctx, s, admins); err != nil {
		return err
	}
	if err = r.access(ctx, s, emails); err != nil {
		return err
	}
	// Re-read identity state before publication; identity transactions never wait on Cloudflare.
	current, _, err := r.activeMembers(ctx)
	if err != nil {
		return err
	}
	if !sameEmails(emails, current) {
		return ErrMemberLockout
	}
	if err = r.publish(ctx, s); err != nil {
		return err
	}
	token, err := r.cloud.TunnelToken(ctx, s.Resources.AccountID, s.Resources.TunnelID)
	if err != nil {
		return err
	}
	if token.Reveal() == "" {
		return ErrConfiguration
	}
	if err = r.credentials.Save(ctx, token); err != nil {
		return err
	}
	if err = r.current(ctx, s); err != nil {
		return err
	}
	if err = r.projection.Apply(ctx, ProjectionState{Enabled: true, Generation: s.Generation, Token: token}); err != nil {
		return err
	}
	s.Phase = PhaseConnecting
	s.ObservedHealthy = false
	tunnel, err := r.cloud.GetTunnel(ctx, s.Resources.AccountID, s.Resources.TunnelID)
	if err != nil {
		return err
	}
	if !ownedTunnel(*s, tunnel) || tunnel.DeletedAt != nil {
		return ErrOwnershipConflict
	}
	if tunnel.Status == "healthy" && r.acceptance != nil {
		ready, e := r.acceptance.Ready(ctx, *s)
		if e != nil {
			return e
		}
		if ready {
			s.Phase = PhaseHealthy
			s.ObservedHealthy = true
		}
	}
	return r.persist(ctx, s)
}

func (r *Reconciler) persist(ctx context.Context, s *State) error {
	saved, err := r.store.Advance(ctx, *s)
	if err == nil {
		*s = saved
	}
	return err
}
func (r *Reconciler) current(ctx context.Context, s *State) error {
	current, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	if current.Generation != s.Generation {
		return ErrStaleGeneration
	}
	return nil
}
func (r *Reconciler) failed(ctx context.Context, s *State, err error) error {
	if errors.Is(err, ErrStaleGeneration) || errors.Is(err, context.Canceled) {
		return err
	}
	deleting := s.Phase == PhaseDeleting
	s.Phase = PhaseError
	s.ObservedHealthy = false
	s.LastError = "Remote access reconciliation failed; retry or check credentials."
	if cloudflareapi.Retryable(err) || errors.Is(err, ErrRemotePresent) {
		s.Phase = PhaseDegraded
		if deleting {
			s.Phase = PhaseDeleting
		}
		if r.retryGeneration != s.Generation {
			r.attempts = 0
		}
		delay := time.Second << min(r.attempts, 9)
		r.retryAt = r.now().Add(min(delay, 5*time.Minute))
		r.retryGeneration = s.Generation
		r.attempts++
	}
	r.log.WarnContext(ctx, "Remote access reconciliation failed", "phase", s.Phase)
	return errors.Join(err, r.persist(ctx, s))
}

// Disable stops the connector while retaining all Cloudflare resources.
func (r *Reconciler) Disable(ctx context.Context, by string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	s.Desired.Enabled = false
	s, err = r.store.SaveDesired(ctx, s.Generation, s.Desired, by)
	if err != nil {
		return err
	}
	return r.disableProjection(ctx, &s)
}
func (r *Reconciler) disableProjection(ctx context.Context, s *State) error {
	if r.projection == nil {
		return ErrConfiguration
	}
	if err := r.projection.Apply(ctx, ProjectionState{Generation: s.Generation}); err != nil {
		return err
	}
	s.Phase = PhaseDisabled
	s.ObservedHealthy = false
	s.LastError = ""
	return r.persist(ctx, s)
}

func notFound(err error) bool {
	var e *cloudflareapi.APIError
	return errors.Is(err, errResourceAbsent) || errors.As(err, &e) && e.Status == 404
}
