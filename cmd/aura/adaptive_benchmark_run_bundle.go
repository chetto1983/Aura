package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

const (
	adaptiveBenchmarkDecisionBundleSchemaID = "aura.adaptive.benchmark-decision-bundle/v2"
	adaptiveBenchmarkDecisionBundleRevision = 2
)

var adaptiveBenchmarkDecisionBundleNamespace = uuid.MustParse(
	"1c263ae1-4346-49d5-9b34-d5190b3a2a29",
)

type adaptiveBenchmarkDecisionBundleManifest struct {
	SchemaID       string                                 `json:"schema_id"`
	Revision       int                                    `json:"revision"`
	RunID          string                                 `json:"run_id"`
	PrimaryOwnerID string                                 `json:"primary_owner_id"`
	ControlOwnerID string                                 `json:"control_owner_id"`
	ProviderID     string                                 `json:"provider_id"`
	ModelID        string                                 `json:"model_id"`
	Seed           uint64                                 `json:"seed"`
	PolicyEpoch    int64                                  `json:"policy_epoch"`
	PolicyVersion  string                                 `json:"policy_version"`
	PolicySHA256   string                                 `json:"policy_sha256"`
	TrainingCutoff string                                 `json:"training_cutoff"`
	Entries        []adaptiveBenchmarkBundleManifestEntry `json:"entries"`
}

type adaptiveBenchmarkBundleManifestEntry struct {
	OwnerID        string                 `json:"owner_id"`
	Domain         adaptive.Domain        `json:"domain"`
	Point          adaptive.DecisionPoint `json:"point"`
	SnapshotID     string                 `json:"snapshot_id"`
	SnapshotSHA256 string                 `json:"snapshot_sha256"`
	CohortID       string                 `json:"cohort_id"`
	CohortSHA256   string                 `json:"cohort_sha256"`
}

type adaptiveBenchmarkDecisionBundleEntry struct {
	OwnerID        uuid.UUID
	Domain         adaptive.Domain
	Point          adaptive.DecisionPoint
	SnapshotID     uuid.UUID
	SnapshotSHA256 string
	Snapshot       *adaptive.PolicySnapshot
	Policy         adaptive.Policy
}

type adaptiveBenchmarkOwnedSnapshot struct {
	ownerID  uuid.UUID
	snapshot *adaptive.PolicySnapshot
}

type adaptiveBenchmarkDecisionBundle struct {
	id             uuid.UUID
	sha256         string
	manifestJSON   []byte
	runID          uuid.UUID
	primaryOwnerID uuid.UUID
	controlOwnerID uuid.UUID
	providerID     string
	modelID        string
	policyVersion  string
	policySHA256   string
	entries        []adaptiveBenchmarkDecisionBundleEntry
	byOwnerDomain  map[uuid.UUID]map[adaptive.Domain]adaptive.Policy
	bySnapshot     map[uuid.UUID]adaptiveBenchmarkOwnedSnapshot
}

type adaptiveBenchmarkPolicyReader struct {
	policy adaptive.Policy
	ok     bool
}

func (reader adaptiveBenchmarkPolicyReader) CurrentPolicy(
	context.Context,
) (adaptive.Policy, error) {
	if !reader.ok {
		return adaptive.Policy{}, errors.New(
			"benchmark offline policy is unavailable",
		)
	}
	policy := reader.policy
	policy.Config = append(json.RawMessage(nil), policy.Config...)
	return policy, nil
}

func buildAdaptiveBenchmarkDecisionBundle(
	runID uuid.UUID,
	ownerID uuid.UUID,
	providerID string,
	modelID string,
	observed adaptive.Policy,
) (*adaptiveBenchmarkDecisionBundle, error) {
	controlOwnerID := uuid.MustParse(identityctx.CLIServiceIdentity)
	if runID == uuid.Nil ||
		ownerID == uuid.Nil ||
		ownerID == controlOwnerID ||
		strings.TrimSpace(providerID) != providerID ||
		providerID == "" ||
		strings.TrimSpace(modelID) != modelID ||
		modelID == "" ||
		!validAdaptiveBootPolicy(observed) ||
		observed.UpdatedAt.IsZero() {
		return nil, errors.New(
			"adaptive benchmark decision bundle scope is invalid",
		)
	}
	policySHA256, err := adaptiveBenchmarkPolicySHA256(observed)
	if err != nil {
		return nil, err
	}
	cutoff := observed.UpdatedAt.UTC().Truncate(time.Microsecond)
	bundle := &adaptiveBenchmarkDecisionBundle{
		runID:          runID,
		primaryOwnerID: ownerID,
		controlOwnerID: controlOwnerID,
		providerID:     providerID,
		modelID:        modelID,
		policyVersion:  observed.Version,
		policySHA256:   policySHA256,
		entries: make(
			[]adaptiveBenchmarkDecisionBundleEntry,
			0,
			10,
		),
		byOwnerDomain: make(
			map[uuid.UUID]map[adaptive.Domain]adaptive.Policy,
			2,
		),
		bySnapshot: make(
			map[uuid.UUID]adaptiveBenchmarkOwnedSnapshot,
			10,
		),
	}
	manifest := adaptiveBenchmarkDecisionBundleManifest{
		SchemaID:       adaptiveBenchmarkDecisionBundleSchemaID,
		Revision:       adaptiveBenchmarkDecisionBundleRevision,
		RunID:          runID.String(),
		PrimaryOwnerID: ownerID.String(),
		ControlOwnerID: controlOwnerID.String(),
		ProviderID:     providerID, ModelID: modelID,
		Seed:        auraeval.AdaptiveBenchmarkSeed,
		PolicyEpoch: observed.Epoch, PolicyVersion: observed.Version,
		PolicySHA256:   policySHA256,
		TrainingCutoff: cutoff.Format(time.RFC3339Nano),
		Entries: make(
			[]adaptiveBenchmarkBundleManifestEntry,
			0,
			10,
		),
	}
	for _, entryOwnerID := range []uuid.UUID{
		ownerID,
		controlOwnerID,
	} {
		bundle.byOwnerDomain[entryOwnerID] = make(
			map[adaptive.Domain]adaptive.Policy,
			5,
		)
		for _, catalog := range auraeval.AdaptiveBenchmarkActionCatalogs() {
			entry, manifestEntry, buildErr :=
				buildAdaptiveBenchmarkBundleEntry(
					runID,
					entryOwnerID,
					providerID,
					modelID,
					observed,
					cutoff,
					catalog,
				)
			if buildErr != nil {
				return nil, buildErr
			}
			bundle.entries = append(bundle.entries, entry)
			bundle.byOwnerDomain[entryOwnerID][entry.Domain] =
				entry.Policy
			bundle.bySnapshot[entry.SnapshotID] =
				adaptiveBenchmarkOwnedSnapshot{
					ownerID:  entryOwnerID,
					snapshot: entry.Snapshot,
				}
			manifest.Entries = append(
				manifest.Entries,
				manifestEntry,
			)
		}
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf(
			"marshal adaptive benchmark decision bundle: %w",
			err,
		)
	}
	sum := sha256.Sum256(payload)
	bundle.id = uuid.NewHash(
		sha256.New(),
		adaptiveBenchmarkDecisionBundleNamespace,
		sum[:],
		5,
	)
	bundle.sha256 = hex.EncodeToString(sum[:])
	bundle.manifestJSON = payload
	return bundle, nil
}

func buildAdaptiveBenchmarkBundleEntry(
	runID uuid.UUID,
	ownerID uuid.UUID,
	providerID string,
	modelID string,
	observed adaptive.Policy,
	cutoff time.Time,
	catalog auraeval.AdaptiveBenchmarkActionCatalog,
) (
	adaptiveBenchmarkDecisionBundleEntry,
	adaptiveBenchmarkBundleManifestEntry,
	error,
) {
	point, features, err := adaptiveBenchmarkDomainSnapshotShape(
		catalog.Domain,
	)
	if err != nil {
		return adaptiveBenchmarkDecisionBundleEntry{},
			adaptiveBenchmarkBundleManifestEntry{},
			err
	}
	actions := make([]adaptive.SnapshotAction, len(catalog.ActionIDs))
	for index, actionID := range catalog.ActionIDs {
		actions[index] = adaptive.SnapshotAction{
			ActionID: actionID,
			Examples: make([]adaptive.SnapshotExample, 0),
		}
	}
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID: ownerID, Domain: catalog.Domain, Point: point,
			ProviderID: providerID, ModelID: modelID,
			PolicyVersion: observed.Version, TrainingCutoff: cutoff,
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema:    features,
		Actions:          actions,
	})
	if err != nil {
		return adaptiveBenchmarkDecisionBundleEntry{},
			adaptiveBenchmarkBundleManifestEntry{},
			fmt.Errorf(
				"build %s offline snapshot: %w",
				catalog.Domain,
				err,
			)
	}
	cohortIdentity := runID.String() + "\x00" +
		ownerID.String() + "\x00" + string(catalog.Domain)
	cohortID := uuid.NewSHA1(
		adaptiveBenchmarkDecisionBundleNamespace,
		[]byte(cohortIdentity),
	)
	cohortSum := sha256.Sum256(
		[]byte("offline-cohort\x00" + cohortIdentity),
	)
	cohortSHA256 := hex.EncodeToString(cohortSum[:])
	config, err := json.Marshal(struct {
		OwnerID      uuid.UUID       `json:"owner_id"`
		Domain       adaptive.Domain `json:"domain"`
		ProviderID   string          `json:"provider_id"`
		ModelID      string          `json:"model_id"`
		SnapshotID   uuid.UUID       `json:"snapshot_id"`
		SnapshotSHA  string          `json:"snapshot_sha256"`
		CohortID     uuid.UUID       `json:"cohort_id"`
		CohortSHA256 string          `json:"cohort_sha256"`
	}{
		OwnerID: ownerID, Domain: catalog.Domain,
		ProviderID: providerID, ModelID: modelID,
		SnapshotID: snapshot.ID(), SnapshotSHA: snapshot.SHA256(),
		CohortID: cohortID, CohortSHA256: cohortSHA256,
	})
	if err != nil {
		return adaptiveBenchmarkDecisionBundleEntry{},
			adaptiveBenchmarkBundleManifestEntry{},
			err
	}
	policy := adaptive.Policy{
		Epoch: observed.Epoch, Version: observed.Version,
		Mode: adaptive.PolicyShadow, RolloutBPS: 0,
		Config: config, UpdatedAt: observed.UpdatedAt,
	}
	return adaptiveBenchmarkDecisionBundleEntry{
			OwnerID: ownerID, Domain: catalog.Domain, Point: point,
			SnapshotID:     snapshot.ID(),
			SnapshotSHA256: snapshot.SHA256(),
			Snapshot:       snapshot, Policy: policy,
		},
		adaptiveBenchmarkBundleManifestEntry{
			OwnerID: ownerID.String(),
			Domain:  catalog.Domain, Point: point,
			SnapshotID:     snapshot.ID().String(),
			SnapshotSHA256: snapshot.SHA256(),
			CohortID:       cohortID.String(), CohortSHA256: cohortSHA256,
		},
		nil
}

func adaptiveBenchmarkDomainSnapshotShape(
	domain adaptive.Domain,
) (
	adaptive.DecisionPoint,
	[]adaptive.SnapshotFeatureDefinition,
	error,
) {
	var point adaptive.DecisionPoint
	var keys []adaptive.FeatureKey
	switch domain {
	case adaptive.DomainReasoning:
		point = adaptive.PointReasoning
		keys = []adaptive.FeatureKey{adaptive.FeatureMessageCount}
	case adaptive.DomainToolDiscovery:
		point = adaptive.PointToolDiscovery
		keys = []adaptive.FeatureKey{
			adaptive.FeatureCandidateCount,
			adaptive.FeatureQueryLength,
			adaptive.FeatureRetrievalLimit,
		}
	case adaptive.DomainSkillRouting:
		point = adaptive.PointSkillRouting
		keys = []adaptive.FeatureKey{
			adaptive.FeatureQueryLength,
			adaptive.FeatureSkillCandidateCount,
		}
	case adaptive.DomainKnowledge:
		point = adaptive.PointKnowledge
		keys = []adaptive.FeatureKey{
			adaptive.FeatureCandidateCount,
			adaptive.FeatureQueryLength,
			adaptive.FeatureRetrievalLimit,
		}
	case adaptive.DomainMemoryRecall:
		point = adaptive.PointMemoryRecall
		keys = []adaptive.FeatureKey{
			adaptive.FeatureQueryLength,
			adaptive.FeatureRecallLimit,
		}
	default:
		return "", nil, fmt.Errorf(
			"adaptive benchmark domain %q is invalid",
			domain,
		)
	}
	features := make(
		[]adaptive.SnapshotFeatureDefinition,
		len(keys),
	)
	for index, key := range keys {
		features[index] = adaptive.SnapshotFeatureDefinition{
			Key: key, Center: 0, Scale: 1,
		}
	}
	return point, features, nil
}

func adaptiveBenchmarkPolicySHA256(
	policy adaptive.Policy,
) (string, error) {
	payload, err := json.Marshal(struct {
		Epoch      int64               `json:"epoch"`
		Version    string              `json:"version"`
		Mode       adaptive.PolicyMode `json:"mode"`
		RolloutBPS int32               `json:"rollout_bps"`
		Config     json.RawMessage     `json:"config"`
		UpdatedAt  string              `json:"updated_at"`
	}{
		Epoch: policy.Epoch, Version: policy.Version,
		Mode: policy.Mode, RolloutBPS: policy.RolloutBPS,
		Config:    policy.Config,
		UpdatedAt: policy.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", fmt.Errorf(
			"marshal authoritative adaptive policy: %w",
			err,
		)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (bundle *adaptiveBenchmarkDecisionBundle) ID() uuid.UUID {
	if bundle == nil {
		return uuid.Nil
	}
	return bundle.id
}

func (bundle *adaptiveBenchmarkDecisionBundle) SHA256() string {
	if bundle == nil {
		return ""
	}
	return bundle.sha256
}

func (bundle *adaptiveBenchmarkDecisionBundle) PrimaryOwnerID() uuid.UUID {
	if bundle == nil {
		return uuid.Nil
	}
	return bundle.primaryOwnerID
}

func (bundle *adaptiveBenchmarkDecisionBundle) ControlOwnerID() uuid.UUID {
	if bundle == nil {
		return uuid.Nil
	}
	return bundle.controlOwnerID
}

func (bundle *adaptiveBenchmarkDecisionBundle) PolicyVersion() string {
	if bundle == nil {
		return ""
	}
	return bundle.policyVersion
}

func (bundle *adaptiveBenchmarkDecisionBundle) PolicySHA256() string {
	if bundle == nil {
		return ""
	}
	return bundle.policySHA256
}

func (bundle *adaptiveBenchmarkDecisionBundle) ManifestJSON() []byte {
	if bundle == nil {
		return nil
	}
	return append([]byte(nil), bundle.manifestJSON...)
}

func (bundle *adaptiveBenchmarkDecisionBundle) Entries() []adaptiveBenchmarkDecisionBundleEntry {
	if bundle == nil {
		return nil
	}
	return append(
		[]adaptiveBenchmarkDecisionBundleEntry(nil),
		bundle.entries...,
	)
}

func (bundle *adaptiveBenchmarkDecisionBundle) PolicyReader(
	ownerID uuid.UUID,
	domain adaptive.Domain,
) adaptive.PolicyReader {
	if bundle == nil {
		return adaptiveBenchmarkPolicyReader{}
	}
	policies, ownerOK := bundle.byOwnerDomain[ownerID]
	policy, ok := policies[domain]
	ok = ownerOK && ok
	return adaptiveBenchmarkPolicyReader{policy: policy, ok: ok}
}

func (bundle *adaptiveBenchmarkDecisionBundle) Load(
	_ context.Context,
	ownerID uuid.UUID,
	snapshotID uuid.UUID,
) (*adaptive.PolicySnapshot, error) {
	if bundle == nil ||
		ownerID == uuid.Nil ||
		snapshotID == uuid.Nil {
		return nil, errors.New(
			"benchmark offline snapshot scope is invalid",
		)
	}
	owned, ok := bundle.bySnapshot[snapshotID]
	if !ok ||
		owned.ownerID != ownerID ||
		owned.snapshot == nil {
		return nil, errors.New(
			"benchmark offline snapshot is unavailable",
		)
	}
	return owned.snapshot, nil
}
