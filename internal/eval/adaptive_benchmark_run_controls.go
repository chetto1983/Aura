package eval

import (
	"context"
	"slices"
)

var adaptiveBenchmarkControlMatrix = []string{
	"negative_control",
	"static_equivalence",
	"restart_recovery",
	"rollback_static",
	"privacy_redaction",
	"retention_cleanup",
	"same_owner_concurrency",
	"cross_owner_isolation",
	"assignment_write_failure",
	"delivery_write_failure",
	"outcome_write_failure",
	"model_transport_failure",
	"memory_epoch_mismatch",
	"settings_apply_failure",
	"settings_restore_failure",
	"stale_override_recovery",
}

// AdaptiveBenchmarkControlMatrix returns the exact registered control order.
func AdaptiveBenchmarkControlMatrix() []string {
	return slices.Clone(adaptiveBenchmarkControlMatrix)
}

// AdaptiveBenchmarkConcurrencyClient is one preregistered concurrent turn.
type AdaptiveBenchmarkConcurrencyClient struct {
	ClientID          string
	OwnerAlias        string
	ConversationAlias string
	ScenarioID        string
}

// AdaptiveBenchmarkConcurrencyPlan freezes clients and release order.
type AdaptiveBenchmarkConcurrencyPlan struct {
	Clients      []AdaptiveBenchmarkConcurrencyClient
	ReleaseOrder []string
}

// AdaptiveBenchmarkControlCase is one exact executable control invocation.
type AdaptiveBenchmarkControlCase struct {
	ControlID       string
	ConcurrencyPlan *AdaptiveBenchmarkConcurrencyPlan
}

// AdaptiveBenchmarkControlResult contains real, transient control evidence.
type AdaptiveBenchmarkControlResult struct {
	Status                     AdaptiveBenchmarkStatus
	StartedAt                  string
	CompletedAt                string
	EvidenceIDs                []string
	ReasonCodes                []string
	ObservedFailureReasonCodes []string
	Concurrency                *AdaptiveBenchmarkConcurrencyEvidence
}

// AdaptiveBenchmarkControlDriver executes controls through production seams.
type AdaptiveBenchmarkControlDriver interface {
	RunControl(
		context.Context,
		AdaptiveBenchmarkControlCase,
	) (AdaptiveBenchmarkControlResult, error)
}

// AdaptiveBenchmarkControlRun contains the registered report records.
type AdaptiveBenchmarkControlRun struct {
	Records  []AdaptiveBenchmarkControlRecord
	verified bool
}

// AdaptiveBenchmarkConcurrencyClientEvidence proves one controlled turn.
type AdaptiveBenchmarkConcurrencyClientEvidence struct {
	ClientID          string
	OwnerAlias        string
	ConversationAlias string
	ScenarioID        string
	OwnerID           string
	ConversationID    string
	RequestID         string
	AssignmentID      string
	DeliveryEventID   string
	StartedAt         string
	AssignmentAt      string
	ReleasedAt        string
	ModelStartedAt    string
	DeliveryAt        string
	TerminalAt        string
	DeliveryCount     int
	ChampionActionID  string
	ActionProbability float64
}

// AdaptiveBenchmarkConcurrencyEvidence binds the four-client release trace.
type AdaptiveBenchmarkConcurrencyEvidence struct {
	Clients        []AdaptiveBenchmarkConcurrencyClientEvidence
	ReleaseOrder   []string
	WaitDeadlineAt string
}

// AdaptiveBenchmarkConcurrencySchedule returns a fresh C0..C3 schedule.
func AdaptiveBenchmarkConcurrencySchedule() AdaptiveBenchmarkConcurrencyPlan {
	return AdaptiveBenchmarkConcurrencyPlan{
		Clients: []AdaptiveBenchmarkConcurrencyClient{
			{
				ClientID: "C0", OwnerAlias: "A",
				ConversationAlias: "A0", ScenarioID: "r01",
			},
			{
				ClientID: "C1", OwnerAlias: "A",
				ConversationAlias: "A0", ScenarioID: "r02",
			},
			{
				ClientID: "C2", OwnerAlias: "A",
				ConversationAlias: "A1", ScenarioID: "m01",
			},
			{
				ClientID: "C3", OwnerAlias: "B",
				ConversationAlias: "B0", ScenarioID: "k01",
			},
		},
		ReleaseOrder: []string{"C3", "C2", "C0", "C1"},
	}
}
