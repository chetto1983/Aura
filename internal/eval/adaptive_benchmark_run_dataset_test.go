package eval

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
)

func TestLoadFrozenAdaptiveBenchmarkDatasetMatchesSpikeSource(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(
		"testdata/adaptive_benchmark_dataset.json",
	)
	if err != nil {
		t.Fatalf("read spike dataset: %v", err)
	}
	if !bytes.Equal(frozenAdaptiveBenchmarkDatasetJSON, source) {
		t.Fatal("embedded runtime dataset drifted from spike source")
	}
	dataset, err := LoadFrozenAdaptiveBenchmarkDataset()
	if err != nil {
		t.Fatalf("LoadFrozenAdaptiveBenchmarkDataset: %v", err)
	}
	digest, err := dataset.SHA256()
	if err != nil {
		t.Fatalf("dataset SHA256: %v", err)
	}
	if digest != AdaptiveBenchmarkDatasetSHA256 {
		t.Fatalf("embedded dataset SHA-256 = %q", digest)
	}
}

func TestLoadAdaptiveBenchmarkDatasetFreezesFiftyTwoSyntheticScenarios(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(
		"testdata/adaptive_benchmark_dataset.json",
	)
	if err != nil {
		t.Fatalf("read frozen dataset: %v", err)
	}
	dataset, err := LoadAdaptiveBenchmarkDataset(payload)
	if err != nil {
		t.Fatalf("LoadAdaptiveBenchmarkDataset: %v", err)
	}
	if dataset.DatasetID != AdaptiveBenchmarkDatasetID ||
		dataset.Revision != AdaptiveBenchmarkDatasetRevision ||
		len(dataset.Scenarios) != 52 {
		t.Fatalf(
			"dataset identity/count = %q/%d/%d",
			dataset.DatasetID,
			dataset.Revision,
			len(dataset.Scenarios),
		)
	}

	wantCounts := map[adaptive.Domain]int{
		adaptive.DomainReasoning:     12,
		adaptive.DomainToolDiscovery: 12,
		adaptive.DomainSkillRouting:  12,
		adaptive.DomainKnowledge:     8,
		adaptive.DomainMemoryRecall:  8,
	}
	gotCounts := make(map[adaptive.Domain]int, len(wantCounts))
	seen := make(map[string]struct{}, len(dataset.Scenarios))
	for _, scenario := range dataset.Scenarios {
		gotCounts[scenario.Domain]++
		if _, duplicate := seen[scenario.ScenarioID]; duplicate {
			t.Fatalf("duplicate scenario %q", scenario.ScenarioID)
		}
		seen[scenario.ScenarioID] = struct{}{}
		if scenario.Prompt == "" || scenario.Expected == "" ||
			scenario.EvaluatorID != AdaptiveBenchmarkEvaluatorID ||
			scenario.EvaluatorRevision != AdaptiveBenchmarkEvaluatorRevision {
			t.Fatalf("scenario %q is not frozen: %#v", scenario.ScenarioID, scenario)
		}
	}
	for domain, want := range wantCounts {
		if got := gotCounts[domain]; got != want {
			t.Errorf("%s scenarios = %d, want %d", domain, got, want)
		}
	}
	digest, err := dataset.SHA256()
	if err != nil {
		t.Fatalf("dataset SHA256: %v", err)
	}
	if digest !=
		"ab65400e241a9a000f206f0aa360acc9d43272acee9cd69906e14439fce06df0" {
		t.Fatalf("dataset SHA-256 = %q", digest)
	}
}

func TestAdaptiveBenchmarkScenarioOrderUsesRegisteredSHASchedule(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(
		"testdata/adaptive_benchmark_dataset.json",
	)
	if err != nil {
		t.Fatalf("read frozen dataset: %v", err)
	}
	dataset, err := LoadAdaptiveBenchmarkDataset(payload)
	if err != nil {
		t.Fatalf("LoadAdaptiveBenchmarkDataset: %v", err)
	}
	ordered := AdaptiveBenchmarkScenarioOrder(dataset.Scenarios)
	got := make([]string, len(ordered))
	for index, scenario := range ordered {
		got[index] = scenario.ScenarioID
	}
	want := strings.Split(
		"r01,t06,t05,s01,k06,r05,s08,s12,m05,m06,m07,s06,t08,r06,"+
			"t12,k01,r03,r07,s02,t11,t09,s03,s07,t03,r09,r11,s05,r08,"+
			"t07,s11,r04,m04,m08,k02,t02,k08,r12,r10,m02,k05,m01,s09,"+
			"t10,m03,t04,s04,k04,t01,k07,k03,s10,r02",
		",",
	)
	if !slices.Equal(got, want) {
		t.Fatalf("scheduled scenario IDs = %v, want %v", got, want)
	}
}

func TestLoadAdaptiveBenchmarkDatasetRejectsNoncanonicalJSON(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(
		"testdata/adaptive_benchmark_dataset.json",
	)
	if err != nil {
		t.Fatalf("read frozen dataset: %v", err)
	}
	canonical := strings.TrimSuffix(string(payload), "\n")
	tests := []struct {
		name    string
		payload string
	}{
		{
			name: "unknown field",
			payload: strings.Replace(
				canonical,
				`"revision":1`,
				`"revision":1,"unknown":true`,
				1,
			),
		},
		{
			name: "duplicate field",
			payload: strings.Replace(
				canonical,
				`"revision":1`,
				`"revision":1,"revision":1`,
				1,
			),
		},
		{
			name: "scenario key order",
			payload: strings.Replace(
				canonical,
				`{"scenario_id":"r01","domain":"reasoning"`,
				`{"domain":"reasoning","scenario_id":"r01"`,
				1,
			),
		},
		{name: "leading whitespace", payload: " " + canonical},
		{name: "trailing data", payload: canonical + `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadAdaptiveBenchmarkDataset(
				[]byte(test.payload),
			); err == nil {
				t.Fatal("LoadAdaptiveBenchmarkDataset accepted noncanonical JSON")
			}
		})
	}
}

func TestLoadAdaptiveBenchmarkDatasetRejectsRegisteredCorpusMutation(
	t *testing.T,
) {
	t.Parallel()

	payload, err := os.ReadFile(
		"testdata/adaptive_benchmark_dataset.json",
	)
	if err != nil {
		t.Fatalf("read frozen dataset: %v", err)
	}
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "prompt",
			from: `"prompt":"Return only the integer: 18 multiplied by 23."`,
			to:   `"prompt":"Return only the integer: 18 multiplied by 24."`,
		},
		{
			name: "expected",
			from: `"expected":"414"`,
			to:   `"expected":"415"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mutated := strings.Replace(
				string(payload),
				test.from,
				test.to,
				1,
			)
			if mutated == string(payload) {
				t.Fatalf("fixture does not contain %s", test.from)
			}
			if _, err := LoadAdaptiveBenchmarkDataset(
				[]byte(mutated),
			); err == nil {
				t.Fatal("loader accepted a mutated registered corpus")
			}
		})
	}
}

func TestAdaptiveBenchmarkActionCatalogsMatchProductionOrder(t *testing.T) {
	t.Parallel()

	got := AdaptiveBenchmarkActionCatalogs()
	want := []AdaptiveBenchmarkActionCatalog{
		{
			Domain: adaptive.DomainReasoning,
			ActionIDs: []string{
				"reasoning_high", "reasoning_low", "reasoning_none", "static",
			},
		},
		{
			Domain:    adaptive.DomainToolDiscovery,
			ActionIDs: []string{"bm25", "semantic", "static"},
		},
		{
			Domain:    adaptive.DomainSkillRouting,
			ActionIDs: []string{"catalog", "static"},
		},
		{
			Domain: adaptive.DomainKnowledge,
			ActionIDs: []string{
				"sparse", "static", "vector", "vector_rerank",
				"vector_rerank_expand",
			},
		},
		{
			Domain: adaptive.DomainMemoryRecall,
			ActionIDs: []string{
				"static", "long_term_top_4", "long_term_top_8",
			},
		},
	}
	if !slices.EqualFunc(got, want, func(
		left AdaptiveBenchmarkActionCatalog,
		right AdaptiveBenchmarkActionCatalog,
	) bool {
		return left.Domain == right.Domain &&
			slices.Equal(left.ActionIDs, right.ActionIDs)
	}) {
		t.Fatalf("action catalogs = %#v, want %#v", got, want)
	}
	first := AdaptiveBenchmarkActionCatalogs()
	first[0].ActionIDs[0] = "mutated"
	if AdaptiveBenchmarkActionCatalogs()[0].ActionIDs[0] == "mutated" {
		t.Fatal("AdaptiveBenchmarkActionCatalogs leaked mutable package state")
	}
	if !validBenchmarkSHA256(AdaptiveBenchmarkActionCatalogsSHA256()) {
		t.Fatal("catalog digest is not a lowercase SHA-256")
	}
}

func TestEvaluateAdaptiveBenchmarkScenarioUsesExactRegisteredNormalizers(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name     string
		scenario AdaptiveBenchmarkScenario
		observed AdaptiveBenchmarkObserved
		wantPass bool
		wantCode string
	}{
		{
			name: "answer normalization",
			scenario: benchmarkScenario(
				"r01", adaptive.DomainReasoning, "414",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "  `414.` "},
			wantPass: true,
		},
		{
			name: "answer explanation is not exact",
			scenario: benchmarkScenario(
				"r01", adaptive.DomainReasoning, "414",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "the answer is 414"},
			wantCode: AdaptiveBenchmarkReasonAnswerMismatch,
		},
		{
			name: "case normalization",
			scenario: benchmarkScenario(
				"r02", adaptive.DomainReasoning, "rome",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "ROME"},
			wantPass: true,
		},
		{
			name: "currency punctuation normalization",
			scenario: benchmarkScenario(
				"r03", adaptive.DomainReasoning, "0.05",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "$0.05."},
			wantPass: true,
		},
		{
			name: "number word normalization",
			scenario: benchmarkScenario(
				"k02", adaptive.DomainKnowledge, "7",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "Seven."},
			wantPass: true,
		},
		{
			name: "eighteen and whitespace normalization",
			scenario: benchmarkScenario(
				"k06", adaptive.DomainKnowledge, "18 days",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "Eighteen    days."},
			wantPass: true,
		},
		{
			name: "ninety normalization",
			scenario: benchmarkScenario(
				"k07", adaptive.DomainKnowledge, "90 days",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "Ninety days."},
			wantPass: true,
		},
		{
			name: "number replacement has token boundary",
			scenario: benchmarkScenario(
				"k02", adaptive.DomainKnowledge, "7",
			),
			observed: AdaptiveBenchmarkObserved{Answer: "sevenfold"},
			wantCode: AdaptiveBenchmarkReasonAnswerMismatch,
		},
		{
			name: "time connector normalization",
			scenario: benchmarkScenario(
				"k05", adaptive.DomainKnowledge, "tuesday 02:00 utc",
			),
			observed: AdaptiveBenchmarkObserved{
				Answer: "Tuesday at 02:00 UTC.",
			},
			wantPass: true,
		},
		{
			name: "tool ID exact",
			scenario: benchmarkScenario(
				"t01", adaptive.DomainToolDiscovery, "weather_now",
			),
			observed: AdaptiveBenchmarkObserved{ToolID: "weather_now"},
			wantPass: true,
		},
		{
			name: "tool ID mismatch",
			scenario: benchmarkScenario(
				"t01", adaptive.DomainToolDiscovery, "weather_now",
			),
			observed: AdaptiveBenchmarkObserved{ToolID: "web_search"},
			wantCode: AdaptiveBenchmarkReasonToolMismatch,
		},
		{
			name: "skill ID exact",
			scenario: benchmarkScenario(
				"s01", adaptive.DomainSkillRouting, "golang-testing",
			),
			observed: AdaptiveBenchmarkObserved{SkillID: "golang-testing"},
			wantPass: true,
		},
		{
			name: "skill ID is case sensitive",
			scenario: benchmarkScenario(
				"s01", adaptive.DomainSkillRouting, "golang-testing",
			),
			observed: AdaptiveBenchmarkObserved{SkillID: "Golang-Testing"},
			wantCode: AdaptiveBenchmarkReasonSkillMismatch,
		},
		{
			name: "memory IDs preserve order",
			scenario: benchmarkScenario(
				"m01",
				adaptive.DomainMemoryRecall,
				"preference:temperature-unit,fact:answer-style",
			),
			observed: AdaptiveBenchmarkObserved{
				ResultIDs: []string{
					"preference:temperature-unit", "fact:answer-style",
				},
			},
			wantPass: true,
		},
		{
			name: "memory IDs reject reordered facts",
			scenario: benchmarkScenario(
				"m01",
				adaptive.DomainMemoryRecall,
				"preference:temperature-unit,fact:answer-style",
			),
			observed: AdaptiveBenchmarkObserved{
				ResultIDs: []string{
					"fact:answer-style", "preference:temperature-unit",
				},
			},
			wantCode: AdaptiveBenchmarkReasonResultOrderMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := EvaluateAdaptiveBenchmarkScenario(
				test.scenario,
				test.observed,
			)
			if err != nil {
				t.Fatalf("EvaluateAdaptiveBenchmarkScenario: %v", err)
			}
			if got.Passed != test.wantPass {
				t.Fatalf("Passed = %v, want %v", got.Passed, test.wantPass)
			}
			if test.wantCode != "" &&
				!slices.Contains(got.ReasonCodes, test.wantCode) {
				t.Fatalf(
					"reason codes = %v, want %q",
					got.ReasonCodes,
					test.wantCode,
				)
			}
		})
	}
}

func TestAdaptiveBenchmarkControlContractsAreFixed(t *testing.T) {
	t.Parallel()

	got := AdaptiveBenchmarkControlMatrix()
	want := []string{
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
	if !slices.Equal(got, want) {
		t.Fatalf("control matrix = %v, want %v", got, want)
	}

	schedule := AdaptiveBenchmarkConcurrencySchedule()
	if len(schedule.Clients) != 4 ||
		schedule.Clients[0] != (AdaptiveBenchmarkConcurrencyClient{
			ClientID: "C0", OwnerAlias: "A",
			ConversationAlias: "A0", ScenarioID: "r01",
		}) ||
		!slices.Equal(
			schedule.ReleaseOrder,
			[]string{"C3", "C2", "C0", "C1"},
		) {
		t.Fatalf("concurrency schedule = %#v", schedule)
	}
}

func benchmarkScenario(
	id string,
	domain adaptive.Domain,
	expected string,
) AdaptiveBenchmarkScenario {
	return AdaptiveBenchmarkScenario{
		ScenarioID:        id,
		Domain:            domain,
		Prompt:            "synthetic prompt",
		Expected:          expected,
		EvaluatorID:       AdaptiveBenchmarkEvaluatorID,
		EvaluatorRevision: AdaptiveBenchmarkEvaluatorRevision,
	}
}
