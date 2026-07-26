package main

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

type adaptiveBenchmarkConcurrencyStateView struct {
	requestID      uuid.UUID
	assignmentID   uuid.UUID
	startedAt      time.Time
	assignmentAt   time.Time
	releasedAt     time.Time
	deliveryAt     time.Time
	modelStartedAt time.Time
	terminalAt     time.Time
	deliveryCount  int
	turnErr        error
}

func (state *adaptiveBenchmarkConcurrencyClientState) snapshot() adaptiveBenchmarkConcurrencyStateView {
	state.mu.Lock()
	defer state.mu.Unlock()
	return adaptiveBenchmarkConcurrencyStateView{
		requestID: state.requestID, assignmentID: state.assignmentID,
		startedAt: state.startedAt, assignmentAt: state.assignmentAt,
		releasedAt: state.releasedAt, deliveryAt: state.deliveryAt,
		modelStartedAt: state.modelStartedAt, terminalAt: state.terminalAt,
		deliveryCount: state.deliveryCount, turnErr: state.turnErr,
	}
}

func adaptiveBenchmarkConcurrencyEvidence(
	ctx context.Context,
	plan auraeval.AdaptiveBenchmarkConcurrencyPlan,
	states map[string]*adaptiveBenchmarkConcurrencyClientState,
	waitDeadline time.Time,
) (
	auraeval.AdaptiveBenchmarkConcurrencyEvidence,
	map[string]adaptiveBenchmarkPersistedFacts,
	error,
) {
	evidence := auraeval.AdaptiveBenchmarkConcurrencyEvidence{
		Clients: make(
			[]auraeval.AdaptiveBenchmarkConcurrencyClientEvidence,
			0,
			len(plan.Clients),
		),
		ReleaseOrder:   slices.Clone(plan.ReleaseOrder),
		WaitDeadlineAt: waitDeadline.Format(time.RFC3339Nano),
	}
	factsByClient := make(
		map[string]adaptiveBenchmarkPersistedFacts,
		len(plan.Clients),
	)
	for _, spec := range plan.Clients {
		state := states[spec.ClientID]
		snapshot := state.snapshot()
		if snapshot.turnErr != nil ||
			snapshot.requestID == uuid.Nil ||
			snapshot.assignmentID == uuid.Nil ||
			snapshot.deliveryCount != 1 {
			return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil,
				errors.Join(
					snapshot.turnErr,
					adaptiveBenchmarkControlError(
						"adaptive benchmark concurrency turn is incomplete",
					),
				)
		}
		facts, err := state.subject.recorder.PersistedFactsForAssignment(
			ctx,
			state.subject.ownerID,
			snapshot.requestID,
			state.domain,
			snapshot.assignmentID,
		)
		if err != nil {
			return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
		}
		if facts.Assignment.AssignmentID != snapshot.assignmentID ||
			facts.DeliveryEvent.ID == uuid.Nil ||
			facts.Assignment.ChampionActionID != adaptive.StaticActionID ||
			facts.Delivery.ActualActionID != adaptive.StaticActionID {
			return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil,
				adaptiveBenchmarkControlError(
					"adaptive benchmark concurrency facts drifted",
				)
		}
		probability := adaptiveBenchmarkStaticProbability(
			facts.Assignment,
		)
		factsByClient[spec.ClientID] = facts
		evidence.Clients = append(
			evidence.Clients,
			auraeval.AdaptiveBenchmarkConcurrencyClientEvidence{
				ClientID: spec.ClientID, OwnerAlias: spec.OwnerAlias,
				ConversationAlias: spec.ConversationAlias,
				ScenarioID:        spec.ScenarioID,
				OwnerID:           state.subject.ownerID.String(),
				ConversationID:    state.conversationID.String(),
				RequestID:         snapshot.requestID.String(),
				AssignmentID:      snapshot.assignmentID.String(),
				DeliveryEventID:   facts.DeliveryEvent.ID.String(),
				StartedAt:         snapshot.startedAt.Format(time.RFC3339Nano),
				AssignmentAt: snapshot.assignmentAt.Format(
					time.RFC3339Nano,
				),
				ReleasedAt: snapshot.releasedAt.Format(time.RFC3339Nano),
				ModelStartedAt: snapshot.modelStartedAt.Format(
					time.RFC3339Nano,
				),
				DeliveryAt:        snapshot.deliveryAt.Format(time.RFC3339Nano),
				TerminalAt:        snapshot.terminalAt.Format(time.RFC3339Nano),
				DeliveryCount:     snapshot.deliveryCount,
				ChampionActionID:  facts.Assignment.ChampionActionID,
				ActionProbability: probability,
			},
		)
	}
	return evidence, factsByClient, nil
}

func adaptiveBenchmarkStaticProbability(
	assignment adaptive.Assignment,
) float64 {
	for _, candidate := range assignment.ActionProbabilities {
		if candidate.ActionID == adaptive.StaticActionID {
			return candidate.Probability
		}
	}
	return -1
}

func (runtime *adaptiveBenchmarkControlRuntime) runCrossOwnerIsolation(
	_ context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"cross_owner_isolation",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	snapshot := runtime.concurrencySnapshot
	if snapshot == nil || snapshot.facts == nil ||
		len(snapshot.evidence.Clients) != 4 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark cross-owner evidence is unavailable",
			)
	}
	facts, ok := snapshot.facts["C3"]
	if !ok ||
		facts.Assignment.OwnerID != runtime.ownerB ||
		facts.Assignment.OwnerID == runtime.request.OwnerID ||
		facts.Assignment.Domain != adaptive.DomainKnowledge ||
		facts.Assignment.ChampionActionID != adaptive.StaticActionID ||
		facts.Assignment.IntendedActionID != adaptive.StaticActionID ||
		facts.Delivery.ActualActionID != adaptive.StaticActionID ||
		adaptiveBenchmarkStaticProbability(facts.Assignment) != 1 ||
		facts.AssignmentEvent.OwnerID != runtime.ownerB ||
		facts.DeliveryEvent.OwnerID != runtime.ownerB {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark cross-owner facts escaped owner B",
			)
	}
	if !adaptiveBenchmarkBundleOwnsFacts(
		runtime.environment.bundle,
		runtime.ownerB,
		facts,
	) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark cross-owner bundle linkage drifted",
			)
	}
	for _, result := range facts.Delivery.ResultIDs {
		id, err := uuid.Parse(result.ID())
		if err != nil {
			continue
		}
		if _, found := runtime.environment.fixtureAliases.ResolveFixtureAlias(
			adaptive.DomainKnowledge,
			id,
		); found {
			return adaptiveBenchmarkControlExecution{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark owner B exposed owner A fixtures",
				)
		}
	}
	client := snapshot.evidence.Clients[3]
	primaryIDs := make(map[string]struct{}, 9)
	for _, primary := range snapshot.evidence.Clients[:3] {
		for _, value := range []string{
			primary.RequestID,
			primary.AssignmentID,
			primary.DeliveryEventID,
		} {
			primaryIDs[value] = struct{}{}
		}
	}
	ids := make([]uuid.UUID, 0, 3)
	for _, value := range []string{
		client.RequestID,
		client.AssignmentID,
		client.DeliveryEventID,
	} {
		if _, leaked := primaryIDs[value]; leaked {
			return adaptiveBenchmarkControlExecution{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark owner B reused owner A evidence",
				)
		}
		id, err := parseCanonicalAdaptiveBenchmarkUUID(value)
		if err != nil {
			return adaptiveBenchmarkControlExecution{}, err
		}
		ids = append(ids, id)
	}
	return adaptiveBenchmarkControlExecution{EvidenceIDs: ids}, nil
}

func adaptiveBenchmarkBundleOwnsFacts(
	bundle *adaptiveBenchmarkDecisionBundle,
	ownerID uuid.UUID,
	facts adaptiveBenchmarkPersistedFacts,
) bool {
	if bundle == nil ||
		bundle.PrimaryOwnerID() == ownerID ||
		bundle.ControlOwnerID() != ownerID {
		return false
	}
	for _, entry := range bundle.Entries() {
		if entry.OwnerID == ownerID &&
			entry.Domain == facts.Assignment.Domain &&
			entry.SnapshotID == facts.Assignment.SnapshotID &&
			entry.SnapshotSHA256 == facts.Assignment.SnapshotSHA256 {
			return true
		}
	}
	return false
}
