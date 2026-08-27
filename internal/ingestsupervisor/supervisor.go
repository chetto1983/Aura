// Package ingestsupervisor keeps one identity-scoped CocoIndex worker running for
// every active user that already owns a provisioned Garage credential.
package ingestsupervisor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

const (
	defaultPollInterval = 15 * time.Second
	defaultStateRoot    = "/state"
)

// IdentityLister is the existing identity-store surface used to discover active users.
type IdentityLister interface {
	ListIdentities(context.Context) ([]identity.Identity, error)
}

// CredentialResolver is the existing RLS-scoped Garage credential resolver.
type CredentialResolver interface {
	Resolve(context.Context) (objectstore.Credentials, error)
}

// Process is one running identity-scoped CocoIndex child.
type Process interface {
	Done() <-chan error
	Stop(context.Context) error
}

// Launcher starts a CocoIndex process from an identity-scoped specification.
type Launcher interface {
	Start(context.Context, ProcessSpec) (Process, error)
}

// Options configure discovery cadence and the non-secret shared S3 route.
type Options struct {
	PollInterval time.Duration
	StateRoot    string
	S3Endpoint   string
	S3Region     string
	Logger       *slog.Logger
}

// ProcessSpec is the complete private binding for one CocoIndex child.
type ProcessSpec struct {
	IdentityID string
	Bucket     string
	AccessKey  string
	SecretKey  string
	S3Endpoint string
	S3Region   string
	StateDB    string
}

// Environment overlays a child binding onto inherited deployment configuration.
// Existing values for the owned keys are replaced rather than duplicated.
func (s ProcessSpec) Environment(base []string) []string {
	overrides := []string{
		"AURA_INGEST_IDENTITY_ID=" + s.IdentityID,
		"AURA_INGEST_S3_ENDPOINT=" + s.S3Endpoint,
		"AURA_INGEST_S3_BUCKET=" + s.Bucket,
		"AURA_INGEST_S3_ACCESS_KEY_ID=" + s.AccessKey,
		"AURA_INGEST_S3_SECRET_ACCESS_KEY=" + s.SecretKey,
		"AURA_INGEST_S3_REGION=" + s.S3Region,
		"COCOINDEX_DB=" + s.StateDB,
	}
	owned := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		owned[environmentKey(entry)] = struct{}{}
	}
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		if _, replace := owned[environmentKey(entry)]; !replace {
			env = append(env, entry)
		}
	}
	return append(env, overrides...)
}

func (s ProcessSpec) fingerprint() [sha256.Size]byte {
	return sha256.Sum256([]byte(strings.Join([]string{
		s.IdentityID, s.Bucket, s.AccessKey, s.SecretKey,
		s.S3Endpoint, s.S3Region, s.StateDB,
	}, "\x00")))
}

type managedProcess struct {
	process     Process
	fingerprint [sha256.Size]byte
}

// Supervisor owns the desired active-user process set.
type Supervisor struct {
	lister   IdentityLister
	resolver CredentialResolver
	launcher Launcher
	options  Options
	active   map[string]managedProcess
	unbound  map[string]string
}

// New constructs a supervisor over Aura's existing identity and object-store seams.
func New(lister IdentityLister, resolver CredentialResolver, launcher Launcher, options Options) *Supervisor {
	if options.PollInterval <= 0 {
		options.PollInterval = defaultPollInterval
	}
	if strings.TrimSpace(options.StateRoot) == "" {
		options.StateRoot = defaultStateRoot
	}
	if strings.TrimSpace(options.S3Region) == "" {
		options.S3Region = "garage"
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Supervisor{
		lister: lister, resolver: resolver, launcher: launcher,
		options: options, active: make(map[string]managedProcess), unbound: make(map[string]string),
	}
}

// Run reconciles immediately and then on the configured bounded interval until shutdown.
func (s *Supervisor) Run(ctx context.Context) error {
	if s.lister == nil || s.resolver == nil || s.launcher == nil {
		return errors.New("ingest supervisor requires identity, credential, and process dependencies")
	}
	if err := s.Reconcile(ctx); err != nil {
		s.options.Logger.Error("initial ingest reconciliation failed", "error", err)
	}
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return errors.Join(ctx.Err(), s.stopAll(shutdownCtx))
		case <-ticker.C:
			if err := s.Reconcile(ctx); err != nil {
				s.options.Logger.Error("ingest reconciliation failed", "error", err)
			}
		}
	}
}

// Reconcile converges the running children on provisioned, active user identities.
func (s *Supervisor) Reconcile(ctx context.Context) error {
	s.reapExited()
	identities, err := s.lister.ListIdentities(ctx)
	if err != nil {
		return fmt.Errorf("list identities: %w", err)
	}
	desired := make(map[string]ProcessSpec)
	seenUsers := make(map[string]struct{})
	for _, item := range identities {
		if item.Kind != "user" || item.Deactivated {
			continue
		}
		seenUsers[item.ID] = struct{}{}
		identityCtx := identityctx.WithIdentityID(ctx, item.ID)
		credentials, resolveErr := s.resolver.Resolve(identityCtx)
		if resolveErr != nil {
			if previous := s.unbound[item.ID]; previous != resolveErr.Error() {
				s.options.Logger.Warn("identity has no usable Garage binding", "identity", item.ID, "error", resolveErr)
				s.unbound[item.ID] = resolveErr.Error()
			}
			continue
		}
		delete(s.unbound, item.ID)
		desired[item.ID] = ProcessSpec{
			IdentityID: item.ID,
			Bucket:     credentials.Bucket, AccessKey: credentials.AccessKey, SecretKey: credentials.SecretKey,
			S3Endpoint: s.options.S3Endpoint, S3Region: s.options.S3Region,
			StateDB: filepath.Join(s.options.StateRoot, item.ID, "coco.db"),
		}
	}
	for identityID := range s.unbound {
		if _, stillActive := seenUsers[identityID]; !stillActive {
			delete(s.unbound, identityID)
		}
	}

	var reconcileErr error
	for identityID, running := range s.active {
		spec, wanted := desired[identityID]
		if wanted && running.fingerprint == spec.fingerprint() {
			continue
		}
		if stopErr := running.process.Stop(ctx); stopErr != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("stop ingest for %s: %w", identityID, stopErr))
		}
		delete(s.active, identityID)
	}

	ids := make([]string, 0, len(desired))
	for identityID := range desired {
		ids = append(ids, identityID)
	}
	sort.Strings(ids)
	for _, identityID := range ids {
		if _, running := s.active[identityID]; running {
			continue
		}
		spec := desired[identityID]
		process, startErr := s.launcher.Start(ctx, spec)
		if startErr != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("start ingest for %s: %w", identityID, startErr))
			continue
		}
		s.active[identityID] = managedProcess{process: process, fingerprint: spec.fingerprint()}
		s.options.Logger.Info("identity ingest started", "identity", identityID, "bucket", spec.Bucket)
	}
	return reconcileErr
}

func (s *Supervisor) reapExited() {
	for identityID, running := range s.active {
		select {
		case err := <-running.process.Done():
			delete(s.active, identityID)
			s.options.Logger.Warn("identity ingest exited", "identity", identityID, "error", err)
		default:
		}
	}
}

func (s *Supervisor) stopAll(ctx context.Context) error {
	var stopErr error
	for identityID, running := range s.active {
		if err := running.process.Stop(ctx); err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("stop ingest for %s: %w", identityID, err))
		}
		delete(s.active, identityID)
	}
	return stopErr
}

func environmentKey(entry string) string {
	key, _, _ := strings.Cut(entry, "=")
	return key
}

func environmentValue(env []string, key string) string {
	for _, entry := range env {
		if environmentKey(entry) == key {
			_, value, _ := strings.Cut(entry, "=")
			return value
		}
	}
	return ""
}

func environmentCount(env []string, key string) int {
	count := 0
	for _, entry := range env {
		if environmentKey(entry) == key {
			count++
		}
	}
	return count
}
