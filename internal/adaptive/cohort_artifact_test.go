package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"testing"
)

func TestFocalCohort_CanonicalArtifactMatchesIdentity(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())

	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		t.Fatalf("canonicalArtifact() error = %v", err)
	}
	sum := sha256.Sum256(artifact)
	if got := hex.EncodeToString(sum[:]); got != cohort.SHA256() {
		t.Fatalf("artifact SHA256 = %s, want %s", got, cohort.SHA256())
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(artifact, &document); err != nil {
		t.Fatalf("decode canonical artifact: %v", err)
	}
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	wantKeys := []string{
		"actions", "admission", "arms", "censoring", "cutoff", "evaluators",
		"experiment_id", "looks", "margins", "power", "predicate",
		"primary_harm", "primary_quality", "schema_version", "scope",
	}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("artifact keys = %v, want %v", keys, wantKeys)
	}

	var schemaVersion string
	if err := json.Unmarshal(document["schema_version"], &schemaVersion); err != nil {
		t.Fatalf("decode schema_version: %v", err)
	}
	if schemaVersion != CohortSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", schemaVersion, CohortSchemaVersion)
	}
	if _, ok := document["Scope"]; ok {
		t.Fatal("artifact exposed Go field names instead of canonical JSON names")
	}
}

func TestFocalCohort_CanonicalArtifactRejectsUninitializedCohort(t *testing.T) {
	var cohort FocalCohort

	if _, err := cohort.canonicalArtifact(); err == nil {
		t.Fatal("zero-value cohort produced a canonical artifact")
	}
}
