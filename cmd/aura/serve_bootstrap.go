package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/webauth"
)

const bootstrapAdvisoryLockKey int64 = 0x41555241424f4f54 // "AURABOOT"

type bootstrapServer interface {
	SetBootstrapService(agui.BootstrapService)
}

type bootstrapProvider interface {
	CoreServices() *authulaservices.CoreServices
	OperatorUserID(context.Context) (string, error)
}

// firstPartyGrantProvisioner mints the identity-scoped grants for the MCP sidecars Aura
// ships. It is OPTIONAL, unlike every other dependency here: a deployment can lack the
// authorization server that issues them and must still be able to create its operator.
type firstPartyGrantProvisioner interface {
	EnsureIdentity(ctx context.Context, identityID string) error
}

func wireBootstrapService(
	server bootstrapServer,
	pool *pgxpool.Pool,
	provider bootstrapProvider,
	memory agui.MemoryProvisioner,
	grants firstPartyGrantProvisioner,
	resources bootstrapResources,
) bool {
	if isNilBootstrapDependency(server) || pool == nil || isNilBootstrapDependency(provider) || isNilBootstrapDependency(memory) {
		return false
	}
	core := provider.CoreServices()
	if core == nil {
		return false
	}
	if isNilBootstrapDependency(grants) {
		grants = nil
	}
	server.SetBootstrapService(&firstOperatorBootstrapService{
		authula:        authulaCoreAdapter{core: core},
		operator:       provider,
		pool:           pool,
		memory:         memory,
		grants:         grants,
		resources:      resources,
		identityDelete: auraLegAdapter{pool: pool},
	})
	return true
}

func isNilBootstrapDependency(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

type firstOperatorBootstrapService struct {
	mu             sync.Mutex
	authula        agui.AuthulaCore
	operator       bootstrapProvider
	pool           *pgxpool.Pool
	memory         agui.MemoryProvisioner
	grants         firstPartyGrantProvisioner
	resources      bootstrapResources
	identityDelete agui.IdentityDeleter
}

func (s *firstOperatorBootstrapService) provisionTenantMemory(
	ctx context.Context,
	identityName, identityID string,
	compAuthula func(),
) error {
	if s.memory == nil || s.identityDelete == nil {
		return errors.New("bootstrap memory provisioning unavailable")
	}
	if err := s.provisionFirstPartyGrants(ctx, identityID); err != nil {
		// Deliberately not fatal, and deliberately not silent. The identity is real and
		// usable without its sidecar grants; the boot keeper re-mints them on its next
		// pass. Rolling the whole operator back over a token would be the worse answer.
		slog.Warn("aura serve: bootstrap first-party MCP grants", "err", err)
	}
	if err := s.memory.ProvisionMemory(ctx, identityID); err != nil {
		cctx := context.WithoutCancel(ctx)
		if derr := s.memory.PurgeMemory(cctx, identityID); derr != nil {
			slog.Error("aura serve: bootstrap memory compensation failed")
		}
		if derr := s.identityDelete.DeleteIdentity(cctx, identityName); derr != nil {
			slog.Error("aura serve: bootstrap identity compensation failed")
		}
		if compAuthula != nil {
			compAuthula()
		}
		return err
	}
	// Memory is the only leg worth destroying an account over — an operator whose
	// long-term memory does not exist is not an operator. The rest (bucket, filesystem
	// roots, box, MCP remount) are fail-soft and loud, in serve_bootstrap_resources.go.
	s.resources.provision(ctx, identityID)
	return nil
}

// provisionFirstPartyGrants mints this identity's grants for the MCP sidecars Aura ships,
// so its memory answers on the first turn instead of after the next restart. A deployment
// with no authorization server wired has nothing to do here.
func (s *firstOperatorBootstrapService) provisionFirstPartyGrants(ctx context.Context, identityID string) error {
	if s.grants == nil {
		return nil
	}
	return s.grants.EnsureIdentity(ctx, identityID)
}

func (s *firstOperatorBootstrapService) CreateFirstOperator(ctx context.Context, req agui.BootstrapCreateRequest) (agui.BootstrapCreateResponse, error) {
	if s == nil || s.authula == nil || s.operator == nil || s.pool == nil {
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	identityName := strings.TrimSpace(req.Email)
	if identityName == "" || req.Password == "" || strings.TrimSpace(req.SecurityQuestion) == "" || strings.TrimSpace(req.SecurityAnswer) == "" {
		return agui.BootstrapCreateResponse{}, agui.ErrBootstrapInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		slog.Warn("aura serve: bootstrap tx begin failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLockKey); err != nil {
		slog.Warn("aura serve: bootstrap advisory lock failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}

	operatorID, err := s.operator.OperatorUserID(ctx)
	if err != nil {
		if errors.Is(err, webauth.ErrOperatorAmbiguous) {
			return agui.BootstrapCreateResponse{}, agui.ErrBootstrapAlreadyConfigured
		}
		slog.Warn("aura serve: bootstrap operator availability check failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	if operatorID != "" {
		return agui.BootstrapCreateResponse{}, agui.ErrBootstrapAlreadyConfigured
	}
	exists, err := s.authula.UserByEmail(ctx, identityName)
	if err != nil {
		slog.Warn("aura serve: bootstrap authula duplicate check failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	if exists {
		return agui.BootstrapCreateResponse{}, agui.ErrBootstrapAlreadyConfigured
	}

	hash, err := s.authula.HashPassword(req.Password)
	if err != nil {
		slog.Warn("aura serve: bootstrap password hash failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	user, err := s.authula.CreateUser(ctx, identityName)
	if err != nil {
		slog.Warn("aura serve: bootstrap authula create user failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	compAuthula := func() {
		if derr := s.authula.DeleteUser(context.WithoutCancel(ctx), user.ID); derr != nil {
			slog.Error("aura serve: bootstrap authula compensation failed")
		}
	}
	if err := s.authula.CreateAccount(ctx, user.ID, user.Email, hash); err != nil {
		compAuthula()
		slog.Warn("aura serve: bootstrap authula create account failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}

	identityID, err := createAuraFirstOperatorTx(ctx, tx, identityName, user.ID, req.SecurityQuestion, req.SecurityAnswer)
	if err != nil {
		compAuthula()
		if errors.Is(err, agui.ErrBootstrapAlreadyConfigured) {
			return agui.BootstrapCreateResponse{}, agui.ErrBootstrapAlreadyConfigured
		}
		slog.Warn("aura serve: bootstrap aura write failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	if err := tx.Commit(ctx); err != nil {
		compAuthula()
		slog.Warn("aura serve: bootstrap tx commit failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	committed = true
	if err := s.provisionTenantMemory(ctx, identityName, identityID, compAuthula); err != nil {
		slog.Warn("aura serve: bootstrap memory provision failed")
		return agui.BootstrapCreateResponse{}, errors.New("bootstrap unavailable")
	}
	return agui.BootstrapCreateResponse{IdentityID: identityID}, nil
}

func createAuraFirstOperatorTx(ctx context.Context, tx pgx.Tx, identityName, authulaUserID, question, answer string) (string, error) {
	answerHash, answerVersion, err := (agui.RecoveryHasher{}).HashAnswer(answer)
	if err != nil {
		return "", fmt.Errorf("bootstrap recovery hash: %w", err)
	}
	q := sqlc.New(tx)
	newID := uuid.New()
	newPGID := pgtype.UUID{Bytes: newID, Valid: true}
	if _, err := q.CreateIdentity(ctx, sqlc.CreateIdentityParams{
		ID:   newPGID,
		Name: identityName,
		Kind: "user",
	}); err != nil {
		if isUniqueViolation(err) {
			return "", agui.ErrBootstrapAlreadyConfigured
		}
		return "", fmt.Errorf("create bootstrap identity: %w", err)
	}
	// The wildcard grant below lands in aura.capability_grants, fail-closed as of migration
	// 0087. The identity is minted one statement above, so the scoping has to happen inside
	// this tx rather than around it.
	if err := db.SetTxIdentity(ctx, tx, newID.String()); err != nil {
		return "", fmt.Errorf("scope bootstrap tx to new identity: %w", err)
	}
	if err := q.GrantCapability(ctx, sqlc.GrantCapabilityParams{
		IdentityID: newPGID,
		Capability: "*",
	}); err != nil {
		return "", fmt.Errorf("grant bootstrap wildcard: %w", err)
	}
	if _, err := tx.Exec(ctx, linkOperatorSQL, newID.String(), authulaUserID); err != nil {
		if isUniqueViolation(err) {
			return "", agui.ErrBootstrapAlreadyConfigured
		}
		return "", fmt.Errorf("link bootstrap operator: %w", err)
	}
	if err := q.UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
		IdentityID:        newPGID,
		Question:          strings.TrimSpace(question),
		AnswerHash:        answerHash,
		AnswerHashVersion: answerVersion,
	}); err != nil {
		return "", fmt.Errorf("write bootstrap recovery: %w", err)
	}
	if err := identity.InsertIdentityAuditTx(ctx, q, identity.IdentityAuditInsert{
		ActorIdentityID:     "bootstrap",
		NewIdentityID:       newID.String(),
		NewIdentityName:     identityName,
		GrantedCapabilities: []string{"*"},
		AuthulaUserID:       authulaUserID,
	}); err != nil {
		return "", fmt.Errorf("write bootstrap audit: %w", err)
	}
	return newID.String(), nil
}
