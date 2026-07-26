package eval

import (
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

func validAdaptiveBenchmarkReport(
	t testing.TB,
	mode AdaptiveBenchmarkMode,
) AdaptiveBenchmarkReport {
	t.Helper()

	payload, err := os.ReadFile(
		"../../.planning/spikes/106-adaptive-aura-portability/dataset.json",
	)
	if err != nil {
		t.Fatalf("read dataset: %v", err)
	}
	dataset, err := LoadAdaptiveBenchmarkDataset(payload)
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	datasetHash, err := dataset.SHA256()
	if err != nil {
		t.Fatalf("dataset SHA256: %v", err)
	}
	scenarios := make(
		[]AdaptiveBenchmarkScenarioRecord,
		0,
		len(dataset.Scenarios),
	)
	for index, scenario := range AdaptiveBenchmarkScenarioOrder(dataset.Scenarios) {
		catalog := benchmarkCatalogForDomain(scenario.Domain)
		probabilities := make(
			[]AdaptiveBenchmarkActionProbability,
			len(catalog),
		)
		for actionIndex, actionID := range catalog {
			probability := float64(0)
			if actionID == adaptive.StaticActionID {
				probability = 1
			}
			probabilities[actionIndex] = AdaptiveBenchmarkActionProbability{
				ActionID: actionID, Probability: probability,
			}
		}
		scenarios = append(scenarios, AdaptiveBenchmarkScenarioRecord{
			ScenarioID: scenario.ScenarioID, Domain: scenario.Domain,
			RequestID: benchmarkString(
				benchmarkUUID("request-" + scenario.ScenarioID),
			),
			AssignmentID: benchmarkString(
				benchmarkUUID("assignment-" + scenario.ScenarioID),
			),
			DeliveryEventID: benchmarkString(
				benchmarkUUID("delivery-" + scenario.ScenarioID),
			),
			QualityOutcomeID: benchmarkString(
				benchmarkUUID("quality-" + scenario.ScenarioID),
			),
			ChampionActionID:    adaptive.StaticActionID,
			RecommendedActionID: catalog[0],
			IntendedActionID:    adaptive.StaticActionID,
			ActualActionID:      adaptive.StaticActionID,
			ActionProbabilities: probabilities,
			Quality: &AdaptiveBenchmarkObservation{
				EvaluatorID:       AdaptiveBenchmarkEvaluatorID,
				EvaluatorRevision: AdaptiveBenchmarkEvaluatorRevision,
				Value:             benchmarkFloat(1), Eligible: true,
				ReasonCodes: []string{},
			},
			PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2,
			LatencyNS:   int64(index + 1),
			Status:      AdaptiveBenchmarkStatusPass,
			ReasonCodes: []string{},
		})
	}
	controls := make(
		[]AdaptiveBenchmarkControlRecord,
		len(adaptiveBenchmarkControlMatrix),
	)
	for index, controlID := range adaptiveBenchmarkControlMatrix {
		controls[index] = AdaptiveBenchmarkControlRecord{
			Status:      AdaptiveBenchmarkStatusPass,
			StartedAt:   "2026-07-26T10:00:00Z",
			CompletedAt: "2026-07-26T10:00:01Z",
			EvidenceIDs: []string{
				benchmarkUUID("control-" + controlID),
			},
			ReasonCodes: []string{},
		}
	}
	report := AdaptiveBenchmarkReport{
		SchemaID: AdaptiveBenchmarkReportSchemaID,
		Revision: AdaptiveBenchmarkReportRevision,
		RunID:    benchmarkUUID("run"), Mode: mode,
		PortabilityOnly: mode == AdaptiveBenchmarkModeQwenPortability,
		StartedAt:       "2026-07-26T10:00:00Z",
		CompletedAt:     "2026-07-26T10:10:00Z",
		Seed:            AdaptiveBenchmarkSeed,
		Git: AdaptiveBenchmarkGit{
			Revision: "0123456789abcdef0123456789abcdef01234567",
			Dirty:    false, ChangedFiles: []AdaptiveBenchmarkChangedFile{},
		},
		Environment: AdaptiveBenchmarkEnvironment{
			OS: "linux", Arch: "amd64", AuraVersion: "task10-test",
			GoVersion: "go1.26.5", MigrationHead: "0077",
			Neo4jRevision:       benchmarkString("neo4j-2026.07"),
			AgentMemoryRevision: benchmarkString("agent-memory-2026.07"),
		},
		Model: validAdaptiveBenchmarkModel(mode),
		FrozenInputs: AdaptiveBenchmarkFrozenInputs{
			DatasetID:            AdaptiveBenchmarkDatasetID,
			DatasetRevision:      AdaptiveBenchmarkDatasetRevision,
			DatasetSHA256:        datasetHash,
			PromptTemplateSHA256: strings.Repeat("1", 64),
			EvaluatorID:          AdaptiveBenchmarkEvaluatorID,
			EvaluatorRevision:    AdaptiveBenchmarkEvaluatorRevision,
			ActionCatalogsSHA256: AdaptiveBenchmarkActionCatalogsSHA256(),
			PolicyVersion:        "task10-shadow-v1",
			PolicySHA256:         strings.Repeat("2", 64),
			SnapshotID:           benchmarkString(benchmarkUUID("snapshot")),
			SnapshotSHA256:       benchmarkString(strings.Repeat("3", 64)),
			Seed:                 AdaptiveBenchmarkSeed,
		},
		Scenarios: scenarios, Controls: controls,
		Verdict: AdaptiveBenchmarkStatusPass,
	}
	report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
	return report
}

func validAdaptiveBenchmarkModel(
	mode AdaptiveBenchmarkMode,
) AdaptiveBenchmarkModel {
	if mode == AdaptiveBenchmarkModeQwenPortability {
		return AdaptiveBenchmarkModel{
			ProviderID: "llamacpp", ModelID: "qwen",
			BaseURLClass:   AdaptiveBenchmarkBaseURLLoopback,
			ServerName:     benchmarkString("llama.cpp"),
			ServerVersion:  benchmarkString("b9994"),
			ServerRevision: benchmarkString("14d3ba45f"),
			ArtifactName:   benchmarkString(AdaptiveBenchmarkQwenArtifactName),
			ArtifactSHA256: benchmarkString(
				AdaptiveBenchmarkQwenArtifactSHA256,
			),
			SnapshotRevision: benchmarkString("unsloth-revision"),
			ImageDigest: benchmarkString(
				"sha256:" + strings.Repeat("4", 64),
			),
		}
	}
	return AdaptiveBenchmarkModel{
		ProviderID: "openrouter", ModelID: "configured-production-model",
		BaseURLClass: AdaptiveBenchmarkBaseURLRemote,
	}
}

func benchmarkCatalogForDomain(domain adaptive.Domain) []string {
	for _, catalog := range AdaptiveBenchmarkActionCatalogs() {
		if catalog.Domain == domain {
			return catalog.ActionIDs
		}
	}
	return nil
}

func benchmarkUUID(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

func benchmarkString(value string) *string {
	return &value
}

func benchmarkFloat(value float64) *float64 {
	return &value
}

func benchmarkInt(value int) *int {
	return &value
}
