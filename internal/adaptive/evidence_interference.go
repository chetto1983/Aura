package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

// InterferencePlanArtifact freezes the conversation-cluster assumption.
type InterferencePlanArtifact struct {
	SchemaID                       string   `json:"schema_id"`
	Revision                       int      `json:"revision"`
	ClusterSchemaID                string   `json:"cluster_schema_id"`
	ClusterRevision                int      `json:"cluster_revision"`
	ClusterSchemaSHA256            string   `json:"cluster_schema_sha256"`
	ClusterKeys                    []string `json:"cluster_keys"`
	CarryoverScope                 string   `json:"carryover_scope"`
	RandomizedFocalUnitsPerCluster int      `json:"randomized_focal_units_per_cluster"`
	WithinClusterRule              string   `json:"within_cluster_rule"`
	BetweenClusterAssumption       string   `json:"between_cluster_assumption"`
	SharedStateRule                string   `json:"shared_state_rule"`
	FoldUnit                       string   `json:"fold_unit"`
	ResamplingUnit                 string   `json:"resampling_unit"`
}

// CanonicalJSON validates and emits the registered interference field order.
func (artifact InterferencePlanArtifact) CanonicalJSON() ([]byte, error) {
	if artifact.SchemaID != "aura.adaptive.interference-plan/v1" ||
		artifact.Revision != 1 ||
		artifact.ClusterSchemaID !=
			"aura.adaptive.interference-cluster/conversation/v1" ||
		artifact.ClusterRevision != 1 ||
		!validSHA256(artifact.ClusterSchemaSHA256) ||
		!slices.Equal(
			artifact.ClusterKeys,
			[]string{"owner_id", "evaluation_conversation_id"},
		) ||
		artifact.CarryoverScope != "conversation" ||
		artifact.RandomizedFocalUnitsPerCluster != 1 ||
		artifact.WithinClusterRule != "arbitrary_carryover_one_focal" ||
		artifact.BetweenClusterAssumption !=
			"no_treatment_induced_cross_conversation_interference" ||
		artifact.SharedStateRule != "read_only_or_versioned" ||
		artifact.FoldUnit != "interference_cluster" ||
		artifact.ResamplingUnit != "interference_cluster" {
		return nil, errors.New("adaptive interference plan is invalid")
	}
	return json.Marshal(artifact)
}

// DecodeInterferencePlanArtifact rejects malformed or noncanonical bytes.
func DecodeInterferencePlanArtifact(
	payload []byte,
) (InterferencePlanArtifact, error) {
	var artifact InterferencePlanArtifact
	if err := decodeStrictEvidence(payload, &artifact); err != nil {
		return InterferencePlanArtifact{}, err
	}
	canonical, err := artifact.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, payload) {
		return InterferencePlanArtifact{}, errors.New(
			"adaptive interference plan is noncanonical",
		)
	}
	return artifact, nil
}
