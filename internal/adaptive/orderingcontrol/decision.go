// Package orderingcontrol binds read-only discovery ports to schema-2 facts.
//
// This file holds what every ordering domain shares: the decision record, the
// event-recorder port, and the validation and diagnostics helpers. The
// tool-DISCOVERY domain that these were first written for is gone — tool_search
// resolves names and ranks lexically now, so there is one strategy and nothing to
// assign between. Skill routing, recall, retrieval and source ordering remain.
package orderingcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

type toolDecision struct {
	Policy            adaptive.Policy
	Snapshot          *adaptive.PolicySnapshot
	Environment       adaptive.EvaluationEnvironment
	RecommendedAction string
	ProviderID        string
	ModelID           string
}

// EventRecorder persists typed assignment and delivery facts.
type EventRecorder interface {
	RecordAssignment(
		context.Context,
		adaptive.Assignment,
	) (int64, error)
	RecordDelivery(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		adaptive.Delivery,
	) (int64, error)
}

func validateOrderingDecision(
	ownerID uuid.UUID,
	domain adaptive.Domain,
	point adaptive.DecisionPoint,
	actionCatalog []string,
	decision toolDecision,
) error {
	if decision.Policy.Epoch <= 0 ||
		decision.Policy.Version == "" ||
		decision.Snapshot == nil {
		return errors.New("adaptive ordering decision state is incomplete")
	}
	scope := decision.Snapshot.Scope()
	if scope.OwnerID != ownerID ||
		scope.Domain != domain ||
		scope.Point != point ||
		scope.ProviderID != decision.ProviderID ||
		scope.ModelID != decision.ModelID ||
		scope.PolicyVersion != decision.Policy.Version {
		return errors.New(
			"adaptive ordering snapshot scope does not match",
		)
	}
	if decision.Snapshot.ChampionActionID() !=
		adaptive.StaticActionID {
		return errors.New(
			"adaptive ordering snapshot champion is not static",
		)
	}
	actions := decision.Snapshot.Actions()
	actionIDs := make([]string, len(actions))
	for index, action := range actions {
		actionIDs[index] = action.ActionID
	}
	if !slices.Equal(actionIDs, actionCatalog) {
		return errors.New(
			"adaptive ordering snapshot action catalog is not frozen",
		)
	}
	return nil
}

func diagnosticSelectionReason(
	mode adaptive.PolicyMode,
) (adaptive.SelectionReason, error) {
	switch mode {
	case adaptive.PolicyShadow:
		return adaptive.SelectionShadowStatic, nil
	case adaptive.PolicyCanary:
		return adaptive.SelectionCanaryDiagnostic, nil
	case adaptive.PolicyActive:
		return adaptive.SelectionUnsupported, nil
	default:
		return "", fmt.Errorf(
			"adaptive policy mode %q cannot emit a discovery assignment",
			mode,
		)
	}
}

func deterministicActionProbabilities(
	actions []string,
	selected string,
) []adaptive.ActionProbability {
	probabilities := make([]adaptive.ActionProbability, len(actions))
	for index, action := range actions {
		probability := float64(0)
		if action == selected {
			probability = 1
		}
		probabilities[index] = adaptive.ActionProbability{
			ActionID: action, Probability: probability,
		}
	}
	return probabilities
}

func actionCatalogHashes(
	actions []string,
	probabilities []adaptive.ActionProbability,
) (string, string, error) {
	eligibility, err := json.Marshal(actions)
	if err != nil {
		return "", "", err
	}
	catalog, err := json.Marshal(probabilities)
	if err != nil {
		return "", "", err
	}
	eligibilitySum := sha256.Sum256(eligibility)
	catalogSum := sha256.Sum256(catalog)
	return hex.EncodeToString(eligibilitySum[:]),
		hex.EncodeToString(catalogSum[:]),
		nil
}
