package adaptive

import (
	"testing"

	"github.com/google/uuid"
)

func TestValidateAdmissionArtifactRegistrationUsesTypedCanonicalContract(
	t *testing.T,
) {
	t.Parallel()
	artifact := validInterferencePlanArtifact()
	canonical, err := artifact.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	registration := EvidenceArtifactRegistration{
		Ref: ChildArtifactRef{
			SchemaID:         ChildArtifactRefSchemaID,
			Revision:         1,
			Kind:             "interference_plan",
			ArtifactSchemaID: "aura.adaptive.interference-plan/v1",
			ArtifactRevision: 1,
			ArtifactID:       uuid.Must(uuid.NewV7()).String(),
			ArtifactSHA256:   sha256Hex(canonical),
		},
		Canonical: canonical,
	}
	if err := ValidateAdmissionArtifactRegistration(registration); err != nil {
		t.Fatalf("validate canonical admission registration: %v", err)
	}
	registration.Canonical = []byte(
		`{"schema_id":"aura.adaptive.interference-plan/v1","revision":1}`,
	)
	registration.Ref.ArtifactSHA256 = sha256Hex(registration.Canonical)
	if err := ValidateAdmissionArtifactRegistration(registration); err == nil {
		t.Fatal("typed admission validator accepted missing fields")
	}
}
