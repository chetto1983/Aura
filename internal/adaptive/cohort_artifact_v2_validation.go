package adaptive

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

func validateChildArtifactRef(ref ChildArtifactRef, kind, schema string) error {
	artifactID, err := uuid.Parse(ref.ArtifactID)
	if ref.SchemaID != ChildArtifactRefSchemaID || ref.Revision != 1 ||
		ref.Kind != kind || ref.ArtifactSchemaID != schema || ref.ArtifactRevision != 1 ||
		err != nil || artifactID.String() != ref.ArtifactID || !validSHA256(ref.ArtifactSHA256) {
		return fmt.Errorf("adaptive cohort v2 %s reference is invalid", kind)
	}
	return nil
}

func validateCanonicalRandomizationPlan(
	plan canonicalRandomizationPlan,
	document canonicalCohortV2,
) error {
	if plan.SchemaID != "aura.adaptive.randomization-plan/v1" || plan.Revision != 1 ||
		plan.DesignID != "independent-bernoulli" ||
		plan.MechanismID != randomizationMechanismID || plan.MechanismRevision != 1 ||
		plan.EntropySourceID != randomizationEntropyID || plan.DrawByteCount != 1 ||
		plan.BitIndex != 0 || plan.CommonDenominator != 2 ||
		plan.AnalysisStratumSchemaID != analysisStratumSchemaID ||
		plan.AnalysisStratumRevision != 1 || !validSHA256(plan.AnalysisStratumSchemaSHA256) ||
		plan.BlockOrigin != "1970-01-01T00:00:00Z" || plan.BlockWidthSeconds == 0 ||
		len(plan.Arms) != 2 {
		return errors.New("adaptive cohort v2 randomization plan is invalid")
	}
	actionSchemaSHA256, err := hashCanonical(canonicalActionSchema{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1,
		Actions: document.SupportedActions,
	})
	if err != nil {
		return err
	}
	expected := []struct {
		armID, role, policyID string
	}{
		{"baseline", "control", "aura/static-champion"},
		{"challenger", "treatment", "aura/graph-knn-snapshot"},
	}
	for index, arm := range plan.Arms {
		rule := expected[index]
		if arm.SchemaID != "aura.adaptive.arm-policy-ref/v1" || arm.Revision != 1 ||
			arm.ArmID != rule.armID || arm.ArmRole != rule.role ||
			arm.Weight.Cmp(MustExactRational(1, 1)) != 0 ||
			arm.PolicyID != rule.policyID || arm.PolicyRevision != 1 ||
			arm.SnapshotID != document.SnapshotID ||
			arm.SnapshotSHA256 != document.SnapshotSHA256 ||
			arm.ActionSchemaSHA256 != actionSchemaSHA256 ||
			!validSHA256(arm.FeatureSchemaSHA256) ||
			arm.FeatureSchemaSHA256 == strings.Repeat("0", 64) {
			return errors.New("adaptive cohort v2 randomization arm reference is invalid")
		}
	}
	if plan.Arms[0].FeatureSchemaSHA256 != plan.Arms[1].FeatureSchemaSHA256 {
		return errors.New("adaptive cohort v2 randomization feature binding differs by arm")
	}
	return nil
}

func snapshotMatchesCohortSpec(snapshot *PolicySnapshot, spec CohortSpec) bool {
	scope := snapshot.Scope()
	return snapshot.ID() == spec.Scope.SnapshotID &&
		snapshot.SHA256() == spec.Scope.SnapshotSHA256 &&
		scope.OwnerID == spec.Scope.OwnerID && scope.Domain == spec.Predicate.Domain &&
		scope.Point == spec.Predicate.Point && scope.ProviderID == spec.Scope.ProviderID &&
		scope.ModelID == spec.Scope.ModelID && scope.PolicyVersion == spec.Scope.PolicyVersion &&
		slices.Equal(snapshotActionIDs(snapshot.Actions()), cohortActionIDs(spec.Actions))
}
