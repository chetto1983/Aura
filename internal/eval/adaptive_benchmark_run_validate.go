package eval

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ValidateAdaptiveBenchmarkReport verifies schema, provenance, facts, and sums.
func ValidateAdaptiveBenchmarkReport(report AdaptiveBenchmarkReport) error {
	if report.SchemaID != AdaptiveBenchmarkReportSchemaID ||
		report.Revision != AdaptiveBenchmarkReportRevision ||
		report.Seed != AdaptiveBenchmarkSeed ||
		report.PortabilityOnly !=
			(report.Mode == AdaptiveBenchmarkModeQwenPortability) ||
		(report.Mode != AdaptiveBenchmarkModeQwenPortability &&
			report.Mode != AdaptiveBenchmarkModeProductionShadow) {
		return errors.New("adaptive benchmark report identity is invalid")
	}
	if !validBenchmarkUUID(report.RunID) {
		return errors.New("adaptive benchmark run_id is invalid")
	}
	started, err := parseAdaptiveBenchmarkTime(report.StartedAt)
	if err != nil {
		return err
	}
	completed, err := parseAdaptiveBenchmarkTime(report.CompletedAt)
	if err != nil || completed.Before(started) {
		return errors.New("adaptive benchmark report time range is invalid")
	}
	if err := validateAdaptiveBenchmarkGit(report.Git); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkEnvironment(report.Environment); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkModel(report.Mode, report.Model); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkFrozenInputs(
		report.FrozenInputs,
		report.Summary.CostCount,
	); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkScenarios(report.Scenarios); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkControls(
		report.Controls,
		started,
		completed,
	); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkAggregateBounds(
		report.Scenarios,
	); err != nil {
		return err
	}
	wantSummary := BuildAdaptiveBenchmarkSummary(report.Scenarios)
	if !reflect.DeepEqual(report.Summary, wantSummary) {
		return errors.New("adaptive benchmark summary does not match scenarios")
	}
	if report.Verdict != ExpectedAdaptiveBenchmarkVerdict(report) {
		return errors.New("adaptive benchmark verdict does not match evidence")
	}
	return nil
}

func validateAdaptiveBenchmarkGit(git AdaptiveBenchmarkGit) error {
	if !validBenchmarkGitRevision(git.Revision) ||
		git.ChangedFiles == nil ||
		(!git.Dirty && len(git.ChangedFiles) != 0) ||
		(git.Dirty && len(git.ChangedFiles) == 0) {
		return errors.New("adaptive benchmark git provenance is invalid")
	}
	previous := ""
	for _, changed := range git.ChangedFiles {
		if !validBenchmarkRelativePath(changed.Path) ||
			!validBenchmarkSHA256(changed.SHA256) ||
			(previous != "" && changed.Path <= previous) {
			return errors.New("adaptive benchmark changed files are invalid")
		}
		previous = changed.Path
	}
	return nil
}

func validBenchmarkGitRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func validBenchmarkRelativePath(value string) bool {
	return value != "" &&
		!strings.Contains(value, "\\") &&
		!strings.HasPrefix(value, "/") &&
		path.Clean(value) == value &&
		value != "." &&
		!strings.HasPrefix(value, "../")
}

func validateAdaptiveBenchmarkEnvironment(
	environment AdaptiveBenchmarkEnvironment,
) error {
	for _, value := range []string{
		environment.OS,
		environment.Arch,
		environment.AuraVersion,
		environment.GoVersion,
		environment.MigrationHead,
	} {
		if !validBenchmarkMetadata(value, 128) {
			return errors.New("adaptive benchmark environment is invalid")
		}
	}
	for _, value := range []*string{
		environment.Neo4jRevision,
		environment.AgentMemoryRevision,
	} {
		if value != nil && !validBenchmarkMetadata(*value, 128) {
			return errors.New(
				"adaptive benchmark environment provenance is invalid",
			)
		}
	}
	return nil
}

func validateAdaptiveBenchmarkModel(
	mode AdaptiveBenchmarkMode,
	model AdaptiveBenchmarkModel,
) error {
	if !validBenchmarkMetadata(model.ProviderID, 128) ||
		!validBenchmarkMetadata(model.ModelID, 256) ||
		(model.BaseURLClass != AdaptiveBenchmarkBaseURLLoopback &&
			model.BaseURLClass != AdaptiveBenchmarkBaseURLRemote) {
		return errors.New("adaptive benchmark model identity is invalid")
	}
	for _, value := range []*string{
		model.ServerName,
		model.ServerVersion,
		model.ServerRevision,
		model.ArtifactName,
		model.SnapshotRevision,
	} {
		if value != nil && !validBenchmarkMetadata(*value, 256) {
			return errors.New("adaptive benchmark model provenance is unsafe")
		}
	}
	if model.ArtifactSHA256 != nil &&
		!validBenchmarkSHA256(*model.ArtifactSHA256) {
		return errors.New("adaptive benchmark model artifact hash is invalid")
	}
	if model.ImageDigest != nil &&
		(!strings.HasPrefix(*model.ImageDigest, "sha256:") ||
			!validBenchmarkSHA256(strings.TrimPrefix(
				*model.ImageDigest,
				"sha256:",
			))) {
		return errors.New("adaptive benchmark model image digest is invalid")
	}
	if mode != AdaptiveBenchmarkModeQwenPortability {
		return nil
	}
	if model.ProviderID != "llamacpp" ||
		model.ModelID != "qwen" ||
		model.BaseURLClass != AdaptiveBenchmarkBaseURLLoopback ||
		model.ServerVersion == nil ||
		model.ServerRevision == nil ||
		model.ArtifactName == nil ||
		*model.ArtifactName != AdaptiveBenchmarkQwenArtifactName ||
		model.ArtifactSHA256 == nil ||
		*model.ArtifactSHA256 != AdaptiveBenchmarkQwenArtifactSHA256 ||
		model.SnapshotRevision == nil ||
		model.ImageDigest == nil {
		return errors.New("adaptive benchmark Qwen provenance is invalid")
	}
	return nil
}

func validateAdaptiveBenchmarkFrozenInputs(
	inputs AdaptiveBenchmarkFrozenInputs,
	costCount int,
) error {
	if inputs.DatasetID != AdaptiveBenchmarkDatasetID ||
		inputs.DatasetRevision != AdaptiveBenchmarkDatasetRevision ||
		inputs.Seed != AdaptiveBenchmarkSeed ||
		inputs.DatasetSHA256 != AdaptiveBenchmarkDatasetSHA256 ||
		!validBenchmarkSHA256(inputs.PromptTemplateSHA256) ||
		inputs.EvaluatorID != AdaptiveBenchmarkEvaluatorID ||
		inputs.EvaluatorRevision != AdaptiveBenchmarkEvaluatorRevision ||
		!validBenchmarkSHA256(inputs.ActionCatalogsSHA256) ||
		inputs.ActionCatalogsSHA256 != AdaptiveBenchmarkActionCatalogsSHA256() ||
		!validBenchmarkMetadata(inputs.PolicyVersion, 128) ||
		!validBenchmarkSHA256(inputs.PolicySHA256) {
		return errors.New("adaptive benchmark frozen input provenance is invalid")
	}
	if inputs.CalibrationSHA256 != nil &&
		!validBenchmarkSHA256(*inputs.CalibrationSHA256) {
		return errors.New("adaptive benchmark calibration provenance is invalid")
	}
	if (inputs.SnapshotID == nil) != (inputs.SnapshotSHA256 == nil) {
		return errors.New(
			"adaptive benchmark snapshot provenance is inconsistent",
		)
	}
	if inputs.SnapshotID != nil &&
		(!validBenchmarkUUID(*inputs.SnapshotID) ||
			!validBenchmarkSHA256(*inputs.SnapshotSHA256)) {
		return errors.New("adaptive benchmark snapshot provenance is invalid")
	}
	priceFields := 0
	if inputs.PriceTableID != nil {
		priceFields++
	}
	if inputs.PriceTableRevision != nil {
		priceFields++
	}
	if inputs.PriceTableSHA256 != nil {
		priceFields++
	}
	if priceFields != 0 && priceFields != 3 {
		return errors.New("adaptive benchmark price provenance is inconsistent")
	}
	if priceFields == 3 &&
		(!validBenchmarkMetadata(*inputs.PriceTableID, 128) ||
			*inputs.PriceTableRevision <= 0 ||
			!validBenchmarkSHA256(*inputs.PriceTableSHA256)) {
		return errors.New("adaptive benchmark price provenance is invalid")
	}
	if costCount == 0 && priceFields != 0 {
		return errors.New("adaptive benchmark unused price provenance is forbidden")
	}
	return nil
}

func parseAdaptiveBenchmarkTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC ||
		parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, errors.New(
			"adaptive benchmark timestamp is not canonical UTC RFC3339Nano",
		)
	}
	return parsed, nil
}

func validBenchmarkUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func validBenchmarkMetadata(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return false
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"://", "password=", "token=", "api_key=", "secret=", "authorization:",
	} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return !slices.ContainsFunc([]rune(value), unicode.IsControl)
}

func benchmarkValidationError(format string, values ...any) error {
	return fmt.Errorf("adaptive benchmark "+format, values...)
}
