package adaptive

import (
	"bytes"
	"testing"
)

func TestInterferencePlanArtifactCanonicalRoundTrip(t *testing.T) {
	artifact := validInterferencePlanArtifact()
	canonical, err := artifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	decoded, err := DecodeInterferencePlanArtifact(canonical)
	if err != nil {
		t.Fatalf("DecodeInterferencePlanArtifact: %v", err)
	}
	if decoded.ClusterSchemaSHA256 != artifact.ClusterSchemaSHA256 ||
		!bytes.Equal(canonical, mustInterferenceCanonical(t, decoded)) {
		t.Fatal("interference plan round trip changed canonical content")
	}
	tampered := bytes.Replace(
		canonical, []byte(`"conversation"`), []byte(`"session"`), 1,
	)
	if _, err := DecodeInterferencePlanArtifact(tampered); err == nil {
		t.Fatal("DecodeInterferencePlanArtifact accepted changed carryover scope")
	}
}

func validInterferencePlanArtifact() InterferencePlanArtifact {
	return InterferencePlanArtifact{
		SchemaID: "aura.adaptive.interference-plan/v1", Revision: 1,
		ClusterSchemaID: "aura.adaptive.interference-cluster/conversation/v1",
		ClusterRevision: 1, ClusterSchemaSHA256: repeatedSHA("a"),
		ClusterKeys:    []string{"owner_id", "evaluation_conversation_id"},
		CarryoverScope: "conversation", RandomizedFocalUnitsPerCluster: 1,
		WithinClusterRule:        "arbitrary_carryover_one_focal",
		BetweenClusterAssumption: "no_treatment_induced_cross_conversation_interference",
		SharedStateRule:          "read_only_or_versioned",
		FoldUnit:                 "interference_cluster", ResamplingUnit: "interference_cluster",
	}
}

func mustInterferenceCanonical(
	t *testing.T,
	artifact InterferencePlanArtifact,
) []byte {
	t.Helper()
	canonical, err := artifact.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
