package main

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

type adaptiveBenchmarkRecorderProbeFact struct {
	Kind         adaptive.EventKind
	OwnerID      uuid.UUID
	RequestID    uuid.UUID
	AssignmentID uuid.UUID
	Domain       adaptive.Domain
	Sequence     int64
}

type adaptiveBenchmarkRecorderProbe struct {
	BeforeWrite func(
		context.Context,
		adaptiveBenchmarkRecorderProbeFact,
	) error
	AfterCommit func(
		context.Context,
		adaptiveBenchmarkRecorderProbeFact,
	) error
}

type adaptiveBenchmarkRecorderProbeRegistration struct {
	probe adaptiveBenchmarkRecorderProbe
}

type adaptiveBenchmarkRecorderProbeHandle struct {
	recorder     *adaptiveBenchmarkTeeRecorder
	registration *adaptiveBenchmarkRecorderProbeRegistration
}

func (recorder *adaptiveBenchmarkTeeRecorder) InstallProbe(
	probe adaptiveBenchmarkRecorderProbe,
) (*adaptiveBenchmarkRecorderProbeHandle, error) {
	if recorder == nil ||
		probe.BeforeWrite == nil ||
		probe.AfterCommit == nil {
		return nil, errors.New(
			"adaptive benchmark recorder probe is invalid",
		)
	}
	registration := &adaptiveBenchmarkRecorderProbeRegistration{
		probe: probe,
	}
	recorder.probeMu.Lock()
	defer recorder.probeMu.Unlock()
	if recorder.probe != nil {
		return nil, errors.New(
			"adaptive benchmark recorder probe is already installed",
		)
	}
	recorder.probe = registration
	return &adaptiveBenchmarkRecorderProbeHandle{
		recorder: recorder, registration: registration,
	}, nil
}

func (handle *adaptiveBenchmarkRecorderProbeHandle) Clear() error {
	if handle == nil ||
		handle.recorder == nil ||
		handle.registration == nil {
		return errors.New(
			"adaptive benchmark recorder probe handle is invalid",
		)
	}
	handle.recorder.probeMu.Lock()
	defer handle.recorder.probeMu.Unlock()
	if handle.recorder.probe != handle.registration {
		return errors.New(
			"adaptive benchmark recorder probe handle is stale",
		)
	}
	handle.recorder.probe = nil
	return nil
}

func (recorder *adaptiveBenchmarkTeeRecorder) currentProbe() *adaptiveBenchmarkRecorderProbeRegistration {
	recorder.probeMu.Lock()
	defer recorder.probeMu.Unlock()
	return recorder.probe
}

func adaptiveBenchmarkAssignmentProbeFact(
	assignment adaptive.Assignment,
) adaptiveBenchmarkRecorderProbeFact {
	return adaptiveBenchmarkRecorderProbeFact{
		Kind:    adaptive.EventDecision,
		OwnerID: assignment.OwnerID, RequestID: assignment.RequestID,
		AssignmentID: assignment.AssignmentID,
		Domain:       assignment.Domain,
	}
}
