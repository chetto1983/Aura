package eval

import (
	"errors"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
)

// ValidateAdaptiveBenchmarkConcurrencyEvidence proves the fixed C0..C3 trace.
func ValidateAdaptiveBenchmarkConcurrencyEvidence(
	evidence AdaptiveBenchmarkConcurrencyEvidence,
) error {
	plan := AdaptiveBenchmarkConcurrencySchedule()
	if len(evidence.Clients) != len(plan.Clients) ||
		!slices.Equal(evidence.ReleaseOrder, plan.ReleaseOrder) {
		return errors.New("adaptive benchmark concurrency schedule drifted")
	}
	waitDeadline, err := parseAdaptiveBenchmarkTime(evidence.WaitDeadlineAt)
	if err != nil {
		return errors.New(
			"adaptive benchmark concurrency wait deadline is invalid",
		)
	}
	times := make([]adaptiveBenchmarkConcurrencyTimes, len(plan.Clients))
	requests := make(map[string]struct{}, len(plan.Clients))
	assignments := make(map[string]struct{}, len(plan.Clients))
	deliveries := make(map[string]struct{}, len(plan.Clients))
	for index, want := range plan.Clients {
		client := evidence.Clients[index]
		if client.ClientID != want.ClientID ||
			client.OwnerAlias != want.OwnerAlias ||
			client.ConversationAlias != want.ConversationAlias ||
			client.ScenarioID != want.ScenarioID ||
			!validBenchmarkUUID(client.OwnerID) ||
			!validBenchmarkUUID(client.ConversationID) ||
			!validBenchmarkUUID(client.RequestID) ||
			!validBenchmarkUUID(client.AssignmentID) ||
			!validBenchmarkUUID(client.DeliveryEventID) ||
			client.DeliveryCount != 1 ||
			client.ChampionActionID != adaptive.StaticActionID ||
			client.ActionProbability != 1 {
			return errors.New(
				"adaptive benchmark concurrency client evidence is invalid",
			)
		}
		parsed, err := parseAdaptiveBenchmarkConcurrencyTimes(client)
		if err != nil {
			return err
		}
		times[index] = parsed
		if !strictBenchmarkConcurrencyTimes(parsed) {
			return errors.New(
				"adaptive benchmark assignment/exposure order is invalid",
			)
		}
		if duplicateBenchmarkFact(requests, client.RequestID) ||
			duplicateBenchmarkFact(assignments, client.AssignmentID) ||
			duplicateBenchmarkFact(deliveries, client.DeliveryEventID) {
			return errors.New(
				"adaptive benchmark concurrency fact ID is duplicated",
			)
		}
	}
	if err := validateAdaptiveBenchmarkConcurrencyIdentity(
		evidence.Clients,
	); err != nil {
		return err
	}
	if !times[1].started.Before(times[0].released) ||
		!times[1].assignment.After(times[0].terminal) ||
		times[1].terminal.After(waitDeadline) ||
		!times[2].assignment.Before(times[0].released) ||
		!times[3].assignment.Before(times[0].released) {
		return errors.New(
			"adaptive benchmark conversation blocking proof is invalid",
		)
	}
	releaseTimes := map[string]time.Time{}
	for index, client := range evidence.Clients {
		releaseTimes[client.ClientID] = times[index].released
	}
	for index := 1; index < len(evidence.ReleaseOrder); index++ {
		if !releaseTimes[evidence.ReleaseOrder[index]].After(
			releaseTimes[evidence.ReleaseOrder[index-1]],
		) {
			return errors.New(
				"adaptive benchmark release timing is invalid",
			)
		}
	}
	return nil
}

type adaptiveBenchmarkConcurrencyTimes struct {
	started      time.Time
	assignment   time.Time
	released     time.Time
	modelStarted time.Time
	delivery     time.Time
	terminal     time.Time
}

func parseAdaptiveBenchmarkConcurrencyTimes(
	client AdaptiveBenchmarkConcurrencyClientEvidence,
) (adaptiveBenchmarkConcurrencyTimes, error) {
	values := []string{
		client.StartedAt,
		client.AssignmentAt,
		client.ReleasedAt,
		client.ModelStartedAt,
		client.DeliveryAt,
		client.TerminalAt,
	}
	parsed := make([]time.Time, len(values))
	for index, value := range values {
		timestamp, err := parseAdaptiveBenchmarkTime(value)
		if err != nil {
			return adaptiveBenchmarkConcurrencyTimes{}, err
		}
		parsed[index] = timestamp
	}
	return adaptiveBenchmarkConcurrencyTimes{
		started: parsed[0], assignment: parsed[1], released: parsed[2],
		modelStarted: parsed[3], delivery: parsed[4], terminal: parsed[5],
	}, nil
}

func strictBenchmarkConcurrencyTimes(
	times adaptiveBenchmarkConcurrencyTimes,
) bool {
	return times.assignment.After(times.started) &&
		times.released.After(times.assignment) &&
		times.delivery.After(times.released) &&
		!times.modelStarted.Before(times.delivery) &&
		!times.terminal.Before(times.modelStarted)
}

func validateAdaptiveBenchmarkConcurrencyIdentity(
	clients []AdaptiveBenchmarkConcurrencyClientEvidence,
) error {
	if clients[0].OwnerID != clients[1].OwnerID ||
		clients[0].OwnerID != clients[2].OwnerID ||
		clients[0].OwnerID == clients[3].OwnerID ||
		clients[0].ConversationID != clients[1].ConversationID ||
		clients[0].ConversationID == clients[2].ConversationID ||
		clients[0].ConversationID == clients[3].ConversationID ||
		clients[2].ConversationID == clients[3].ConversationID {
		return errors.New("adaptive benchmark owner isolation proof is invalid")
	}
	return nil
}

func duplicateBenchmarkFact(seen map[string]struct{}, value string) bool {
	if _, duplicate := seen[value]; duplicate {
		return true
	}
	seen[value] = struct{}{}
	return false
}

func benchmarkConcurrencyEvidenceIDs(
	evidence AdaptiveBenchmarkConcurrencyEvidence,
) []string {
	ids := make([]string, 0, len(evidence.Clients)*3)
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
