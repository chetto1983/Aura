package eval

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestRunAdaptiveBenchmarkControlsExecutesRegisteredMatrix(t *testing.T) {
	t.Parallel()

	driver := &adaptiveBenchmarkControlDriverFake{}
	run, err := RunAdaptiveBenchmarkControls(t.Context(), driver)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkControls: %v", err)
	}
	if !slices.Equal(driver.controlIDs, AdaptiveBenchmarkControlMatrix()) {
		t.Fatalf("control order = %v", driver.controlIDs)
	}
	if len(run.Records) != 16 {
		t.Fatalf("control records = %d, want 16", len(run.Records))
	}
	if driver.concurrencyPlan == nil ||
		!slices.Equal(
			driver.concurrencyPlan.ReleaseOrder,
			[]string{"C3", "C2", "C0", "C1"},
		) {
		t.Fatalf("concurrency plan = %#v", driver.concurrencyPlan)
	}
}

func TestRunAdaptiveBenchmarkControlsRejectsUnprovedFaultAndStillRunsAll(
	t *testing.T,
) {
	t.Parallel()

	driver := &adaptiveBenchmarkControlDriverFake{
		mutate: func(
			controlID string,
			result *AdaptiveBenchmarkControlResult,
		) {
			if controlID == "assignment_write_failure" {
				result.ObservedFailureReasonCodes = []string{}
			}
		},
	}
	if _, err := RunAdaptiveBenchmarkControls(
		t.Context(),
		driver,
	); err == nil {
		t.Fatal("control runner accepted an unproved injected fault")
	}
	if len(driver.controlIDs) != 16 {
		t.Fatalf("control calls = %d, want 16", len(driver.controlIDs))
	}
}

func TestRunAdaptiveBenchmarkControlsContinuesAfterDriverError(t *testing.T) {
	t.Parallel()

	driver := &adaptiveBenchmarkControlDriverFake{failAt: 3}
	if _, err := RunAdaptiveBenchmarkControls(
		t.Context(),
		driver,
	); !errors.Is(err, errAdaptiveBenchmarkControlFixture) {
		t.Fatalf("control runner error = %v", err)
	}
	if len(driver.controlIDs) != 16 {
		t.Fatalf("control calls = %d, want 16", len(driver.controlIDs))
	}
}

func TestValidateAdaptiveBenchmarkConcurrencyEvidenceRejectsDrift(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*AdaptiveBenchmarkConcurrencyEvidence)
	}{
		{
			name: "C1 entered before C0 terminal",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[1].AssignmentAt = "2026-07-26T10:00:12Z"
			},
		},
		{
			name: "C1 started after C0 release",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[1].StartedAt = "2026-07-26T10:00:11Z"
			},
		},
		{
			name: "release order",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.ReleaseOrder[0], evidence.ReleaseOrder[1] =
					evidence.ReleaseOrder[1], evidence.ReleaseOrder[0]
			},
		},
		{
			name: "owner isolation",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[3].OwnerID = evidence.Clients[0].OwnerID
			},
		},
		{
			name: "assignment uniqueness",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[3].AssignmentID =
					evidence.Clients[0].AssignmentID
			},
		},
		{
			name: "delivery count",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[2].DeliveryCount = 2
			},
		},
		{
			name: "model before release",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[0].ModelStartedAt =
					"2026-07-26T10:00:09Z"
			},
		},
		{
			name: "delivery after model start",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.Clients[0].ModelStartedAt =
					"2026-07-26T10:00:11Z"
				evidence.Clients[0].DeliveryAt =
					"2026-07-26T10:00:12Z"
			},
		},
		{
			name: "wait deadline exceeded",
			mutate: func(evidence *AdaptiveBenchmarkConcurrencyEvidence) {
				evidence.WaitDeadlineAt = "2026-07-26T10:00:17Z"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			evidence := adaptiveBenchmarkConcurrencyEvidenceFixture()
			test.mutate(&evidence)
			if err := ValidateAdaptiveBenchmarkConcurrencyEvidence(
				evidence,
			); err == nil {
				t.Fatal("concurrency validator accepted schedule drift")
			}
		})
	}
}

func TestAdaptiveBenchmarkConcurrencyEvidenceCarriesBoundedWaitDeadline(
	t *testing.T,
) {
	t.Parallel()

	if _, ok := reflect.TypeFor[AdaptiveBenchmarkConcurrencyEvidence]().FieldByName("WaitDeadlineAt"); !ok {
		t.Fatal("concurrency evidence omits a bounded wait deadline")
	}
}

var errAdaptiveBenchmarkControlFixture = errors.New("control fixture failure")

type adaptiveBenchmarkControlDriverFake struct {
	controlIDs      []string
	concurrencyPlan *AdaptiveBenchmarkConcurrencyPlan
	failAt          int
	mutate          func(string, *AdaptiveBenchmarkControlResult)
}

func (driver *adaptiveBenchmarkControlDriverFake) RunControl(
	_ context.Context,
	control AdaptiveBenchmarkControlCase,
) (AdaptiveBenchmarkControlResult, error) {
	driver.controlIDs = append(driver.controlIDs, control.ControlID)
	if control.ConcurrencyPlan != nil {
		plan := *control.ConcurrencyPlan
		driver.concurrencyPlan = &plan
	}
	result := adaptiveBenchmarkControlResultFixture(control.ControlID)
	if driver.mutate != nil {
		driver.mutate(control.ControlID, &result)
	}
	if driver.failAt == len(driver.controlIDs) {
		return result, errAdaptiveBenchmarkControlFixture
	}
	return result, nil
}

func adaptiveBenchmarkControlResultFixture(
	controlID string,
) AdaptiveBenchmarkControlResult {
	result := AdaptiveBenchmarkControlResult{
		Status:      AdaptiveBenchmarkStatusPass,
		StartedAt:   "2026-07-26T10:00:00Z",
		CompletedAt: "2026-07-26T10:01:00Z",
		EvidenceIDs: []string{benchmarkUUID("control-" + controlID)},
		ReasonCodes: []string{},
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedFaultReasons(
			controlID,
		),
	}
	if controlID == "same_owner_concurrency" {
		evidence := adaptiveBenchmarkConcurrencyEvidenceFixture()
		result.Concurrency = &evidence
		result.EvidenceIDs = adaptiveBenchmarkConcurrencyEvidenceIDs(evidence)
	}
	return result
}

func adaptiveBenchmarkConcurrencyEvidenceFixture() AdaptiveBenchmarkConcurrencyEvidence {
	clients := []AdaptiveBenchmarkConcurrencyClientEvidence{
		adaptiveBenchmarkConcurrencyClientFixture(
			"C0", "A", "A0", "r01", 0, 1, 10, 12, 11, 13,
		),
		adaptiveBenchmarkConcurrencyClientFixture(
			"C1", "A", "A0", "r02", 2, 14, 15, 17, 16, 18,
		),
		adaptiveBenchmarkConcurrencyClientFixture(
			"C2", "A", "A1", "m01", 3, 4, 8, 10, 9, 11,
		),
		adaptiveBenchmarkConcurrencyClientFixture(
			"C3", "B", "B0", "k01", 3, 5, 7, 9, 8, 10,
		),
	}
	clients[1].OwnerID = clients[0].OwnerID
	clients[1].ConversationID = clients[0].ConversationID
	clients[2].OwnerID = clients[0].OwnerID
	return AdaptiveBenchmarkConcurrencyEvidence{
		Clients:        clients,
		ReleaseOrder:   []string{"C3", "C2", "C0", "C1"},
		WaitDeadlineAt: "2026-07-26T10:00:20Z",
	}
}

func adaptiveBenchmarkConcurrencyClientFixture(
	clientID string,
	ownerAlias string,
	conversationAlias string,
	scenarioID string,
	started int,
	assigned int,
	released int,
	modelStarted int,
	delivered int,
	terminal int,
) AdaptiveBenchmarkConcurrencyClientEvidence {
	return AdaptiveBenchmarkConcurrencyClientEvidence{
		ClientID: clientID, OwnerAlias: ownerAlias,
		ConversationAlias: conversationAlias, ScenarioID: scenarioID,
		OwnerID:        benchmarkUUID("owner-" + ownerAlias),
		ConversationID: benchmarkUUID("conversation-" + conversationAlias),
		RequestID:      benchmarkUUID("request-control-" + clientID),
		AssignmentID:   benchmarkUUID("assignment-control-" + clientID),
		DeliveryEventID: benchmarkUUID(
			"delivery-control-" + clientID,
		),
		StartedAt:         benchmarkControlTime(started),
		AssignmentAt:      benchmarkControlTime(assigned),
		ReleasedAt:        benchmarkControlTime(released),
		ModelStartedAt:    benchmarkControlTime(modelStarted),
		DeliveryAt:        benchmarkControlTime(delivered),
		TerminalAt:        benchmarkControlTime(terminal),
		DeliveryCount:     1,
		ChampionActionID:  "static",
		ActionProbability: 1,
	}
}

func adaptiveBenchmarkConcurrencyEvidenceIDs(
	evidence AdaptiveBenchmarkConcurrencyEvidence,
) []string {
	ids := make([]string, 0, 12)
	for _, client := range evidence.Clients {
		ids = append(
			ids,
			client.RequestID,
			client.AssignmentID,
			client.DeliveryEventID,
		)
	}
	slices.Sort(ids)
	return ids
}

func benchmarkControlTime(second int) string {
	return "2026-07-26T10:00:" + []string{
		"00Z", "01Z", "02Z", "03Z", "04Z", "05Z", "06Z",
		"07Z", "08Z", "09Z", "10Z", "11Z", "12Z", "13Z",
		"14Z", "15Z", "16Z", "17Z", "18Z",
	}[second]
}

var _ AdaptiveBenchmarkControlDriver = (*adaptiveBenchmarkControlDriverFake)(nil)
