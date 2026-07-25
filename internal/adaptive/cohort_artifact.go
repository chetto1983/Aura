package adaptive

import (
	"encoding/json"
	"errors"
	"time"
)

// CohortSchemaVersion identifies the canonical persisted cohort artifact.
const CohortSchemaVersion = "1.0"

type canonicalCohort struct {
	SchemaVersion  string                `json:"schema_version"`
	Scope          CohortScope           `json:"scope"`
	Predicate      FocalPredicate        `json:"predicate"`
	ExperimentID   string                `json:"experiment_id"`
	Arms           []CohortArm           `json:"arms"`
	Actions        []ActionProbability   `json:"actions"`
	Evaluators     []EvaluatorProvenance `json:"evaluators"`
	PrimaryQuality CohortEndpoint        `json:"primary_quality"`
	PrimaryHarm    CohortEndpoint        `json:"primary_harm"`
	Power          CohortPowerPlan       `json:"power"`
	Margins        CohortMargins         `json:"margins"`
	Censoring      CohortCensoring       `json:"censoring"`
	Admission      CohortAdmission       `json:"admission"`
	Looks          []CohortLook          `json:"looks"`
	Cutoff         time.Time             `json:"cutoff"`
}

func marshalCanonicalCohort(spec CohortSpec) ([]byte, error) {
	evaluators := make([]EvaluatorProvenance, len(spec.Evaluators))
	for index, registration := range spec.Evaluators {
		evaluators[index] = registration.Provenance()
	}
	return json.Marshal(canonicalCohort{
		SchemaVersion:  CohortSchemaVersion,
		Scope:          spec.Scope,
		Predicate:      spec.Predicate,
		ExperimentID:   spec.ExperimentID,
		Arms:           spec.Arms,
		Actions:        spec.Actions,
		Evaluators:     evaluators,
		PrimaryQuality: spec.PrimaryQuality,
		PrimaryHarm:    spec.PrimaryHarm,
		Power:          spec.Power,
		Margins:        spec.Margins,
		Censoring:      spec.Censoring,
		Admission:      spec.Admission,
		Looks:          spec.Looks,
		Cutoff:         spec.Cutoff,
	})
}

func (cohort *FocalCohort) canonicalArtifact() ([]byte, error) {
	if !cohort.initialized() {
		return nil, errors.New("adaptive cohort is uninitialized")
	}
	return marshalCanonicalCohort(cohort.spec)
}
