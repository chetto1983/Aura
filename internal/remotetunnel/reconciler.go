package remotetunnel

import (
	"context"
	"errors"
	"fmt"
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
	nameServers     NameServers
	log             *slog.Logger
	now             func() time.Time
	retryAt         time.Time
	attempts        uint
	retryGeneration int64
}

// Option binds the credential authority.
type Option func(*Reconciler)

// WithCredentials is required before enabled reconciliation can publish.
func WithCredentials(c TunnelCredentials) Option { return func(r *Reconciler) { r.credentials = c } }

// WithNameServers is required before a domain that Cloudflare does not hold yet can be added: it
// is how a domain active in another Cloudflare account is told apart from a fresh one.
func WithNameServers(n NameServers) Option { return func(r *Reconciler) { r.nameServers = n } }

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

// RetryNow is an operator's press: the next pass runs at once and the next failure waits from the
// start. Automatic passes never call it, so they keep their backoff.
func (r *Reconciler) RetryNow() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts = 0
	r.retryAt = time.Time{}
}

// Reconcile resumes persisted work; terminal errors require an explicit intent save.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	if s.Generation == r.retryGeneration && r.now().Before(r.retryAt) {
		return ErrBackoff
	}
	if s.Phase == PhaseDeleting {
		return r.delete(ctx, &s)
	}
	// Retirement must not wait for a healthy install: an error row and a disabled one hold WARP too.
	if err = r.retireWARP(ctx, &s); err != nil {
		if s.Phase != PhaseError && s.Desired.Enabled {
			return r.failed(ctx, &s, err)
		}
		r.log.WarnContext(ctx, "Retiring the WARP leg failed; the row keeps its state", "phase", s.Phase, "error", err)
	}
	if s.Phase == PhaseError {
		return ErrTerminal
	}
	if !s.Desired.Enabled {
		// failed would write error over a disabled row, which SaveDesired reads as deletion intent.
		err = r.disableProjection(ctx, &s)
		if err != nil {
			r.log.WarnContext(ctx, "Stopping the connector of a disabled install failed; retrying", "error", err)
		}
		return err
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
	// Without a tunnel the connector has nothing to serve: a token replacement can leave the old
	// one running, and no later step may start before it is off.
	if s.Resources.TunnelID == "" {
		if err := r.project(ctx, ProjectionState{Generation: s.Generation}); err != nil {
			return err
		}
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
	if addressChanged(*s) {
		s.Phase, s.Reason, s.ObservedHealthy = PhaseProvisioning, "", false
		if err = r.teardown(ctx, s); err != nil {
			return err
		}
	}
	recordOwned(s)
	active, err := r.zone(ctx, s)
	if err != nil || !active {
		return err
	}
	if !settled(s) {
		s.Phase = PhaseProvisioning
		s.Reason = ""
		if err = r.persist(ctx, s); err != nil {
			return err
		}
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
	if err = r.project(ctx, ProjectionState{Enabled: true, Generation: s.Generation, Token: token}); err != nil {
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
	s.Reason = ""
	if s.Accepted() {
		// The phone test passed once for this address; an outage degrades it and heals by itself.
		s.Phase, s.ObservedHealthy = PhaseHealthy, tunnelServes(tunnel)
		if !s.ObservedHealthy {
			s.Phase, s.Reason = PhaseDegraded, ReasonTunnelDown
		}
	}
	return r.persist(ctx, s)
}

// settled is a row whose last observation stands until this pass replaces it: writing
// provisioning over it would show the operator the connection step for the length of the walk.
func settled(s *State) bool {
	return s.Phase == PhaseHealthy || s.Phase == PhaseDegraded && s.Reason == ReasonTunnelDown
}

// tunnelServes: Cloudflare reports degraded for a connector that is running and serving traffic
// with at least one failed connection (developers.cloudflare.com/cloudflare-one/networks/
// connectors/cloudflare-tunnel/troubleshoot-tunnels/common-errors/, "Tunnel status").
func tunnelServes(t cloudflareapi.Tunnel) bool {
	return t.Status == "healthy" || t.Status == "degraded"
}

// project is the one way the connector's state is written, so that a failed write is always the
// retryable ErrProjection and never a terminal error.
func (r *Reconciler) project(ctx context.Context, state ProjectionState) error {
	if err := r.projection.Apply(ctx, state); err != nil {
		return fmt.Errorf("%w: %w", ErrProjection, err)
	}
	return nil
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
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrStaleGeneration) || errors.Is(err, context.Canceled) {
		return err
	}
	// Disabled operations never call failed except deletion: disabled member sync is
	// refused locally and disableProjection returns directly. SaveDesired relies on
	// error + disabled + retained references to recover terminal deletion intent.
	deleting := s.Phase == PhaseDeleting
	s.Phase = PhaseError
	s.ObservedHealthy = false
	s.Reason = ReasonOf(err)
	if transient(err) {
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
	// Reason is a code, never the cause, because it reaches a browser; the cause has to survive
	// somewhere, and this log is the only place an operator can read it.
	r.log.WarnContext(ctx, "Remote access reconciliation failed", "phase", s.Phase, "error", err)
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
	if err := r.project(ctx, ProjectionState{Generation: s.Generation}); err != nil {
		return err
	}
	s.Phase = PhaseDisabled
	s.ObservedHealthy = false
	s.Reason = ""
	return r.persist(ctx, s)
}

// denied is the token lacking permission for a resource, not the resource being absent.
func denied(err error) bool {
	var e *cloudflareapi.APIError
	return errors.As(err, &e) && (e.Status == 401 || e.Status == 403)
}

func notFound(err error) bool {
	var e *cloudflareapi.APIError
	return errors.Is(err, errResourceAbsent) || errors.As(err, &e) && e.Status == 404
}
