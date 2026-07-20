package conversations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadEffectiveCompaction projects only the versioned active config for runtime consumers.
func (s *CompactionRolloutStore) LoadEffectiveCompaction(ctx context.Context, scope string) (config.PersistedCompactionState, error) {
	state, err := s.Load(ctx, scope)
	if err != nil {
		return config.PersistedCompactionState{}, err
	}
	return config.PersistedCompactionState{ScopeID: state.ScopeID, Version: state.Version, ActiveConfig: append([]byte(nil), state.ActiveConfig...)}, nil
}

// ErrRolloutStaleVersion identifies a failed expected-version CAS.
var ErrRolloutStaleVersion = errors.New("compaction rollout version is stale")

var rolloutCodePattern = regexp.MustCompile(`^[a-z0-9_]+$`)
var rolloutDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RolloutState is the durable effective rollout state shared by all replicas.
type RolloutState struct {
	ScopeID, Stage                                                string
	Version, EligibleAttempts                                     int64
	StageStartedAt                                                time.Time
	EvaluatorVersion, ScorerVersion, ConfigVersion, CorpusVersion string
	StratumSnapshots, FailureWindow, LatencyWindow, RestoreWindow []byte
	ActiveConfig, LastKnownGoodConfig, LastKnownGoodPolicy        []byte
}

// RolloutEvidence is an immutable, digest-addressed evaluator observation.
type RolloutEvidence struct {
	ScopeID, Digest                                               string
	EvaluatorVersion, ScorerVersion, ConfigVersion, CorpusVersion string
	Snapshot                                                      []byte
}

// RolloutTransition atomically records evidence, a decision, and a new effective state.
type RolloutTransition struct {
	ExpectedVersion int64
	State           RolloutState
	Evidence        RolloutEvidence
	ReasonCode      string
}

// RolloutRollback atomically records why the complete last-known-good snapshot was restored.
type RolloutRollback struct {
	ScopeID         string
	ExpectedVersion int64
	Evidence        RolloutEvidence
	ReasonCode      string
}

// CompactionRolloutStore owns the transactional rollout control-plane boundary.
type CompactionRolloutStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewCompactionRolloutStore constructs a durable rollout store without activating it.
func NewCompactionRolloutStore(pool *pgxpool.Pool) *CompactionRolloutStore {
	return &CompactionRolloutStore{pool: pool, q: sqlc.New(pool)}
}

// Create installs a disabled-by-default effective state for one deployment scope.
func (s *CompactionRolloutStore) Create(ctx context.Context, state RolloutState) (RolloutState, error) {
	if err := validateRolloutState(state); err != nil {
		return RolloutState{}, err
	}
	row, err := s.q.CreateCompactionRolloutState(ctx, sqlc.CreateCompactionRolloutStateParams{
		ScopeID: state.ScopeID, EvaluatorVersion: state.EvaluatorVersion, ScorerVersion: state.ScorerVersion,
		ConfigVersion: state.ConfigVersion, CorpusVersion: state.CorpusVersion, ActiveConfig: state.ActiveConfig,
		LastKnownGoodConfig: state.LastKnownGoodConfig, LastKnownGoodPolicy: state.LastKnownGoodPolicy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return s.Load(ctx, state.ScopeID)
	}
	if err != nil {
		return RolloutState{}, fmt.Errorf("create compaction rollout state: %w", err)
	}
	return rolloutStateFromSQL(row), nil
}

// bootstrapEvaluatorVersion, bootstrapScorerVersion, bootstrapConfigVersion, and
// bootstrapCorpusVersion identify the disabled-default row EnsureDisabledDefault
// seeds. They follow the "v1" convention already used by the rollout evidence/decision
// fixtures across this package (see rolloutFixture/rolloutControllerState in the tests)
// so a freshly-seeded scope stays version-consistent with the first real evaluator
// evidence appended against it; bump alongside a rollout evidence migration.
const (
	bootstrapEvaluatorVersion = "eval-v1"
	bootstrapScorerVersion    = "score-v1"
	bootstrapConfigVersion    = "config-v1"
	bootstrapCorpusVersion    = "corpus-v1"
)

// bootstrapDisabledActiveConfig is the exact disabled snapshot
// config.PersistedCompactionReader.Read requires ({mode,percent,recovery_drill_passed})
// to yield a valid, non-fail-closed disabled CompactionConfig.
var bootstrapDisabledActiveConfig = []byte(`{"mode":"disabled","percent":0,"recovery_drill_passed":false}`)

// EnsureDisabledDefault idempotently seeds a disabled-by-default effective state for
// scopeID and returns the current row. A freshly migrated deployment has NO row for
// any scope (0039_compaction_rollout creates the table but seeds nothing), so the
// durable boot preflight and the background evaluator loop would otherwise hard-fail
// forever with "no rows in result set". This reuses Create's existing idempotent
// on-conflict Load fallback: the first caller inserts the seed, every later caller
// (including every subsequent boot) finds the existing row and returns it unmodified.
// A genuine failure (DB unreachable, or an existing row somehow failing
// validateRolloutState) still propagates so boot keeps failing closed.
func (s *CompactionRolloutStore) EnsureDisabledDefault(ctx context.Context, scopeID string) (RolloutState, error) {
	return s.Create(ctx, RolloutState{
		ScopeID:             scopeID,
		Stage:               "disabled",
		EvaluatorVersion:    bootstrapEvaluatorVersion,
		ScorerVersion:       bootstrapScorerVersion,
		ConfigVersion:       bootstrapConfigVersion,
		CorpusVersion:       bootstrapCorpusVersion,
		StratumSnapshots:    []byte(`{}`),
		FailureWindow:       []byte(`{}`),
		LatencyWindow:       []byte(`{}`),
		RestoreWindow:       []byte(`{}`),
		ActiveConfig:        bootstrapDisabledActiveConfig,
		LastKnownGoodConfig: bootstrapDisabledActiveConfig,
		LastKnownGoodPolicy: []byte(`{}`),
	})
}

// Load reads the replica-shared effective state for a deployment scope.
func (s *CompactionRolloutStore) Load(ctx context.Context, scopeID string) (RolloutState, error) {
	row, err := s.q.GetCompactionRolloutState(ctx, scopeID)
	if err != nil {
		return RolloutState{}, fmt.Errorf("load compaction rollout state %q: %w", scopeID, err)
	}
	return rolloutStateFromSQL(row), nil
}

// Transition applies one expected-version state change and immutable decision transactionally.
func (s *CompactionRolloutStore) Transition(ctx context.Context, change RolloutTransition) (out RolloutState, err error) {
	if err = validateTransition(change); err != nil {
		return out, err
	}
	err = db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		before, txErr := q.GetCompactionRolloutState(ctx, change.State.ScopeID)
		if txErr != nil {
			return fmt.Errorf("load compaction rollout before transition: %w", txErr)
		}
		row, txErr := q.CASTransitionCompactionRollout(ctx, transitionParams(change))
		if errors.Is(txErr, pgx.ErrNoRows) {
			return ErrRolloutStaleVersion
		}
		if txErr != nil {
			return fmt.Errorf("CAS compaction rollout transition: %w", txErr)
		}
		evidence, txErr := appendRolloutEvidence(ctx, q, change.Evidence)
		if txErr != nil {
			return txErr
		}
		_, txErr = q.AppendCompactionRolloutDecision(ctx, sqlc.AppendCompactionRolloutDecisionParams{
			ScopeID: change.State.ScopeID, EvidenceID: evidence.ID, ExpectedVersion: change.ExpectedVersion,
			ResultingVersion: row.Version, DecisionKind: "transition", FromStage: before.Stage,
			ToStage: row.Stage, ReasonCode: change.ReasonCode,
		})
		if txErr != nil {
			return fmt.Errorf("append compaction rollout decision: %w", txErr)
		}
		out = rolloutStateFromSQL(row)
		return nil
	})
	if err != nil {
		return RolloutState{}, fmt.Errorf("transition compaction rollout %q: %w", change.State.ScopeID, err)
	}
	return out, nil
}

// Rollback restores the last-known-good config and records evidence and reason atomically.
func (s *CompactionRolloutStore) Rollback(ctx context.Context, rollback RolloutRollback) (out RolloutState, err error) {
	if err = validateRollback(rollback); err != nil {
		return out, err
	}
	err = db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		before, txErr := q.GetCompactionRolloutState(ctx, rollback.ScopeID)
		if txErr != nil {
			return fmt.Errorf("load compaction rollout before rollback: %w", txErr)
		}
		row, txErr := q.CASRollbackCompactionRollout(ctx, sqlc.CASRollbackCompactionRolloutParams{ScopeID: rollback.ScopeID, ExpectedVersion: rollback.ExpectedVersion})
		if errors.Is(txErr, pgx.ErrNoRows) {
			return ErrRolloutStaleVersion
		}
		if txErr != nil {
			return fmt.Errorf("CAS compaction rollout rollback: %w", txErr)
		}
		evidence, txErr := appendRolloutEvidence(ctx, q, rollback.Evidence)
		if txErr != nil {
			return txErr
		}
		_, txErr = q.AppendCompactionRolloutDecision(ctx, sqlc.AppendCompactionRolloutDecisionParams{
			ScopeID: rollback.ScopeID, EvidenceID: evidence.ID, ExpectedVersion: rollback.ExpectedVersion,
			ResultingVersion: row.Version, DecisionKind: "rollback", FromStage: before.Stage, ToStage: row.Stage, ReasonCode: rollback.ReasonCode,
		})
		if txErr != nil {
			return fmt.Errorf("append compaction rollout rollback decision: %w", txErr)
		}
		out = rolloutStateFromSQL(row)
		return nil
	})
	if err != nil {
		return RolloutState{}, fmt.Errorf("rollback compaction rollout %q: %w", rollback.ScopeID, err)
	}
	return out, nil
}

// DecisionCount returns immutable ledger cardinality for recovery verification.
func (s *CompactionRolloutStore) DecisionCount(ctx context.Context, scopeID string) (int64, error) {
	n, err := s.q.CountCompactionRolloutDecisions(ctx, scopeID)
	if err != nil {
		return 0, fmt.Errorf("count compaction rollout decisions %q: %w", scopeID, err)
	}
	return n, nil
}

func appendRolloutEvidence(ctx context.Context, q *sqlc.Queries, evidence RolloutEvidence) (sqlc.AuraCompactionRolloutEvidence, error) {
	row, err := q.AppendCompactionRolloutEvidence(ctx, sqlc.AppendCompactionRolloutEvidenceParams{ScopeID: evidence.ScopeID, EvidenceDigest: evidence.Digest, EvaluatorVersion: evidence.EvaluatorVersion, ScorerVersion: evidence.ScorerVersion, ConfigVersion: evidence.ConfigVersion, CorpusVersion: evidence.CorpusVersion, Snapshot: evidence.Snapshot})
	if errors.Is(err, pgx.ErrNoRows) {
		// The digest already exists (a byte-identical observation was appended before):
		// ON CONFLICT DO NOTHING returns no row. The ledger is immutable, so re-fetch the
		// existing row to keep the append idempotent — reusing its id for the decision
		// insert — instead of propagating a 23505 that would kill the evaluator (Wave 0.1,
		// mirrors Create's on-conflict Load fallback).
		existing, getErr := q.GetCompactionRolloutEvidenceByDigest(ctx, sqlc.GetCompactionRolloutEvidenceByDigestParams{ScopeID: evidence.ScopeID, EvidenceDigest: evidence.Digest})
		if getErr != nil {
			return existing, fmt.Errorf("load existing compaction rollout evidence: %w", getErr)
		}
		return existing, nil
	}
	if err != nil {
		return row, fmt.Errorf("append compaction rollout evidence: %w", err)
	}
	return row, nil
}

func validateTransition(change RolloutTransition) error {
	if change.ExpectedVersion < 1 || change.State.Version != change.ExpectedVersion {
		return errors.New("rollout transition expected version must match state version")
	}
	if err := validateReason(change.ReasonCode); err != nil {
		return err
	}
	if err := validateRolloutState(change.State); err != nil {
		return err
	}
	return validateEvidence(change.State.ScopeID, change.Evidence)
}

func validateRollback(rollback RolloutRollback) error {
	if rollback.ExpectedVersion < 1 {
		return errors.New("rollout rollback expected version must be positive")
	}
	if err := validateReason(rollback.ReasonCode); err != nil {
		return err
	}
	return validateEvidence(rollback.ScopeID, rollback.Evidence)
}

func validateRolloutState(state RolloutState) error {
	if state.ScopeID == "" || state.Stage == "" || state.EvaluatorVersion == "" || state.ScorerVersion == "" || state.ConfigVersion == "" || state.CorpusVersion == "" {
		return errors.New("rollout state scope, stage, and versions are required")
	}
	for name, value := range map[string][]byte{"stratum snapshots": state.StratumSnapshots, "failure window": state.FailureWindow, "latency window": state.LatencyWindow, "restore window": state.RestoreWindow, "active config": state.ActiveConfig, "last-known-good config": state.LastKnownGoodConfig, "last-known-good policy": state.LastKnownGoodPolicy} {
		if err := validateJSONObject(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func validateEvidence(scope string, evidence RolloutEvidence) error {
	if evidence.ScopeID != scope || !rolloutDigestPattern.MatchString(evidence.Digest) {
		return errors.New("rollout evidence scope or SHA-256 digest is invalid")
	}
	if evidence.EvaluatorVersion == "" || evidence.ScorerVersion == "" || evidence.ConfigVersion == "" || evidence.CorpusVersion == "" {
		return errors.New("rollout evidence versions are required")
	}
	return validateJSONObject(evidence.Snapshot)
}

func validateReason(code string) error {
	if !rolloutCodePattern.MatchString(code) {
		return errors.New("rollout reason must be a stable locale-neutral code")
	}
	return nil
}
func validateJSONObject(value []byte) error {
	var v any
	if json.Unmarshal(value, &v) != nil || len(bytes.TrimSpace(value)) == 0 {
		return errors.New("must be valid JSON")
	}
	if _, ok := v.(map[string]any); !ok {
		return errors.New("must be a JSON object")
	}
	return nil
}

func transitionParams(change RolloutTransition) sqlc.CASTransitionCompactionRolloutParams {
	s := change.State
	return sqlc.CASTransitionCompactionRolloutParams{Stage: s.Stage, StageStartedAt: pgtype.Timestamptz{Time: s.StageStartedAt, Valid: true}, EligibleAttempts: s.EligibleAttempts, EvaluatorVersion: s.EvaluatorVersion, ScorerVersion: s.ScorerVersion, ConfigVersion: s.ConfigVersion, CorpusVersion: s.CorpusVersion, StratumSnapshots: s.StratumSnapshots, FailureWindow: s.FailureWindow, LatencyWindow: s.LatencyWindow, RestoreWindow: s.RestoreWindow, ActiveConfig: s.ActiveConfig, LastKnownGoodConfig: s.LastKnownGoodConfig, LastKnownGoodPolicy: s.LastKnownGoodPolicy, ScopeID: s.ScopeID, ExpectedVersion: change.ExpectedVersion}
}

func rolloutStateFromSQL(row sqlc.AuraCompactionRolloutStates) RolloutState {
	return RolloutState{ScopeID: row.ScopeID, Version: row.Version, Stage: row.Stage, StageStartedAt: row.StageStartedAt.Time, EligibleAttempts: row.EligibleAttempts, EvaluatorVersion: row.EvaluatorVersion, ScorerVersion: row.ScorerVersion, ConfigVersion: row.ConfigVersion, CorpusVersion: row.CorpusVersion, StratumSnapshots: row.StratumSnapshots, FailureWindow: row.FailureWindow, LatencyWindow: row.LatencyWindow, RestoreWindow: row.RestoreWindow, ActiveConfig: row.ActiveConfig, LastKnownGoodConfig: row.LastKnownGoodConfig, LastKnownGoodPolicy: row.LastKnownGoodPolicy}
}
