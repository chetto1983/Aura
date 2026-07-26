package adaptive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PolicyMode controls whether Aura observes or serves adaptive choices.
type PolicyMode string

// Adaptive policy modes progress from disabled observation to controlled serving.
const (
	PolicyOff      PolicyMode = "off"
	PolicyShadow   PolicyMode = "shadow"
	PolicyCanary   PolicyMode = "canary"
	PolicyActive   PolicyMode = "active"
	PolicyRollback PolicyMode = "rollback"
)

// Policy is the current distributed adaptive-control state.
type Policy struct {
	Epoch      int64
	Version    string
	Mode       PolicyMode
	RolloutBPS int32
	Config     json.RawMessage
	UpdatedAt  time.Time
}

// PolicyReader supplies the authoritative policy at each decision point.
type PolicyReader interface {
	CurrentPolicy(context.Context) (Policy, error)
}

// PolicyStore reads adaptive policy state from PostgreSQL.
type PolicyStore struct {
	pool *pgxpool.Pool
}

// NewPolicyStore binds policy reads to a database pool.
func NewPolicyStore(pool *pgxpool.Pool) *PolicyStore {
	return &PolicyStore{pool: pool}
}

// CurrentPolicy returns the latest authoritative adaptive policy.
func (s *PolicyStore) CurrentPolicy(ctx context.Context) (Policy, error) {
	if s == nil || s.pool == nil {
		return Policy{}, errors.New("adaptive policy store requires a database pool")
	}
	row, err := sqlc.New(s.pool).GetAdaptivePolicy(ctx)
	if err != nil {
		return Policy{}, fmt.Errorf("read adaptive policy: %w", err)
	}
	return policyFromRow(row.Epoch, row.PolicyVersion, row.Mode, row.RolloutBps, row.Config, row.UpdatedAt.Time), nil
}

// ApplyEvidenceTransition advances policy only through sealed database evidence.
func (s *PolicyStore) ApplyEvidenceTransition(
	ctx context.Context,
	actorID uuid.UUID,
	expectedEpoch int64,
	evidenceID uuid.UUID,
	rolloutBPS int32,
	reason string,
) (Policy, error) {
	if s == nil || s.pool == nil {
		return Policy{}, errors.New("adaptive policy store requires a database pool")
	}
	if actorID == uuid.Nil || evidenceID == uuid.Nil || expectedEpoch < 0 ||
		rolloutBPS < 0 || rolloutBPS > 10_000 || strings.TrimSpace(reason) == "" {
		return Policy{}, errors.New("adaptive evidence transition is invalid")
	}
	var policy Policy
	err := db.WithIdentityTx(
		ctx, s.pool, actorID.String(),
		func(queries *sqlc.Queries) error {
			row, err := queries.ApplyAdaptivePolicyTransition(
				ctx,
				sqlc.ApplyAdaptivePolicyTransitionParams{
					ActorID: dbUUID(actorID), ExpectedEpoch: expectedEpoch,
					EvidenceID: dbUUID(evidenceID), RolloutBps: rolloutBPS,
					Reason: reason,
				},
			)
			if err != nil {
				return err
			}
			policy = policyFromRow(
				row.Epoch, row.PolicyVersion, row.Mode, row.RolloutBps,
				row.Config, row.UpdatedAt.Time,
			)
			return nil
		},
	)
	if err != nil {
		return Policy{}, fmt.Errorf("apply adaptive evidence transition: %w", err)
	}
	return policy, nil
}

func policyFromRow(epoch int64, version, mode string, rolloutBPS int32, config []byte, updatedAt time.Time) Policy {
	return Policy{
		Epoch: epoch, Version: version, Mode: PolicyMode(mode), RolloutBPS: rolloutBPS,
		Config: append(json.RawMessage(nil), config...), UpdatedAt: updatedAt,
	}
}

// PolicyGate is the decision-local result of evaluating the current policy.
type PolicyGate struct {
	Epoch         int64
	PolicyVersion string
	Observe       bool
	Adapt         bool
	Reason        string
}

// PolicyController deliberately has no policy cache. Each execution decision
// re-reads the authoritative epoch, so a rollback performed by another process
// is visible before the next adaptive action. A read failure returns a disabled
// gate plus the error; callers continue with the non-adaptive baseline.
type PolicyController struct {
	reader PolicyReader
}

// NewPolicyController creates a no-cache distributed policy gate.
func NewPolicyController(reader PolicyReader) *PolicyController {
	return &PolicyController{reader: reader}
}

// Gate decides whether one deterministic decision assignment may observe or adapt.
func (c *PolicyController) Gate(ctx context.Context, decisionID uuid.UUID) (PolicyGate, error) {
	if c == nil || c.reader == nil {
		return PolicyGate{Reason: "policy_unavailable"}, errors.New("adaptive policy reader is not configured")
	}
	policy, err := c.reader.CurrentPolicy(ctx)
	if err != nil {
		return PolicyGate{Reason: "policy_unavailable"}, err
	}
	gate := PolicyGate{
		Epoch: policy.Epoch, PolicyVersion: policy.Version, Reason: string(policy.Mode),
	}
	switch policy.Mode {
	case PolicyOff, PolicyRollback:
		return gate, nil
	case PolicyShadow:
		gate.Observe = true
		return gate, nil
	case PolicyCanary:
		gate.Observe = true
		gate.Adapt = canaryAssigned(decisionID, policy.Version, policy.RolloutBPS)
		return gate, nil
	case PolicyActive:
		gate.Observe = true
		gate.Adapt = true
		return gate, nil
	default:
		return PolicyGate{Epoch: policy.Epoch, PolicyVersion: policy.Version, Reason: "invalid_policy"},
			fmt.Errorf("unknown adaptive policy mode %q", policy.Mode)
	}
}

func canaryAssigned(decisionID uuid.UUID, policyVersion string, rolloutBPS int32) bool {
	sum := sha256.Sum256([]byte(decisionID.String() + "\x00" + policyVersion))
	bucket := binary.BigEndian.Uint64(sum[:8]) % 10_000
	return bucket < uint64(rolloutBPS)
}
