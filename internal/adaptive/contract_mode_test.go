package adaptive

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestAssignmentAllowsPromotedNonStaticChampion(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	assignment.ChampionActionID = "candidate"
	assignment.RecommendedActionID = "candidate"
	setDeterministicAction(&assignment, "candidate")

	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	if event.Kind != EventDecision {
		t.Fatalf("Kind = %q, want %q", event.Kind, EventDecision)
	}
}

func TestAssignmentAcceptsEveryServingModeContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		build func() Assignment
	}{
		{name: "shadow promoted champion", build: func() Assignment {
			assignment := validAssignment()
			assignment.ChampionActionID = "candidate"
			assignment.RecommendedActionID = "candidate"
			setDeterministicAction(&assignment, "candidate")
			return assignment
		}},
		{name: "canary focal randomized", build: func() Assignment {
			assignment := validAssignment()
			makeCanary(&assignment)
			return assignment
		}},
		{name: "canary non-focal diagnostic", build: func() Assignment {
			assignment := validAssignment()
			assignment.PolicyMode = PolicyCanary
			assignment.SelectionReason = SelectionCanaryDiagnostic
			assignment.ChampionActionID = "candidate"
			assignment.RecommendedActionID = "candidate"
			setDeterministicAction(&assignment, "candidate")
			return assignment
		}},
		{name: "off static", build: func() Assignment {
			assignment := validAssignment()
			assignment.PolicyMode = PolicyOff
			assignment.SelectionReason = SelectionPolicyOff
			return assignment
		}},
		{name: "rollback static", build: func() Assignment {
			assignment := validAssignment()
			assignment.PolicyMode = PolicyRollback
			assignment.SelectionReason = SelectionPolicyRollback
			return assignment
		}},
		{name: "active deterministic candidate", build: func() Assignment {
			assignment := validAssignment()
			assignment.PolicyMode = PolicyActive
			assignment.SelectionReason = SelectionActivePolicy
			assignment.RecommendedActionID = "candidate"
			setDeterministicAction(&assignment, "candidate")
			return assignment
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewAssignmentEvent(tt.build()); err != nil {
				t.Fatalf("NewAssignmentEvent: %v", err)
			}
		})
	}
}

func TestAssignmentAcceptsCanaryAndActiveFailClosedReasons(t *testing.T) {
	t.Parallel()
	for _, mode := range []PolicyMode{PolicyCanary, PolicyActive} {
		mode := mode
		for _, reason := range failClosedSelectionReasons() {
			reason := reason
			t.Run(string(mode)+"_"+string(reason), func(t *testing.T) {
				t.Parallel()
				assignment := validAssignment()
				assignment.PolicyMode = mode
				assignment.SelectionReason = reason
				assignment.ChampionActionID = "candidate"
				assignment.RecommendedActionID = "candidate"
				setDeterministicAction(&assignment, StaticActionID)
				if _, err := NewAssignmentEvent(assignment); err != nil {
					t.Fatalf("NewAssignmentEvent: %v", err)
				}
			})
		}
	}
}

func TestAssignmentAcceptsShadowFailClosedReasons(t *testing.T) {
	t.Parallel()
	for _, reason := range failClosedSelectionReasons() {
		reason := reason
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			assignment.SelectionReason = reason
			assignment.ChampionActionID = "candidate"
			assignment.RecommendedActionID = "candidate"
			setDeterministicAction(&assignment, StaticActionID)
			if _, err := NewAssignmentEvent(assignment); err != nil {
				t.Fatalf("NewAssignmentEvent: %v", err)
			}
		})
	}
}

func TestAssignmentAcceptsActiveExogenousOverride(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	assignment.PolicyMode = PolicyActive
	assignment.Override = true
	assignment.SelectionReason = SelectionOperatorOverride
	assignment.RecommendedActionID = "candidate"
	setDeterministicAction(&assignment, "candidate")

	if _, err := NewAssignmentEvent(assignment); err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	for _, mutate := range []func(*Assignment){
		func(a *Assignment) { a.ExperimentID = "experiment-1" },
		func(a *Assignment) { a.ArmID = "candidate" },
		func(a *Assignment) {
			probability := 0.5
			a.ArmProbability = &probability
		},
	} {
		invalid := assignment
		mutate(&invalid)
		if _, err := NewAssignmentEvent(invalid); err == nil {
			t.Fatal("NewAssignmentEvent accepted randomized semantics on active override")
		}
	}
}

func TestAssignmentRejectsInvalidModeClaims(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Assignment)
	}{
		{name: "off wrong reason", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyOff
			a.SelectionReason = SelectionPolicyRollback
		}},
		{name: "off non-static", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyOff
			a.SelectionReason = SelectionPolicyOff
			setDeterministicAction(a, "candidate")
		}},
		{name: "rollback wrong reason", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyRollback
			a.SelectionReason = SelectionPolicyOff
		}},
		{name: "rollback non-static", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyRollback
			a.SelectionReason = SelectionPolicyRollback
			setDeterministicAction(a, "candidate")
		}},
		{name: "active wrong reason", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyActive
			a.SelectionReason = SelectionShadowStatic
		}},
		{name: "active randomized probabilities", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyActive
			a.SelectionReason = SelectionActivePolicy
			a.ActionProbabilities[0].Probability = 0.5
			a.ActionProbabilities[1].Probability = 0.5
		}},
		{name: "canary diagnostic cohort", mutate: func(a *Assignment) {
			cohortID := uuid.Must(uuid.NewV7())
			a.PolicyMode = PolicyCanary
			a.SelectionReason = SelectionCanaryDiagnostic
			a.CohortID = &cohortID
		}},
		{name: "canary diagnostic non-champion", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyCanary
			a.SelectionReason = SelectionCanaryDiagnostic
			a.ChampionActionID = "candidate"
			setDeterministicAction(a, StaticActionID)
		}},
		{name: "canary fail-closed non-static", mutate: func(a *Assignment) {
			a.PolicyMode = PolicyCanary
			a.SelectionReason = SelectionStateMissing
			setDeterministicAction(a, "candidate")
		}},
		{name: "shadow fail-closed non-static", mutate: func(a *Assignment) {
			a.SelectionReason = SelectionStateMissing
			a.ChampionActionID = "candidate"
			setDeterministicAction(a, "candidate")
		}},
		{name: "shadow fail-closed arm", mutate: func(a *Assignment) {
			a.SelectionReason = SelectionStateMissing
			a.ArmID = "candidate"
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			tt.mutate(&assignment)
			if _, err := NewAssignmentEvent(assignment); err == nil {
				t.Fatal("NewAssignmentEvent error = nil, want invalid mode rejection")
			}
		})
	}
}

func TestAssignmentRejectsPartialRandomClaimsOutsideFocalCanary(t *testing.T) {
	t.Parallel()
	modes := []struct {
		mode   PolicyMode
		reason SelectionReason
	}{
		{mode: PolicyOff, reason: SelectionPolicyOff},
		{mode: PolicyRollback, reason: SelectionPolicyRollback},
		{mode: PolicyActive, reason: SelectionActivePolicy},
		{mode: PolicyCanary, reason: SelectionCanaryDiagnostic},
	}
	claims := []struct {
		name   string
		mutate func(*Assignment)
	}{
		{name: "experiment only", mutate: func(a *Assignment) { a.ExperimentID = "experiment-1" }},
		{name: "arm only", mutate: func(a *Assignment) { a.ArmID = "candidate" }},
		{name: "probability only", mutate: func(a *Assignment) {
			probability := 0.5
			a.ArmProbability = &probability
		}},
	}
	for _, mode := range modes {
		mode := mode
		for _, claim := range claims {
			claim := claim
			t.Run(string(mode.mode)+"_"+claim.name, func(t *testing.T) {
				t.Parallel()
				assignment := validAssignment()
				assignment.PolicyMode = mode.mode
				assignment.SelectionReason = mode.reason
				claim.mutate(&assignment)
				if _, err := NewAssignmentEvent(assignment); err == nil {
					t.Fatal("NewAssignmentEvent error = nil, want partial random claim rejection")
				}
			})
		}
	}
}

func TestDeliveryAcceptsTypedMemoryResultsAndPerTypeLimits(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	delivery := coherentMemoryDelivery(assignment.AssignmentID)

	event, err := NewDeliveryEvent(assignment.OwnerID, assignment.RequestID, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	var payload Delivery
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delivery: %v", err)
	}
	if !slices.Equal(payload.ResultIDs, delivery.ResultIDs) {
		t.Fatalf("ordered typed result IDs = %#v, want %#v", payload.ResultIDs, delivery.ResultIDs)
	}
}

func TestDeliveryRejectsIncoherentMemoryMetadata(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	tests := []struct {
		name   string
		mutate func(*Delivery)
	}{
		{name: "missing requested k", mutate: func(d *Delivery) {
			delete(d.EffectiveLimits, "entity_requested_k")
		}},
		{name: "missing effective k", mutate: func(d *Delivery) {
			delete(d.EffectiveLimits, "preference_effective_k")
		}},
		{name: "missing count", mutate: func(d *Delivery) {
			delete(d.EffectiveLimits, "message_count")
		}},
		{name: "requested below effective", mutate: func(d *Delivery) {
			d.EffectiveLimits["entity_requested_k"] = 3
			d.EffectiveLimits["entity_effective_k"] = 4
		}},
		{name: "effective below count", mutate: func(d *Delivery) {
			d.EffectiveLimits["preference_effective_k"] = 0
		}},
		{name: "count differs from typed IDs", mutate: func(d *Delivery) {
			d.EffectiveLimits["message_count"] = 2
		}},
		{name: "per-type total differs from result count", mutate: func(d *Delivery) {
			d.ResultCount = 5
		}},
		{name: "memory result mixed with generic node", mutate: func(d *Delivery) {
			d.ResultIDs = append(d.ResultIDs, ResultID{Kind: ResultNode, ID: "document-node-1"})
			d.ResultCount = 5
		}},
		{name: "metadata without matching result kind", mutate: func(d *Delivery) {
			d.ResultIDs = d.ResultIDs[1:]
			d.ResultCount = 3
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			delivery := coherentMemoryDelivery(assignment.AssignmentID)
			tt.mutate(&delivery)
			if _, err := NewDeliveryEvent(assignment.OwnerID, assignment.RequestID, delivery); err == nil {
				t.Fatal("NewDeliveryEvent error = nil, want incoherent memory metadata rejection")
			}
		})
	}
}

func TestDeliveryRejectsPartialMemoryMetadataWithoutTypedResults(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	delivery := validDelivery(assignment.AssignmentID)
	delivery.EffectiveLimits["entity_requested_k"] = 4

	if _, err := NewDeliveryEvent(assignment.OwnerID, assignment.RequestID, delivery); err == nil {
		t.Fatal("NewDeliveryEvent accepted partial memory metadata without typed results")
	}
}

func TestDeliveryKeepsNonMemoryResultCountIndependent(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	delivery := validDelivery(assignment.AssignmentID)
	delivery.ResultCount = 5
	delivery.ResultIDs = []ResultID{{Kind: ResultNode, ID: "document-node-1"}}

	if _, err := NewDeliveryEvent(assignment.OwnerID, assignment.RequestID, delivery); err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
}

func TestDeliveryRejectsUnknownMemoryKindAndLimit(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	tests := []struct {
		name   string
		mutate func(*Delivery)
	}{
		{name: "kind", mutate: func(d *Delivery) {
			d.ResultIDs[0].Kind = ResultKind("memory_fact")
		}},
		{name: "limit", mutate: func(d *Delivery) {
			d.EffectiveLimits["memory_fact_requested_k"] = 1
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			delivery := validDelivery(assignment.AssignmentID)
			tt.mutate(&delivery)
			if _, err := NewDeliveryEvent(assignment.OwnerID, assignment.RequestID, delivery); err == nil {
				t.Fatal("NewDeliveryEvent error = nil, want unknown memory metadata rejection")
			}
		})
	}
}

func TestDeliveryAcceptsRegisteredServingRevisionsAndContextBudgetFallback(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	delivery := validDelivery(assignment.AssignmentID)
	delivery.ActualActionID = ActionNoneID
	delivery.FallbackReason = FallbackContextBudget
	delivery.Revisions = map[string]string{
		"corpus":    "corpus-v1",
		"embedding": "embedding-v2",
		"index":     "index-v3",
		"reranker":  "reranker-v4",
		"retriever": "retriever-v5",
		"schema":    "schema-v6",
	}

	if _, err := NewDeliveryEvent(assignment.OwnerID, assignment.RequestID, delivery); err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
}

func coherentMemoryDelivery(assignmentID uuid.UUID) Delivery {
	delivery := validDelivery(assignmentID)
	delivery.ResultCount = 4
	delivery.ResultIDs = []ResultID{
		{Kind: ResultMemoryEntity, ID: "entity-1"},
		{Kind: ResultMemoryPreference, ID: "preference-1"},
		{Kind: ResultMemoryMessage, ID: "message-1"},
		{Kind: ResultMemoryReasoningTrace, ID: "trace-1"},
	}
	delivery.EffectiveLimits = map[string]int{
		"entity_requested_k":          5,
		"entity_effective_k":          4,
		"entity_count":                1,
		"preference_requested_k":      4,
		"preference_effective_k":      3,
		"preference_count":            1,
		"message_requested_k":         3,
		"message_effective_k":         2,
		"message_count":               1,
		"reasoning_trace_requested_k": 2,
		"reasoning_trace_effective_k": 1,
		"reasoning_trace_count":       1,
	}
	return delivery
}

func setDeterministicAction(assignment *Assignment, intendedActionID string) {
	assignment.IntendedActionID = intendedActionID
	for i := range assignment.ActionProbabilities {
		probability := 0.0
		if assignment.ActionProbabilities[i].ActionID == intendedActionID {
			probability = 1
		}
		assignment.ActionProbabilities[i].Probability = probability
	}
}

func failClosedSelectionReasons() []SelectionReason {
	return []SelectionReason{
		SelectionStateMissing,
		SelectionStateInvalid,
		SelectionStateStale,
		SelectionOwnerMismatch,
		SelectionProviderMismatch,
		SelectionModelMismatch,
		SelectionChecksumMismatch,
		SelectionUnsupported,
	}
}
