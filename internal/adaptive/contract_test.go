package adaptive

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAssignmentIDForPointIsDeterministicAndScoped(t *testing.T) {
	t.Parallel()
	owner := uuid.MustParse("17fc3d72-985a-4aa7-a80f-bc8e0f27d88c")
	request := uuid.MustParse("690d460e-6375-4637-ab93-59e07b2d81d6")

	first := mustAssignmentID(owner, request, PointReasoning, 2)
	second := mustAssignmentID(owner, request, PointReasoning, 2)
	if first == uuid.Nil || first != second {
		t.Fatalf("AssignmentIDForPoint deterministic ID = %s then %s", first, second)
	}
	changes := []uuid.UUID{
		mustAssignmentID(uuid.Must(uuid.NewV7()), request, PointReasoning, 2),
		mustAssignmentID(owner, uuid.Must(uuid.NewV7()), PointReasoning, 2),
		mustAssignmentID(owner, request, PointToolDiscovery, 2),
		mustAssignmentID(owner, request, PointReasoning, 3),
	}
	for i, changed := range changes {
		if changed == first {
			t.Fatalf("scope change %d did not change assignment ID", i)
		}
	}
}

func TestAssignmentEventIDIsDeterministicByKindAndSource(t *testing.T) {
	t.Parallel()
	assignmentID := uuid.MustParse("278b27e8-e53c-4fbc-9e40-ddb359cb3b7f")

	first := mustEventID(assignmentID, EventDecision, "assignment")
	second := mustEventID(assignmentID, EventDecision, "assignment")
	if first == uuid.Nil || first != second {
		t.Fatalf("EventIDForSource deterministic ID = %s then %s", first, second)
	}
	if mustEventID(assignmentID, EventDelivery, "assignment") == first {
		t.Fatal("event kind did not affect deterministic event ID")
	}
	if mustEventID(assignmentID, EventDecision, "retry-2") == first {
		t.Fatal("stable source identity did not affect deterministic event ID")
	}
}

func TestAssignmentConstructorProducesCanonicalSchema2Event(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()

	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	if event.Kind != EventDecision {
		t.Fatalf("Kind = %q, want %q", event.Kind, EventDecision)
	}
	if event.ID != mustEventID(assignment.AssignmentID, EventDecision, "assignment") {
		t.Fatalf("ID = %s, want deterministic assignment event ID", event.ID)
	}
	if event.OwnerID != assignment.OwnerID || event.DecisionID != assignment.AssignmentID {
		t.Fatalf("event binding = (%s, %s), want (%s, %s)",
			event.OwnerID, event.DecisionID, assignment.OwnerID, assignment.AssignmentID)
	}
	if event.AggregateID != assignment.RequestID.String() {
		t.Fatalf("AggregateID = %q, want request ID %q", event.AggregateID, assignment.RequestID)
	}
	if !bytes.Equal(event.PayloadHash, payloadSHA256(event.Payload)) {
		t.Fatalf("PayloadHash = %x, want hash of canonical payload", event.PayloadHash)
	}

	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal assignment payload: %v", err)
	}
	for key, want := range map[string]any{
		"schema_version":     SchemaVersion2,
		"domain":             string(DomainReasoning),
		"point":              string(PointReasoning),
		"point_ordinal":      float64(2),
		"policy_mode":        string(PolicyShadow),
		"champion_action_id": StaticActionID,
		"intended_action_id": StaticActionID,
		"override":           false,
	} {
		if got := payload[key]; got != want {
			t.Errorf("payload[%q] = %#v, want %#v", key, got, want)
		}
	}
	if _, exists := payload["override"]; !exists {
		t.Fatal("payload omits explicit override truth")
	}
}

func TestAssignmentConstructorRejectsMissingBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Assignment)
	}{
		{name: "schema version", mutate: func(a *Assignment) { a.SchemaVersion = "1.0" }},
		{name: "assignment id", mutate: func(a *Assignment) { a.AssignmentID = uuid.Nil }},
		{name: "derived assignment id", mutate: func(a *Assignment) { a.AssignmentID = uuid.Must(uuid.NewV7()) }},
		{name: "owner", mutate: func(a *Assignment) { a.OwnerID = uuid.Nil }},
		{name: "request", mutate: func(a *Assignment) { a.RequestID = uuid.Nil }},
		{name: "domain", mutate: func(a *Assignment) { a.Domain = Domain("") }},
		{name: "domain point mismatch", mutate: func(a *Assignment) { a.Domain = DomainToolDiscovery }},
		{name: "point", mutate: func(a *Assignment) { a.Point = DecisionPoint("unknown") }},
		{name: "policy epoch", mutate: func(a *Assignment) { a.PolicyEpoch = 0 }},
		{name: "policy version", mutate: func(a *Assignment) { a.PolicyVersion = "" }},
		{name: "policy mode", mutate: func(a *Assignment) { a.PolicyMode = PolicyMode("unknown") }},
		{name: "snapshot id", mutate: func(a *Assignment) { a.SnapshotID = uuid.Nil }},
		{name: "snapshot hash", mutate: func(a *Assignment) { a.SnapshotSHA256 = "" }},
		{name: "environment", mutate: func(a *Assignment) { a.Environment = EvaluationEnvironment("unknown") }},
		{name: "provider", mutate: func(a *Assignment) { a.ProviderID = "" }},
		{name: "model", mutate: func(a *Assignment) { a.ModelID = "" }},
		{name: "eligible actions", mutate: func(a *Assignment) { a.EligibleActions = nil }},
		{name: "eligibility hash", mutate: func(a *Assignment) { a.EligibilitySHA256 = "" }},
		{name: "catalog hash", mutate: func(a *Assignment) { a.CatalogSHA256 = "" }},
		{name: "champion", mutate: func(a *Assignment) { a.ChampionActionID = "" }},
		{name: "recommendation", mutate: func(a *Assignment) { a.RecommendedActionID = "" }},
		{name: "intention", mutate: func(a *Assignment) { a.IntendedActionID = "" }},
		{name: "selection reason", mutate: func(a *Assignment) { a.SelectionReason = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			tt.mutate(&assignment)
			if _, err := NewAssignmentEvent(assignment); err == nil {
				t.Fatal("NewAssignmentEvent error = nil, want rejected binding")
			}
		})
	}
}

func TestAssignmentConstructorValidatesFrozenProbabilityCatalogs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Assignment)
	}{
		{name: "unordered actions", mutate: func(a *Assignment) {
			a.EligibleActions = []string{StaticActionID, "candidate"}
		}},
		{name: "duplicate actions", mutate: func(a *Assignment) {
			a.EligibleActions = []string{"candidate", "candidate", StaticActionID}
		}},
		{name: "missing static action", mutate: func(a *Assignment) {
			a.EligibleActions = []string{"candidate"}
			a.ActionProbabilities = []ActionProbability{{ActionID: "candidate", Probability: 1}}
		}},
		{name: "probability order mismatch", mutate: func(a *Assignment) {
			a.ActionProbabilities = []ActionProbability{
				{ActionID: StaticActionID, Probability: 1},
				{ActionID: "candidate", Probability: 0},
			}
		}},
		{name: "probability total", mutate: func(a *Assignment) {
			a.ActionProbabilities[0].Probability = 0.25
		}},
		{name: "non finite probability", mutate: func(a *Assignment) {
			a.ActionProbabilities[0].Probability = math.NaN()
		}},
		{name: "shadow selected challenger", mutate: func(a *Assignment) {
			a.IntendedActionID = "candidate"
		}},
		{name: "shadow challenger support", mutate: func(a *Assignment) {
			a.ActionProbabilities[0].Probability = 0.2
			a.ActionProbabilities[1].Probability = 0.8
		}},
		{name: "shadow experiment", mutate: func(a *Assignment) {
			a.ExperimentID = "experiment-1"
		}},
		{name: "canary missing experiment", mutate: func(a *Assignment) {
			makeCanary(a)
			a.ExperimentID = ""
		}},
		{name: "canary missing arm", mutate: func(a *Assignment) {
			makeCanary(a)
			a.ArmID = ""
		}},
		{name: "canary invalid arm probability", mutate: func(a *Assignment) {
			makeCanary(a)
			zero := 0.0
			a.ArmProbability = &zero
		}},
		{name: "canary unsupported action", mutate: func(a *Assignment) {
			makeCanary(a)
			a.ActionProbabilities[0].Probability = 0
			a.ActionProbabilities[1].Probability = 1
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			tt.mutate(&assignment)
			if _, err := NewAssignmentEvent(assignment); err == nil {
				t.Fatal("NewAssignmentEvent error = nil, want invalid catalog rejection")
			}
		})
	}
}

func TestAssignmentConstructorAcceptsFrozenCanaryProbabilityVectors(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	makeCanary(&assignment)

	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	var payload Assignment
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal assignment: %v", err)
	}
	if payload.ArmProbability == nil || *payload.ArmProbability != 0.5 {
		t.Fatalf("arm probability = %v, want 0.5", payload.ArmProbability)
	}
	if len(payload.ActionProbabilities) != 2 ||
		payload.ActionProbabilities[0].Probability != 0.5 ||
		payload.ActionProbabilities[1].Probability != 0.5 {
		t.Fatalf("action probabilities = %#v, want frozen 0.5/0.5 vector", payload.ActionProbabilities)
	}
}

func TestAssignmentConstructorPreservesExplicitOverride(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	assignment.Override = true
	assignment.SelectionReason = "operator_override"

	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	var payload Assignment
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal assignment: %v", err)
	}
	if !payload.Override || payload.SelectionReason != "operator_override" {
		t.Fatalf("override payload = (%t, %q), want explicit operator override",
			payload.Override, payload.SelectionReason)
	}
}

func TestAssignmentConstructorAcceptsExogenousCanaryOverride(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	assignment.PolicyMode = PolicyCanary
	assignment.Override = true
	assignment.RecommendedActionID = "candidate"
	assignment.IntendedActionID = "candidate"
	assignment.ActionProbabilities[0].Probability = 1
	assignment.ActionProbabilities[1].Probability = 0
	assignment.SelectionReason = SelectionOperatorOverride

	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	var payload Assignment
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal assignment: %v", err)
	}
	if payload.ExperimentID != "" || payload.ArmID != "" || payload.ArmProbability != nil {
		t.Fatalf("override claimed randomized assignment: %#v", payload)
	}
	if !payload.Override || payload.SelectionReason != SelectionOperatorOverride {
		t.Fatalf("override truth = (%t, %q), want explicit operator override",
			payload.Override, payload.SelectionReason)
	}
}

func TestAssignmentConstructorRejectsUnsafeIDsAndUnregisteredReasons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Assignment)
	}{
		{name: "policy version unicode", mutate: func(a *Assignment) { a.PolicyVersion = "policy-☃" }},
		{name: "provider whitespace", mutate: func(a *Assignment) { a.ProviderID = "open router" }},
		{name: "model control", mutate: func(a *Assignment) { a.ModelID = "model\nsecret" }},
		{name: "model too long", mutate: func(a *Assignment) { a.ModelID = strings.Repeat("m", 257) }},
		{name: "action unsafe", mutate: func(a *Assignment) {
			a.EligibleActions[0] = "candidate\n"
			a.RecommendedActionID = "candidate\n"
			a.ActionProbabilities[0].ActionID = "candidate\n"
		}},
		{name: "selection prose", mutate: func(a *Assignment) {
			a.SelectionReason = SelectionReason("operator said use the secret answer")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			tt.mutate(&assignment)
			if _, err := NewAssignmentEvent(assignment); err == nil {
				t.Fatal("NewAssignmentEvent error = nil, want bounded registry rejection")
			}
		})
	}
}

func TestAssignmentFeatureRegistryRejectsFingerprintAliases(t *testing.T) {
	t.Parallel()
	privateAliases := []string{
		"response_fingerprint",
		"memory_fingerprint_sha256",
		"content_sha256",
		"payload_digest",
		"result_checksum",
		"document_content_hash",
	}
	for _, key := range privateAliases {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			assignment.Features = map[FeatureKey]float64{FeatureKey(key): 1}
			if _, err := NewAssignmentEvent(assignment); err == nil {
				t.Fatalf("NewAssignmentEvent accepted fingerprint alias %q", key)
			}
		})
	}
	assignment := validAssignment()
	assignment.Features = map[FeatureKey]float64{
		"prompt_length":         400,
		"available_tool_count":  12,
		"available_skill_count": 8,
		"document_count":        5,
		"memory_count":          3,
	}
	if _, err := NewAssignmentEvent(assignment); err != nil {
		t.Fatalf("NewAssignmentEvent registered safe lengths/counts: %v", err)
	}
	assignment.Features[FeatureKey("unregistered_count")] = 1
	if _, err := NewAssignmentEvent(assignment); err == nil {
		t.Fatal("NewAssignmentEvent accepted unregistered numeric feature")
	}
}

func TestDeliveryConstructorRecordsIntendedActualStatusAndExposure(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	delivery := validDelivery(assignment.AssignmentID)

	event, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	if event.Kind != EventDelivery {
		t.Fatalf("Kind = %q, want %q", event.Kind, EventDelivery)
	}
	if event.ID != mustEventID(delivery.AssignmentID, EventDelivery, "assignment") {
		t.Fatalf("ID = %s, want deterministic delivery event ID", event.ID)
	}
	if event.DecisionID != assignment.AssignmentID ||
		event.OwnerID != assignment.OwnerID ||
		event.AggregateID != assignment.RequestID.String() {
		t.Fatalf("delivery event has incorrect assignment binding: %#v", event)
	}

	var payload Delivery
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delivery payload: %v", err)
	}
	if payload.IntendedActionID != StaticActionID ||
		payload.ActualActionID != "candidate" ||
		payload.Status != DeliveryFallback {
		t.Fatalf("delivery action/status = (%q, %q, %q)",
			payload.IntendedActionID, payload.ActualActionID, payload.Status)
	}
	if payload.ExposureKnown || payload.ExposureProbability != nil {
		t.Fatalf("exposure = (%t, %v), want explicitly unknown", payload.ExposureKnown, payload.ExposureProbability)
	}
	if payload.ResultCount != 2 || len(payload.ResultIDs) != 1 {
		t.Fatalf("result count/IDs = (%d, %d), independent count was not preserved",
			payload.ResultCount, len(payload.ResultIDs))
	}
}

func TestDeliveryConstructorRejectsUnsafeIDsReasonsAndUnregisteredMaps(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	tests := []struct {
		name   string
		mutate func(*Delivery)
	}{
		{name: "intended action", mutate: func(d *Delivery) { d.IntendedActionID = "candidate\n" }},
		{name: "actual action", mutate: func(d *Delivery) { d.ActualActionID = "stàtic" }},
		{name: "result ID", mutate: func(d *Delivery) { d.ResultIDs[0] = ResultID{} }},
		{name: "fallback prose", mutate: func(d *Delivery) {
			d.FallbackReason = FallbackReason("the model exposed private output")
		}},
		{name: "unregistered effective limit", mutate: func(d *Delivery) {
			d.EffectiveLimits["temperature"] = 1
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			delivery := validDelivery(assignment.AssignmentID)
			tt.mutate(&delivery)
			if _, err := NewDeliveryEvent(assignment, delivery); err == nil {
				t.Fatal("NewDeliveryEvent error = nil, want bounded registry rejection")
			}
		})
	}
}

func TestDeliveryConstructorValidatesBindingsAndExposure(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	tests := []struct {
		name   string
		mutate func(*Assignment, *Delivery)
	}{
		{name: "owner", mutate: func(a *Assignment, _ *Delivery) { a.OwnerID = uuid.Nil }},
		{name: "request", mutate: func(a *Assignment, _ *Delivery) { a.RequestID = uuid.Nil }},
		{name: "schema", mutate: func(_ *Assignment, d *Delivery) { d.SchemaVersion = "1.0" }},
		{name: "assignment", mutate: func(_ *Assignment, d *Delivery) { d.AssignmentID = uuid.Nil }},
		{name: "intended action", mutate: func(_ *Assignment, d *Delivery) { d.IntendedActionID = "" }},
		{name: "actual action", mutate: func(_ *Assignment, d *Delivery) { d.ActualActionID = "" }},
		{name: "status", mutate: func(_ *Assignment, d *Delivery) { d.Status = DeliveryStatus("unknown") }},
		{name: "fallback reason", mutate: func(_ *Assignment, d *Delivery) { d.FallbackReason = "" }},
		{name: "unknown exposure with probability", mutate: func(_ *Assignment, d *Delivery) {
			probability := 0.5
			d.ExposureProbability = &probability
		}},
		{name: "known exposure without probability", mutate: func(_ *Assignment, d *Delivery) {
			d.ExposureKnown = true
		}},
		{name: "known invalid exposure", mutate: func(_ *Assignment, d *Delivery) {
			probability := 1.1
			d.ExposureKnown = true
			d.ExposureProbability = &probability
		}},
		{name: "negative result count", mutate: func(_ *Assignment, d *Delivery) { d.ResultCount = -1 }},
		{name: "more result IDs than results", mutate: func(_ *Assignment, d *Delivery) {
			d.ResultCount = 0
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			boundAssignment := assignment
			delivery := validDelivery(assignment.AssignmentID)
			tt.mutate(&boundAssignment, &delivery)
			if _, err := NewDeliveryEvent(boundAssignment, delivery); err == nil {
				t.Fatal("NewDeliveryEvent error = nil, want rejected binding")
			}
		})
	}
}

func TestDeliveryConstructorAcceptsKnownExposureAndSuccess(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	makeCanary(&assignment)
	probability := 0.5
	delivery := validDelivery(assignment.AssignmentID)
	delivery.IntendedActionID = "candidate"
	delivery.ActualActionID = "candidate"
	delivery.Status = DeliverySuccess
	delivery.ExposureKnown = true
	delivery.ExposureProbability = &probability
	delivery.FallbackReason = ""

	event, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	var payload Delivery
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delivery: %v", err)
	}
	if !payload.ExposureKnown || payload.ExposureProbability == nil ||
		*payload.ExposureProbability != probability {
		t.Fatalf("known exposure = (%t, %v), want %v",
			payload.ExposureKnown, payload.ExposureProbability, probability)
	}
}

func TestPrivatePayloadKeysAreRejectedBeforeSerialization(t *testing.T) {
	t.Parallel()
	privateKeys := []string{
		"raw_prompt",
		"query",
		"tool_arguments",
		"document_content",
		"memory_content",
		"credentials",
		"chain_of_thought",
		"content_fingerprint",
	}
	for _, key := range privateKeys {
		t.Run("assignment_"+key, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			assignment.Features[FeatureKey(key)] = 1
			if _, err := NewAssignmentEvent(assignment); err == nil {
				t.Fatalf("NewAssignmentEvent accepted private feature key %q", key)
			}
		})
		t.Run("delivery_"+key, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			delivery := validDelivery(assignment.AssignmentID)
			privateKind := RevisionKind(key)
			delivery.Revisions = RevisionSet{values: map[RevisionKind]Revision{
				privateKind: {kind: privateKind, value: "forbidden", validated: true},
			}}
			if _, err := NewDeliveryEvent(assignment, delivery); err == nil {
				t.Fatalf("NewDeliveryEvent accepted private revision key %q", key)
			}
		})
	}
}

func validAssignment() Assignment {
	owner := uuid.MustParse("17fc3d72-985a-4aa7-a80f-bc8e0f27d88c")
	request := uuid.MustParse("690d460e-6375-4637-ab93-59e07b2d81d6")
	return Assignment{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        mustAssignmentID(owner, request, PointReasoning, 2),
		OwnerID:             owner,
		RequestID:           request,
		Domain:              DomainReasoning,
		Point:               PointReasoning,
		PointOrdinal:        2,
		PolicyEpoch:         11,
		PolicyVersion:       "reasoning-v3",
		PolicyMode:          PolicyShadow,
		SnapshotID:          uuid.MustParse("ca2de29e-5f03-4d29-86f1-8ce0a8a5f62f"),
		SnapshotSHA256:      strings.Repeat("a", 64),
		Environment:         EvaluationProductionCanary,
		ProviderID:          "openrouter",
		ModelID:             "model-v1",
		EligibleActions:     []string{"candidate", StaticActionID},
		EligibilitySHA256:   strings.Repeat("b", 64),
		CatalogSHA256:       strings.Repeat("c", 64),
		ChampionActionID:    StaticActionID,
		RecommendedActionID: "candidate",
		IntendedActionID:    StaticActionID,
		ActionProbabilities: []ActionProbability{
			{ActionID: "candidate", Probability: 0},
			{ActionID: StaticActionID, Probability: 1},
		},
		SelectionReason: "shadow_static",
		Features:        map[FeatureKey]float64{FeatureCandidateCount: 2},
	}
}

func makeCanary(assignment *Assignment) {
	probability := 0.5
	cohortID := uuid.MustParse("0d21ddfc-69f7-42c5-8125-0780f18c6efa")
	assignment.PolicyMode = PolicyCanary
	assignment.CohortID = &cohortID
	assignment.ExperimentID = "experiment-1"
	assignment.ArmID = "candidate"
	assignment.ArmProbability = &probability
	assignment.IntendedActionID = "candidate"
	assignment.ActionProbabilities[0].Probability = probability
	assignment.ActionProbabilities[1].Probability = probability
	assignment.SelectionReason = "canary_assignment"
}

func validDelivery(assignmentID uuid.UUID) Delivery {
	return Delivery{
		SchemaVersion:    SchemaVersion2,
		AssignmentID:     assignmentID,
		IntendedActionID: StaticActionID,
		ActualActionID:   "candidate",
		Status:           DeliveryFallback,
		ExposureKnown:    false,
		FallbackReason:   "candidate_unavailable",
		ResultCount:      2,
		ResultIDs: []ResultID{
			mustResultID(NewArtifactResultID(artifactResultUUID)),
		},
		Revisions:       mustRevisionSet(),
		EffectiveLimits: map[string]int{"top_k": 8},
	}
}
