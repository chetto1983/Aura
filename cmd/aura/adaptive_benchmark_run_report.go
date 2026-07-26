package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/chetto1983/aura/internal/db"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

const adaptiveBenchmarkPromptTemplate = `{"scenario_id":"{{scenario_id}}",` +
	`"domain":"{{domain}}","prompt":"{{prompt}}"}`

type adaptiveBenchmarkScenarioDriverFactory func(
	*adaptiveBenchmarkChatEnvironment,
	adaptiveBenchmarkRunRequest,
	uuid.UUID,
) (auraeval.AdaptiveBenchmarkDriver, error)

type adaptiveBenchmarkControlDriverFactory func(
	*adaptiveBenchmarkChatEnvironment,
	adaptiveBenchmarkRunRequest,
	uuid.UUID,
) (auraeval.AdaptiveBenchmarkControlDriver, error)

type adaptiveBenchmarkReportFinalizer func(
	auraeval.AdaptiveBenchmarkReport,
	auraeval.AdaptiveBenchmarkScenarioRun,
	auraeval.AdaptiveBenchmarkControlRun,
) (auraeval.AdaptiveBenchmarkReport, error)

type adaptiveBenchmarkReportOps struct {
	now              func() time.Time
	workingDirectory func() (string, error)
	migrationHead    func() (int64, error)
	commands         adaptiveBenchmarkCommandRunner
	scenarioDriver   adaptiveBenchmarkScenarioDriverFactory
	controlDriver    adaptiveBenchmarkControlDriverFactory
	finalize         adaptiveBenchmarkReportFinalizer
}

type adaptiveBenchmarkReportDraft struct {
	bundle      *adaptiveBenchmarkDecisionBundle
	request     adaptiveBenchmarkRunRequest
	runID       uuid.UUID
	model       auraeval.AdaptiveBenchmarkModel
	startedAt   time.Time
	scenarioRun auraeval.AdaptiveBenchmarkScenarioRun
	controlRun  auraeval.AdaptiveBenchmarkControlRun
	ops         adaptiveBenchmarkReportOps
}

func runAdaptiveBenchmarkInChatEnvironment(
	ctx context.Context,
	environment *adaptiveBenchmarkChatEnvironment,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
) (*adaptiveBenchmarkReportDraft, error) {
	return runAdaptiveBenchmarkInChatEnvironmentWithOps(
		ctx,
		environment,
		request,
		runID,
		model,
		adaptiveBenchmarkReportOps{
			now:              time.Now,
			workingDirectory: os.Getwd,
			migrationHead:    db.MigrationHead,
			commands:         adaptiveBenchmarkOSCommandRunner{},
			scenarioDriver:   newAdaptiveBenchmarkAuraDriver,
			controlDriver:    newAdaptiveBenchmarkControlDriver,
			finalize:         auraeval.FinalizeAdaptiveBenchmarkReport,
		},
	)
}

func runAdaptiveBenchmarkInChatEnvironmentWithOps(
	ctx context.Context,
	environment *adaptiveBenchmarkChatEnvironment,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
	ops adaptiveBenchmarkReportOps,
) (*adaptiveBenchmarkReportDraft, error) {
	if err := validateAdaptiveBenchmarkReportOps(
		environment,
		request,
		runID,
		model,
		ops,
	); err != nil {
		return nil, err
	}
	startedAt := ops.now().UTC()
	dataset, err := auraeval.LoadFrozenAdaptiveBenchmarkDataset()
	if err != nil {
		return nil, fmt.Errorf("load adaptive benchmark dataset: %w", err)
	}
	scenarioDriver, err := ops.scenarioDriver(
		environment,
		request,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create adaptive benchmark scenario driver: %w",
			err,
		)
	}
	if scenarioDriver == nil {
		return nil, errors.New("create adaptive benchmark scenario driver")
	}
	scenarioRun, err := auraeval.RunAdaptiveBenchmarkScenarios(
		ctx,
		dataset,
		scenarioDriver,
	)
	if err != nil {
		return nil, fmt.Errorf("run adaptive benchmark scenarios: %w", err)
	}
	controlDriver, err := ops.controlDriver(environment, request, runID)
	if err != nil {
		return nil, fmt.Errorf(
			"create adaptive benchmark control driver: %w",
			err,
		)
	}
	if controlDriver == nil {
		return nil, errors.New("create adaptive benchmark control driver")
	}
	controlRun, err := auraeval.RunAdaptiveBenchmarkControls(
		ctx,
		controlDriver,
	)
	if err != nil {
		return nil, fmt.Errorf("run adaptive benchmark controls: %w", err)
	}
	return &adaptiveBenchmarkReportDraft{
		bundle:      environment.bundle,
		request:     request,
		runID:       runID,
		model:       model,
		startedAt:   startedAt,
		scenarioRun: scenarioRun,
		controlRun:  controlRun,
		ops:         ops,
	}, nil
}

func finalizeAdaptiveBenchmarkReportDraft(
	ctx context.Context,
	draft *adaptiveBenchmarkReportDraft,
) ([]byte, error) {
	if draft == nil ||
		draft.bundle == nil ||
		draft.ops.now == nil ||
		draft.ops.finalize == nil {
		return nil, errors.New(
			"adaptive benchmark report draft is unavailable",
		)
	}
	report, err := assembleAdaptiveBenchmarkReport(
		ctx,
		draft.bundle,
		draft.request,
		draft.runID,
		draft.model,
		draft.startedAt,
		draft.ops.now().UTC(),
		draft.scenarioRun,
		draft.controlRun,
		draft.ops,
	)
	if err != nil {
		return nil, err
	}
	payload, err := report.CanonicalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode adaptive benchmark report: %w", err)
	}
	return payload, nil
}

func assembleAdaptiveBenchmarkReport(
	ctx context.Context,
	bundle *adaptiveBenchmarkDecisionBundle,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
	startedAt time.Time,
	completedAt time.Time,
	scenarioRun auraeval.AdaptiveBenchmarkScenarioRun,
	controlRun auraeval.AdaptiveBenchmarkControlRun,
	ops adaptiveBenchmarkReportOps,
) (auraeval.AdaptiveBenchmarkReport, error) {
	root, err := ops.workingDirectory()
	if err != nil {
		return auraeval.AdaptiveBenchmarkReport{}, fmt.Errorf(
			"resolve adaptive benchmark source root: %w",
			err,
		)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return auraeval.AdaptiveBenchmarkReport{}, fmt.Errorf(
			"canonicalize adaptive benchmark source root: %w",
			err,
		)
	}
	root = filepath.Clean(root)
	git, err := inspectAdaptiveBenchmarkGit(ctx, ops.commands, root)
	if err != nil {
		return auraeval.AdaptiveBenchmarkReport{}, err
	}
	environment, err := adaptiveBenchmarkReportEnvironment(ops.migrationHead)
	if err != nil {
		return auraeval.AdaptiveBenchmarkReport{}, err
	}
	frozen := adaptiveBenchmarkFrozenInputs(bundle, scenarioRun)
	report := auraeval.AdaptiveBenchmarkReport{
		SchemaID: auraeval.AdaptiveBenchmarkReportSchemaID,
		Revision: auraeval.AdaptiveBenchmarkReportRevision,
		RunID:    runID.String(),
		Mode:     request.Mode,
		PortabilityOnly: request.Mode ==
			auraeval.AdaptiveBenchmarkModeQwenPortability,
		StartedAt:    startedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:  completedAt.UTC().Format(time.RFC3339Nano),
		Seed:         auraeval.AdaptiveBenchmarkSeed,
		Git:          git,
		Environment:  environment,
		Model:        model,
		FrozenInputs: frozen,
	}
	finalized, err := ops.finalize(
		report,
		scenarioRun,
		controlRun,
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkReport{}, fmt.Errorf(
			"finalize adaptive benchmark report: %w",
			err,
		)
	}
	return finalized, nil
}

func validateAdaptiveBenchmarkReportOps(
	environment *adaptiveBenchmarkChatEnvironment,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
	ops adaptiveBenchmarkReportOps,
) error {
	if environment == nil ||
		environment.env == nil ||
		environment.bundle == nil ||
		request.OwnerID == uuid.Nil ||
		request.OutputDir == "" ||
		!filepath.IsAbs(request.OutputDir) ||
		filepath.Clean(request.OutputDir) != request.OutputDir ||
		(request.Mode != auraeval.AdaptiveBenchmarkModeQwenPortability &&
			request.Mode != auraeval.AdaptiveBenchmarkModeProductionShadow) ||
		request.ConfirmedSettingsOverride !=
			(request.Mode == auraeval.AdaptiveBenchmarkModeQwenPortability) ||
		runID == uuid.Nil ||
		environment.bundle.runID != runID ||
		environment.bundle.primaryOwnerID != request.OwnerID ||
		environment.bundle.providerID != model.ProviderID ||
		environment.bundle.modelID != model.ModelID ||
		ops.now == nil ||
		ops.workingDirectory == nil ||
		ops.migrationHead == nil ||
		ops.commands == nil ||
		ops.scenarioDriver == nil ||
		ops.controlDriver == nil ||
		ops.finalize == nil {
		return errors.New("adaptive benchmark report runtime is unavailable")
	}
	return nil
}

func adaptiveBenchmarkReportEnvironment(
	migrationHead func() (int64, error),
) (auraeval.AdaptiveBenchmarkEnvironment, error) {
	head, err := migrationHead()
	if err != nil || head <= 0 {
		return auraeval.AdaptiveBenchmarkEnvironment{}, errors.New(
			"read adaptive benchmark migration head",
		)
	}
	auraVersion, _, _ := buildInfo()
	return auraeval.AdaptiveBenchmarkEnvironment{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		AuraVersion:   auraVersion,
		GoVersion:     runtime.Version(),
		MigrationHead: fmt.Sprintf("%04d", head),
	}, nil
}

func adaptiveBenchmarkFrozenInputs(
	bundle *adaptiveBenchmarkDecisionBundle,
	run auraeval.AdaptiveBenchmarkScenarioRun,
) auraeval.AdaptiveBenchmarkFrozenInputs {
	snapshotID := bundle.ID().String()
	snapshotSHA256 := bundle.SHA256()
	frozen := auraeval.AdaptiveBenchmarkFrozenInputs{
		DatasetID:            auraeval.AdaptiveBenchmarkDatasetID,
		DatasetRevision:      auraeval.AdaptiveBenchmarkDatasetRevision,
		DatasetSHA256:        auraeval.AdaptiveBenchmarkDatasetSHA256,
		PromptTemplateSHA256: adaptiveBenchmarkPromptTemplateSHA256(),
		EvaluatorID:          auraeval.AdaptiveBenchmarkEvaluatorID,
		EvaluatorRevision:    auraeval.AdaptiveBenchmarkEvaluatorRevision,
		ActionCatalogsSHA256: auraeval.AdaptiveBenchmarkActionCatalogsSHA256(),
		PolicyVersion:        bundle.PolicyVersion(),
		PolicySHA256:         bundle.PolicySHA256(),
		SnapshotID:           &snapshotID,
		SnapshotSHA256:       &snapshotSHA256,
		Seed:                 auraeval.AdaptiveBenchmarkSeed,
	}
	if run.PriceTable != nil {
		priceTableID := run.PriceTable.ID
		priceTableRevision := run.PriceTable.Revision
		priceTableSHA256 := run.PriceTable.SHA256
		frozen.PriceTableID = &priceTableID
		frozen.PriceTableRevision = &priceTableRevision
		frozen.PriceTableSHA256 = &priceTableSHA256
	}
	return frozen
}

func adaptiveBenchmarkPromptTemplateSHA256() string {
	sum := sha256.Sum256([]byte(adaptiveBenchmarkPromptTemplate))
	return hex.EncodeToString(sum[:])
}
