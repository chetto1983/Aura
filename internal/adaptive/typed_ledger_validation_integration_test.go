//go:build db_integration

package adaptive

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type typedAssignmentRejection struct {
	name      string
	mutate    func(map[string]any)
	aggregate string
	decision  uuid.UUID
	kind      EventKind
}

func TestSchema2LedgerRejectsInvalidAssignmentBindingsShapeAndProbabilities(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	otherOwner := seedTypedLedgerOwner(t, pool)
	cases := []typedAssignmentRejection{
		{name: "owner binding", mutate: func(p map[string]any) { p["owner_id"] = otherOwner.String() }},
		{name: "request aggregate binding", aggregate: uuid.Must(uuid.NewV7()).String()},
		{name: "assignment decision binding", decision: uuid.Must(uuid.NewV7())},
		{name: "event kind shape", kind: EventDelivery},
		{name: "required shape", mutate: func(p map[string]any) { delete(p, "eligible_actions") }},
		{name: "probability type", mutate: func(p map[string]any) {
			p["action_probabilities"].([]any)[0].(map[string]any)["probability"] = "NaN"
		}},
		{name: "probability total", mutate: func(p map[string]any) {
			probabilities := p["action_probabilities"].([]any)
			probabilities[0].(map[string]any)["probability"] = 0.4
			probabilities[1].(map[string]any)["probability"] = 0.4
		}},
		{name: "unknown top-level field", mutate: func(p map[string]any) { p["query"] = "private" }},
		{name: "required string type", mutate: func(p map[string]any) { p["eligibility_sha256"] = nil }},
		{name: "unknown probability field", mutate: func(p map[string]any) {
			p["action_probabilities"].([]any)[0].(map[string]any)["content"] = "private"
		}},
		{name: "unregistered feature", mutate: func(p map[string]any) {
			p["features"].(map[string]any)["query_hash"] = 1
		}},
		{name: "bounded integer feature", mutate: func(p map[string]any) {
			p["features"].(map[string]any)["candidate_count"] = 1.5
		}},
		{name: "domain enum", mutate: func(p map[string]any) { p["domain"] = "unknown" }},
		{name: "domain point binding", mutate: func(p map[string]any) { p["point"] = "memory_recall" }},
		{name: "policy enum", mutate: func(p map[string]any) { p["policy_mode"] = "unknown" }},
		{name: "selection reason enum", mutate: func(p map[string]any) {
			p["selection_reason"] = "unknown"
		}},
		{name: "ascii identifier", mutate: func(p map[string]any) { p["provider_id"] = "private value" }},
		{name: "ordered actions", mutate: func(p map[string]any) {
			setTypedActions(p, []string{"static", "candidate"}, []float64{1, 0})
		}},
		{name: "duplicate actions", mutate: func(p map[string]any) {
			setTypedActions(p, []string{"candidate", "candidate", "static"}, []float64{0, 0, 1})
		}},
		{name: "static action required", mutate: func(p map[string]any) {
			setTypedActions(p, []string{"candidate"}, []float64{1})
			p["champion_action_id"], p["intended_action_id"] = "candidate", "candidate"
		}},
		{name: "none action forbidden", mutate: func(p map[string]any) {
			setTypedActions(p, []string{"candidate", "none", "static"}, []float64{0, 0, 1})
		}},
		{name: "shadow action semantics", mutate: func(p map[string]any) {
			p["intended_action_id"] = "candidate"
			setTypedActions(p, []string{"candidate", "static"}, []float64{1, 0})
		}},
		{name: "non-randomized arm", mutate: func(p map[string]any) {
			p["experiment_id"] = "experiment-v1"
		}},
		{name: "override semantics", mutate: func(p map[string]any) { p["override"] = true }},
		{name: "canary support", mutate: func(p map[string]any) {
			p["policy_mode"], p["selection_reason"] = "canary", "canary_assignment"
			p["cohort_id"], p["experiment_id"] = uuid.Must(uuid.NewV7()).String(), "experiment-v1"
			p["arm_id"], p["arm_probability"] = "baseline", 0.5
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertTypedAssignmentRejected(t, ctx, pool, owner, testCase)
		})
	}
}

func assertTypedAssignmentRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
	testCase typedAssignmentRejection,
) {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeTypedLedgerPayload(t, event.Payload)
	if testCase.mutate != nil {
		testCase.mutate(payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	aggregate := testCase.aggregate
	if aggregate == "" {
		aggregate = assignment.RequestID.String()
	}
	decision := testCase.decision
	if decision == uuid.Nil {
		decision = assignment.AssignmentID
	}
	kind := testCase.kind
	if kind == "" {
		kind = EventDecision
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, aggregate,
		1, decision, kind, encoded,
	); err == nil {
		t.Fatalf("database accepted invalid schema-2 assignment payload %s", encoded)
	}
}

func setTypedActions(payload map[string]any, actions []string, probabilities []float64) {
	eligible := make([]any, len(actions))
	vector := make([]any, len(actions))
	for index, action := range actions {
		eligible[index] = action
		vector[index] = map[string]any{"action_id": action, "probability": probabilities[index]}
	}
	payload["eligible_actions"], payload["action_probabilities"] = eligible, vector
}

type typedDeliveryRejection struct {
	name      string
	mutate    func(map[string]any)
	owner     uuid.UUID
	aggregate string
	decision  uuid.UUID
	kind      EventKind
}

func TestSchema2LedgerRejectsInvalidDeliveryBindingsShapeAndProbabilities(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	otherOwner := seedTypedLedgerOwner(t, pool)
	cases := []typedDeliveryRejection{
		{name: "owner assignment binding", owner: otherOwner},
		{name: "request assignment binding", aggregate: uuid.Must(uuid.NewV7()).String()},
		{name: "assignment decision binding", decision: uuid.Must(uuid.NewV7())},
		{name: "event kind shape", kind: EventDecision},
		{name: "required shape", mutate: func(p map[string]any) { delete(p, "actual_action_id") }},
		{name: "known probability required", mutate: func(p map[string]any) {
			p["exposure_probability"] = nil
		}},
		{name: "probability range", mutate: func(p map[string]any) { p["exposure_probability"] = 1.1 }},
		{name: "unknown top-level field", mutate: func(p map[string]any) {
			p["result_content"] = "private"
		}},
		{name: "required string type", mutate: func(p map[string]any) { p["status"] = nil }},
		{name: "unknown result field", mutate: func(p map[string]any) {
			setTypedResult(p, "artifact", uuid.Must(uuid.NewV7()).String(), map[string]any{"content": "private"})
		}},
		{name: "result kind type", mutate: func(p map[string]any) {
			setTypedResult(p, nil, uuid.Must(uuid.NewV7()).String(), nil)
		}},
		{name: "result kind and ID", mutate: func(p map[string]any) {
			setTypedResult(p, "artifact", "not-a-uuid", nil)
		}},
		{name: "revision registry", mutate: func(p map[string]any) {
			p["revisions"] = map[string]any{"query_hash": "private"}
		}},
		{name: "effective limit registry", mutate: func(p map[string]any) {
			p["effective_limits"] = map[string]any{"prompt_length": 1}
		}},
		{name: "intended assignment binding", mutate: func(p map[string]any) {
			p["intended_action_id"] = "candidate"
		}},
		{name: "actual action eligibility", mutate: func(p map[string]any) {
			p["actual_action_id"] = "unregistered"
		}},
		{name: "success fallback semantics", mutate: func(p map[string]any) {
			p["fallback_reason"] = "strategy_failed"
		}},
		{name: "fallback action semantics", mutate: func(p map[string]any) {
			p["status"], p["fallback_reason"] = "fallback", "strategy_failed"
		}},
		{name: "assignment exposure probability", mutate: func(p map[string]any) {
			p["exposure_probability"] = 0.5
		}},
		{name: "duplicate results", mutate: func(p map[string]any) {
			resultID := uuid.Must(uuid.NewV7()).String()
			result := map[string]any{"kind": "artifact", "id": resultID}
			p["result_count"], p["result_ids"] = 2, []any{result, result}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertTypedDeliveryRejected(t, ctx, pool, owner, testCase)
		})
	}
}

func assertTypedDeliveryRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
	testCase typedDeliveryRejection,
) {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		t.Fatal(err)
	}
	deliveryEvent, err := NewDeliveryEvent(assignment, typedLedgerDelivery(t, assignment))
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeTypedLedgerPayload(t, deliveryEvent.Payload)
	if testCase.mutate != nil {
		testCase.mutate(payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	rowOwner := testCase.owner
	if rowOwner == uuid.Nil {
		rowOwner = owner
	}
	aggregate := testCase.aggregate
	if aggregate == "" {
		aggregate = assignment.RequestID.String()
	}
	decision := testCase.decision
	if decision == uuid.Nil {
		decision = assignment.AssignmentID
	}
	kind := testCase.kind
	if kind == "" {
		kind = EventDelivery
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), rowOwner, aggregate,
		2, decision, kind, encoded,
	); err == nil {
		t.Fatalf("database accepted invalid schema-2 delivery payload %s", encoded)
	}
}

func setTypedResult(payload map[string]any, kind any, id string, extras map[string]any) {
	result := map[string]any{"kind": kind, "id": id}
	for key, value := range extras {
		result[key] = value
	}
	payload["result_count"], payload["result_ids"] = 1, []any{result}
}
