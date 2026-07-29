package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestRunAdaptiveBenchmarkInChatEnvironmentDefersCanonicalReport(
	t *testing.T,
) {
	t.Parallel()
	ownerID := uuid.Must(uuid.NewV7())
	runID := uuid.Must(uuid.NewV7())
	bundle := adaptiveBenchmarkReportBundle(t, runID, ownerID)
	outputDir := filepath.Join(t.TempDir(), "report")
	request := adaptiveBenchmarkRunRequest{
		OwnerID:   ownerID,
		OutputDir: outputDir,
		Mode:      auraeval.AdaptiveBenchmarkModeProductionShadow,
	}
	model := auraeval.AdaptiveBenchmarkModel{
		ProviderID:   "openrouter",
		ModelID:      "production-model",
		BaseURLClass: auraeval.AdaptiveBenchmarkBaseURLRemote,
	}
	times := []time.Time{
		time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 26, 10, 10, 0, 0, time.UTC),
	}
	timeIndex := 0
	commands := &adaptiveBenchmarkGitCommandFake{
		outputs: [][]byte{
			[]byte("0123456789abcdef0123456789abcdef01234567\n"),
			nil,
			nil,
			nil,
		},
	}
	finalizeCalls := 0
	draft, err := runAdaptiveBenchmarkInChatEnvironmentWithOps(
		t.Context(),
		&adaptiveBenchmarkChatEnvironment{
			env:    &chatEnv{},
			bundle: bundle,
		},
		request,
		runID,
		model,
		adaptiveBenchmarkReportOps{
			now: func() time.Time {
				value := times[timeIndex]
				timeIndex++
				return value
			},
			workingDirectory: func() (string, error) {
				return t.TempDir(), nil
			},
			migrationHead: func() (int64, error) { return 77, nil },
			commands:      commands,
			scenarioDriver: func(
				*adaptiveBenchmarkChatEnvironment,
				adaptiveBenchmarkRunRequest,
				uuid.UUID,
			) (auraeval.AdaptiveBenchmarkDriver, error) {
				return adaptiveBenchmarkReportScenarioDriver{}, nil
			},
			controlDriver: func(
				*adaptiveBenchmarkChatEnvironment,
				adaptiveBenchmarkRunRequest,
				uuid.UUID,
			) (auraeval.AdaptiveBenchmarkControlDriver, error) {
				return adaptiveBenchmarkReportControlDriver{}, nil
			},
			finalize: func(
				report auraeval.AdaptiveBenchmarkReport,
				scenarios auraeval.AdaptiveBenchmarkScenarioRun,
				controls auraeval.AdaptiveBenchmarkControlRun,
			) (auraeval.AdaptiveBenchmarkReport, error) {
				finalizeCalls++
				return auraeval.FinalizeAdaptiveBenchmarkReport(
					report,
					scenarios,
					controls,
				)
			},
		},
	)
	if err != nil {
		t.Fatalf("runAdaptiveBenchmarkInChatEnvironmentWithOps: %v", err)
	}
	if draft == nil || finalizeCalls != 0 {
		t.Fatalf("draft=%#v finalize_calls=%d", draft, finalizeCalls)
	}
	payload, err := finalizeAdaptiveBenchmarkReportDraft(
		t.Context(),
		draft,
	)
	if err != nil {
		t.Fatalf("finalizeAdaptiveBenchmarkReportDraft: %v", err)
	}
	if finalizeCalls != 1 {
		t.Fatalf("finalize calls=%d, want 1", finalizeCalls)
	}
	report, err := auraeval.DecodeAdaptiveBenchmarkReport(payload)
	if err != nil {
		t.Fatalf("DecodeAdaptiveBenchmarkReport: %v", err)
	}
	if report.RunID != runID.String() ||
		report.Mode != request.Mode ||
		report.PortabilityOnly ||
		report.Environment.MigrationHead != "0077" ||
		report.FrozenInputs.SnapshotID == nil ||
		*report.FrozenInputs.SnapshotID != bundle.ID().String() ||
		report.FrozenInputs.SnapshotSHA256 == nil ||
		*report.FrozenInputs.SnapshotSHA256 != bundle.SHA256() ||
		len(report.Scenarios) != 52 ||
		len(report.Controls) != 16 {
		t.Fatalf("report provenance drifted: %#v", report)
	}
	if _, err := os.Stat(
		filepath.Join(outputDir, adaptiveBenchmarkReportFilename),
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("report published before execution close: %v", err)
	}
}

func TestAdaptiveBenchmarkFrozenInputsBindOptionalPriceTable(t *testing.T) {
	t.Parallel()
	bundle := adaptiveBenchmarkReportBundle(
		t,
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
	)
	withoutPrice := adaptiveBenchmarkFrozenInputs(
		bundle,
		auraeval.AdaptiveBenchmarkScenarioRun{},
	)
	if withoutPrice.PriceTableID != nil ||
		withoutPrice.PriceTableRevision != nil ||
		withoutPrice.PriceTableSHA256 != nil {
		t.Fatalf("unexpected price provenance: %#v", withoutPrice)
	}
	withPrice := adaptiveBenchmarkFrozenInputs(
		bundle,
		auraeval.AdaptiveBenchmarkScenarioRun{
			PriceTable: &auraeval.AdaptiveBenchmarkPriceTable{
				ID:       "prices",
				Revision: 4,
				SHA256:   strings.Repeat("4", 64),
			},
		},
	)
	if withPrice.PriceTableID == nil ||
		*withPrice.PriceTableID != "prices" ||
		withPrice.PriceTableRevision == nil ||
		*withPrice.PriceTableRevision != 4 ||
		withPrice.PriceTableSHA256 == nil ||
		*withPrice.PriceTableSHA256 != strings.Repeat("4", 64) {
		t.Fatalf("price provenance=%#v", withPrice)
	}
	if withPrice.PromptTemplateSHA256 !=
		adaptiveBenchmarkPromptTemplateSHA256() {
		t.Fatal("prompt template hash drifted")
	}
}

func TestRunAdaptiveBenchmarkInChatEnvironmentPropagatesDependencyFailures(
	t *testing.T,
) {
	t.Parallel()
	dependencyErr := errors.New("dependency failed")
	tests := []struct {
		name       string
		want       string
		atFinalize bool
		change     func(*adaptiveBenchmarkReportOps)
	}{
		{
			name: "scenario driver error",
			want: "create adaptive benchmark scenario driver",
			change: func(ops *adaptiveBenchmarkReportOps) {
				ops.scenarioDriver = func(
					*adaptiveBenchmarkChatEnvironment,
					adaptiveBenchmarkRunRequest,
					uuid.UUID,
				) (auraeval.AdaptiveBenchmarkDriver, error) {
					return nil, dependencyErr
				}
			},
		},
		{
			name: "nil scenario driver",
			want: "create adaptive benchmark scenario driver",
			change: func(ops *adaptiveBenchmarkReportOps) {
				ops.scenarioDriver = func(
					*adaptiveBenchmarkChatEnvironment,
					adaptiveBenchmarkRunRequest,
					uuid.UUID,
				) (auraeval.AdaptiveBenchmarkDriver, error) {
					return nil, nil
				}
			},
		},
		{
			name: "control driver error",
			want: "create adaptive benchmark control driver",
			change: func(ops *adaptiveBenchmarkReportOps) {
				ops.controlDriver = func(
					*adaptiveBenchmarkChatEnvironment,
					adaptiveBenchmarkRunRequest,
					uuid.UUID,
				) (auraeval.AdaptiveBenchmarkControlDriver, error) {
					return nil, dependencyErr
				}
			},
		},
		{
			name:       "migration error",
			want:       "read adaptive benchmark migration head",
			atFinalize: true,
			change: func(ops *adaptiveBenchmarkReportOps) {
				ops.migrationHead = func() (int64, error) {
					return 0, dependencyErr
				}
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ownerID := uuid.Must(uuid.NewV7())
			runID := uuid.Must(uuid.NewV7())
			started := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
			nowCalls := 0
			ops := adaptiveBenchmarkReportOps{
				now: func() time.Time {
					nowCalls++
					if nowCalls == 1 {
						return started
					}
					return started.Add(10 * time.Minute)
				},
				workingDirectory: func() (string, error) {
					return t.TempDir(), nil
				},
				migrationHead: func() (int64, error) { return 77, nil },
				commands: &adaptiveBenchmarkGitCommandFake{
					outputs: [][]byte{
						[]byte("0123456789abcdef0123456789abcdef01234567\n"),
						nil,
						nil,
						nil,
					},
				},
				scenarioDriver: func(
					*adaptiveBenchmarkChatEnvironment,
					adaptiveBenchmarkRunRequest,
					uuid.UUID,
				) (auraeval.AdaptiveBenchmarkDriver, error) {
					return adaptiveBenchmarkReportScenarioDriver{}, nil
				},
				controlDriver: func(
					*adaptiveBenchmarkChatEnvironment,
					adaptiveBenchmarkRunRequest,
					uuid.UUID,
				) (auraeval.AdaptiveBenchmarkControlDriver, error) {
					return adaptiveBenchmarkReportControlDriver{}, nil
				},
				finalize: auraeval.FinalizeAdaptiveBenchmarkReport,
			}
			testCase.change(&ops)
			draft, err := runAdaptiveBenchmarkInChatEnvironmentWithOps(
				t.Context(),
				&adaptiveBenchmarkChatEnvironment{
					env: &chatEnv{},
					bundle: adaptiveBenchmarkReportBundle(
						t,
						runID,
						ownerID,
					),
				},
				adaptiveBenchmarkRunRequest{
					OwnerID:   ownerID,
					OutputDir: filepath.Join(t.TempDir(), "report"),
					Mode:      auraeval.AdaptiveBenchmarkModeProductionShadow,
				},
				runID,
				auraeval.AdaptiveBenchmarkModel{
					ProviderID:   "openrouter",
					ModelID:      "production-model",
					BaseURLClass: auraeval.AdaptiveBenchmarkBaseURLRemote,
				},
				ops,
			)
			if err == nil && testCase.atFinalize {
				_, err = finalizeAdaptiveBenchmarkReportDraft(
					t.Context(),
					draft,
				)
			}
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error=%v want containing %q", err, testCase.want)
			}
		})
	}
}

func adaptiveBenchmarkReportBundle(
	t testing.TB,
	runID uuid.UUID,
	ownerID uuid.UUID,
) *adaptiveBenchmarkDecisionBundle {
	t.Helper()
	bundle, err := buildAdaptiveBenchmarkDecisionBundle(
		runID,
		ownerID,
		"openrouter",
		"production-model",
		adaptive.Policy{
			Epoch:      7,
			Version:    "policy-7",
			Mode:       adaptive.PolicyShadow,
			RolloutBPS: 0,
			Config:     json.RawMessage(`{"domain":"reasoning"}`),
			UpdatedAt: time.Date(
				2026,
				7,
				26,
				9,
				0,
				0,
				0,
				time.UTC,
			),
		},
	)
	if err != nil {
		t.Fatalf("buildAdaptiveBenchmarkDecisionBundle: %v", err)
	}
	return bundle
}

type adaptiveBenchmarkReportScenarioDriver struct{}

func (adaptiveBenchmarkReportScenarioDriver) RunScenario(
	_ context.Context,
	subject auraeval.AdaptiveBenchmarkSubject,
) (auraeval.AdaptiveBenchmarkDriverResult, error) {
	catalog := adaptiveBenchmarkReportCatalog(subject.Domain)
	probabilities := make(
		[]auraeval.AdaptiveBenchmarkActionProbability,
		len(catalog),
	)
	for index, actionID := range catalog {
		probability := float64(0)
		if actionID == adaptive.StaticActionID {
			probability = 1
		}
		probabilities[index] = auraeval.AdaptiveBenchmarkActionProbability{
			ActionID:    actionID,
			Probability: probability,
		}
	}
	return auraeval.AdaptiveBenchmarkDriverResult{
		RequestID: adaptiveBenchmarkReportUUID(
			"request-" + subject.ScenarioID,
		),
		AssignmentID: adaptiveBenchmarkReportUUID(
			"assignment-" + subject.ScenarioID,
		),
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: adaptive.StaticActionID,
		IntendedActionID:    adaptive.StaticActionID,
		ActualActionID:      adaptive.StaticActionID,
		ActionProbabilities: probabilities,
		LatencyNS:           1,
		CostBasis:           auraeval.AdaptiveBenchmarkCostUnknown,
		FailureReasonCodes: []string{
			auraeval.AdaptiveBenchmarkReasonModelTransportFailure,
		},
	}, nil
}

func (adaptiveBenchmarkReportScenarioDriver) RecordEvaluation(
	context.Context,
	auraeval.AdaptiveBenchmarkEvaluationRecord,
) (auraeval.AdaptiveBenchmarkRecordedEvaluation, error) {
	return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
		errors.New("unexpected deterministic evaluation")
}

func adaptiveBenchmarkReportCatalog(domain adaptive.Domain) []string {
	for _, catalog := range auraeval.AdaptiveBenchmarkActionCatalogs() {
		if catalog.Domain == domain {
			return catalog.ActionIDs
		}
	}
	return nil
}

type adaptiveBenchmarkReportControlDriver struct{}

func (adaptiveBenchmarkReportControlDriver) RunControl(
	_ context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (auraeval.AdaptiveBenchmarkControlResult, error) {
	result := auraeval.AdaptiveBenchmarkControlResult{
		Status:      auraeval.AdaptiveBenchmarkStatusPass,
		StartedAt:   "2026-07-26T10:00:01Z",
		CompletedAt: "2026-07-26T10:00:18Z",
		EvidenceIDs: []string{
			adaptiveBenchmarkReportUUID("control-" + control.ControlID),
		},
		ReasonCodes:                []string{},
		ObservedFailureReasonCodes: adaptiveBenchmarkReportExpectedFaults(control.ControlID),
	}
	if control.ControlID == "same_owner_concurrency" {
		evidence := adaptiveBenchmarkReportConcurrencyEvidence()
		result.Concurrency = &evidence
		result.EvidenceIDs = adaptiveBenchmarkReportConcurrencyIDs(evidence)
	}
	return result, nil
}

func adaptiveBenchmarkReportExpectedFaults(controlID string) []string {
	switch controlID {
	case "assignment_write_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonAssignmentMissing}
	case "delivery_write_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonDeliveryMissing}
	case "outcome_write_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonOutcomeMissing}
	case "model_transport_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonModelTransportFailure}
	case "memory_epoch_mismatch":
		return []string{auraeval.AdaptiveBenchmarkReasonMemoryEpochMismatch}
	case "settings_apply_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonSettingsApplyFailure}
	case "settings_restore_failure":
		return []string{auraeval.AdaptiveBenchmarkReasonSettingsRestoreFailure}
	case "stale_override_recovery":
		return []string{auraeval.AdaptiveBenchmarkReasonStaleOverrideRecovery}
	default:
		return []string{}
	}
}

func adaptiveBenchmarkReportConcurrencyEvidence() auraeval.AdaptiveBenchmarkConcurrencyEvidence {
	clients := []auraeval.AdaptiveBenchmarkConcurrencyClientEvidence{
		adaptiveBenchmarkReportConcurrencyClient(
			"C0",
			"A",
			"A0",
			"r01",
			0,
			1,
			10,
			12,
			11,
			13,
		),
		adaptiveBenchmarkReportConcurrencyClient(
			"C1",
			"A",
			"A0",
			"r02",
			2,
			14,
			15,
			17,
			16,
			18,
		),
		adaptiveBenchmarkReportConcurrencyClient(
			"C2",
			"A",
			"A1",
			"m01",
			3,
			4,
			8,
			10,
			9,
			11,
		),
		adaptiveBenchmarkReportConcurrencyClient(
			"C3",
			"B",
			"B0",
			"k01",
			3,
			5,
			7,
			9,
			8,
			10,
		),
	}
	clients[1].OwnerID = clients[0].OwnerID
	clients[1].ConversationID = clients[0].ConversationID
	clients[2].OwnerID = clients[0].OwnerID
	return auraeval.AdaptiveBenchmarkConcurrencyEvidence{
		Clients:        clients,
		ReleaseOrder:   []string{"C3", "C2", "C0", "C1"},
		WaitDeadlineAt: adaptiveBenchmarkReportControlTime(19),
	}
}

func adaptiveBenchmarkReportConcurrencyClient(
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
) auraeval.AdaptiveBenchmarkConcurrencyClientEvidence {
	return auraeval.AdaptiveBenchmarkConcurrencyClientEvidence{
		ClientID:          clientID,
		OwnerAlias:        ownerAlias,
		ConversationAlias: conversationAlias,
		ScenarioID:        scenarioID,
		OwnerID:           adaptiveBenchmarkReportUUID("owner-" + ownerAlias),
		ConversationID: adaptiveBenchmarkReportUUID(
			"conversation-" + conversationAlias,
		),
		RequestID: adaptiveBenchmarkReportUUID("request-control-" + clientID),
		AssignmentID: adaptiveBenchmarkReportUUID(
			"assignment-control-" + clientID,
		),
		DeliveryEventID: adaptiveBenchmarkReportUUID(
			"delivery-control-" + clientID,
		),
		StartedAt:         adaptiveBenchmarkReportControlTime(started),
		AssignmentAt:      adaptiveBenchmarkReportControlTime(assigned),
		ReleasedAt:        adaptiveBenchmarkReportControlTime(released),
		ModelStartedAt:    adaptiveBenchmarkReportControlTime(modelStarted),
		DeliveryAt:        adaptiveBenchmarkReportControlTime(delivered),
		TerminalAt:        adaptiveBenchmarkReportControlTime(terminal),
		DeliveryCount:     1,
		ChampionActionID:  adaptive.StaticActionID,
		ActionProbability: 1,
	}
}

func adaptiveBenchmarkReportConcurrencyIDs(
	evidence auraeval.AdaptiveBenchmarkConcurrencyEvidence,
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

func adaptiveBenchmarkReportControlTime(second int) string {
	return time.Date(
		2026,
		7,
		26,
		10,
		0,
		second,
		0,
		time.UTC,
	).Format(time.RFC3339Nano)
}

func adaptiveBenchmarkReportUUID(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}
