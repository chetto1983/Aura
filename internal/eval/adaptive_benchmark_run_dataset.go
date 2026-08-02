package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/runner"
)

// Dataset, seed, evaluator, and reason identifiers are frozen by Amendment #93.11.
const (
	AdaptiveBenchmarkDatasetID       = "aura/adaptive-aura-benchmark-dataset"
	AdaptiveBenchmarkDatasetRevision = 1
	AdaptiveBenchmarkDatasetSHA256   = "ab65400e241a9a000f206f0aa360acc9d" +
		"43272acee9cd69906e14439fce06df0"
	AdaptiveBenchmarkSeed uint64 = 10620260726

	AdaptiveBenchmarkEvaluatorID       = "aura/adaptive-deterministic-checker"
	AdaptiveBenchmarkEvaluatorRevision = 1

	AdaptiveBenchmarkReasonAnswerMismatch      = "answer_mismatch"
	AdaptiveBenchmarkReasonResultOrderMismatch = "result_order_mismatch"
	AdaptiveBenchmarkReasonSkillMismatch       = "skill_mismatch"
	AdaptiveBenchmarkReasonToolMismatch        = "tool_mismatch"
)

// AdaptiveBenchmarkDataset is the frozen, synthetic Task 10 input corpus.
type AdaptiveBenchmarkDataset struct {
	DatasetID string                      `json:"dataset_id"`
	Revision  int                         `json:"revision"`
	Scenarios []AdaptiveBenchmarkScenario `json:"scenarios"`
}

// AdaptiveBenchmarkScenario contains only committed synthetic benchmark data.
type AdaptiveBenchmarkScenario struct {
	ScenarioID        string          `json:"scenario_id"`
	Domain            adaptive.Domain `json:"domain"`
	Prompt            string          `json:"prompt"`
	Expected          string          `json:"expected"`
	EvaluatorID       string          `json:"evaluator_id"`
	EvaluatorRevision int             `json:"evaluator_revision"`
}

// LoadAdaptiveBenchmarkDataset accepts only the exact registered JSON encoding.
func LoadAdaptiveBenchmarkDataset(payload []byte) (AdaptiveBenchmarkDataset, error) {
	var dataset AdaptiveBenchmarkDataset
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return AdaptiveBenchmarkDataset{}, fmt.Errorf(
			"decode adaptive benchmark dataset: %w",
			err,
		)
	}
	if err := requireBenchmarkJSONEOF(decoder); err != nil {
		return AdaptiveBenchmarkDataset{}, err
	}
	if err := validateAdaptiveBenchmarkDataset(dataset); err != nil {
		return AdaptiveBenchmarkDataset{}, err
	}
	canonical, err := json.Marshal(dataset)
	if err != nil {
		return AdaptiveBenchmarkDataset{}, fmt.Errorf(
			"marshal adaptive benchmark dataset: %w",
			err,
		)
	}
	canonicalFile := append(slices.Clone(canonical), '\n')
	if !bytes.Equal(payload, canonical) &&
		!bytes.Equal(payload, canonicalFile) {
		return AdaptiveBenchmarkDataset{}, errors.New(
			"adaptive benchmark dataset is not canonical JSON",
		)
	}
	return dataset, nil
}

// SHA256 returns the digest of the canonical dataset JSON, excluding file LF.
func (dataset AdaptiveBenchmarkDataset) SHA256() (string, error) {
	if err := validateAdaptiveBenchmarkDataset(dataset); err != nil {
		return "", err
	}
	return adaptiveBenchmarkDatasetSHA256(dataset)
}

func adaptiveBenchmarkDatasetSHA256(
	dataset AdaptiveBenchmarkDataset,
) (string, error) {
	canonical, err := json.Marshal(dataset)
	if err != nil {
		return "", fmt.Errorf("marshal adaptive benchmark dataset: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func requireBenchmarkJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("adaptive benchmark JSON has trailing data")
		}
		return fmt.Errorf("decode adaptive benchmark trailing data: %w", err)
	}
	return nil
}

func validateAdaptiveBenchmarkDataset(dataset AdaptiveBenchmarkDataset) error {
	if dataset.DatasetID != AdaptiveBenchmarkDatasetID ||
		dataset.Revision != AdaptiveBenchmarkDatasetRevision ||
		len(dataset.Scenarios) != 52 {
		return errors.New("adaptive benchmark dataset identity is invalid")
	}
	wantIDs := adaptiveBenchmarkScenarioIDs()
	for index, scenario := range dataset.Scenarios {
		if scenario.ScenarioID != wantIDs[index] ||
			scenario.Domain != adaptiveBenchmarkDomainForID(scenario.ScenarioID) ||
			scenario.EvaluatorID != AdaptiveBenchmarkEvaluatorID ||
			scenario.EvaluatorRevision != AdaptiveBenchmarkEvaluatorRevision ||
			!validSyntheticBenchmarkText(scenario.Prompt, 2048) ||
			!validSyntheticBenchmarkText(scenario.Expected, 512) {
			return fmt.Errorf(
				"adaptive benchmark scenario %q is invalid",
				scenario.ScenarioID,
			)
		}
	}
	digest, err := adaptiveBenchmarkDatasetSHA256(dataset)
	if err != nil {
		return err
	}
	if digest != AdaptiveBenchmarkDatasetSHA256 {
		return errors.New("adaptive benchmark corpus digest is not registered")
	}
	return nil
}

func validSyntheticBenchmarkText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character == '\n' || character == '\r' || character == 0 ||
			unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func adaptiveBenchmarkScenarioIDs() []string {
	ids := make([]string, 0, 52)
	for _, prefixCount := range []struct {
		prefix string
		count  int
	}{
		{prefix: "r", count: 12},
		{prefix: "t", count: 12},
		{prefix: "s", count: 12},
		{prefix: "k", count: 8},
		{prefix: "m", count: 8},
	} {
		for ordinal := 1; ordinal <= prefixCount.count; ordinal++ {
			ids = append(ids, fmt.Sprintf("%s%02d", prefixCount.prefix, ordinal))
		}
	}
	return ids
}

func adaptiveBenchmarkDomainForID(scenarioID string) adaptive.Domain {
	if len(scenarioID) != 3 {
		return ""
	}
	switch scenarioID[0] {
	case 'r':
		return adaptive.DomainReasoning
	case 't':
		return adaptive.DomainToolDiscovery
	case 's':
		return adaptive.DomainSkillRouting
	case 'k':
		return adaptive.DomainKnowledge
	case 'm':
		return adaptive.DomainMemoryRecall
	default:
		return ""
	}
}

// AdaptiveBenchmarkScenarioOrder returns a clone in the registered SHA schedule.
func AdaptiveBenchmarkScenarioOrder(
	scenarios []AdaptiveBenchmarkScenario,
) []AdaptiveBenchmarkScenario {
	ordered := slices.Clone(scenarios)
	slices.SortStableFunc(ordered, func(
		left AdaptiveBenchmarkScenario,
		right AdaptiveBenchmarkScenario,
	) int {
		leftHash := adaptiveBenchmarkScheduleHash(left.ScenarioID)
		rightHash := adaptiveBenchmarkScheduleHash(right.ScenarioID)
		if compared := bytes.Compare(leftHash[:], rightHash[:]); compared != 0 {
			return compared
		}
		return strings.Compare(left.ScenarioID, right.ScenarioID)
	})
	return ordered
}

func adaptiveBenchmarkScheduleHash(scenarioID string) [sha256.Size]byte {
	payload := make([]byte, 9+len(scenarioID))
	binary.BigEndian.PutUint64(payload[:8], AdaptiveBenchmarkSeed)
	copy(payload[9:], scenarioID)
	return sha256.Sum256(payload)
}

// AdaptiveBenchmarkActionCatalog is one production action catalog in wire order.
type AdaptiveBenchmarkActionCatalog struct {
	Domain    adaptive.Domain `json:"domain"`
	ActionIDs []string        `json:"action_ids"`
}

var adaptiveBenchmarkActionCatalogs = []AdaptiveBenchmarkActionCatalog{
	{
		Domain: adaptive.DomainReasoning,
		ActionIDs: []string{
			string(agent.ReasoningActionHigh),
			string(agent.ReasoningActionLow),
			string(agent.ReasoningActionNone),
			string(agent.ReasoningActionStatic),
		},
	},
	{
		// The tool-discovery domain is RETIRED: tool_search resolves names and ranks
		// lexically, so there is one strategy and nothing left to assign between. The
		// row stays because the ledger still holds rows stamped with this domain and
		// a report over them must decode; nothing constructs a control for it.
		Domain: adaptive.DomainToolDiscovery,
		ActionIDs: []string{
			"bm25",
			"semantic",
			"static",
		},
	},
	{
		Domain: adaptive.DomainSkillRouting,
		ActionIDs: []string{
			tools.SkillRoutingCatalog,
			tools.SkillRoutingStatic,
		},
	},
	{
		// The knowledge-retrieval domain is RETIRED, for the same reason and in the
		// same shape as tool-discovery above: document_search ranks a Postgres digest
		// and document_open hands over the file, so the five retrieval plans these ids
		// named no longer exist and nothing constructs a control for them. The row
		// stays because the ledger still holds rows stamped with this domain and a
		// report over them must decode.
		Domain: adaptive.DomainKnowledge,
		ActionIDs: []string{
			"sparse",
			"static",
			"vector",
			"vector_rerank",
			"vector_rerank_expand",
		},
	},
	{
		Domain: adaptive.DomainMemoryRecall,
		ActionIDs: []string{
			string(runner.DynamicRecallStatic),
			string(runner.DynamicRecallTop4),
			string(runner.DynamicRecallTop8),
		},
	},
}

// AdaptiveBenchmarkActionCatalogs returns a deep clone of all five catalogs.
func AdaptiveBenchmarkActionCatalogs() []AdaptiveBenchmarkActionCatalog {
	catalogs := make(
		[]AdaptiveBenchmarkActionCatalog,
		len(adaptiveBenchmarkActionCatalogs),
	)
	for index, catalog := range adaptiveBenchmarkActionCatalogs {
		catalogs[index] = AdaptiveBenchmarkActionCatalog{
			Domain:    catalog.Domain,
			ActionIDs: slices.Clone(catalog.ActionIDs),
		}
	}
	return catalogs
}

// AdaptiveBenchmarkActionCatalogsSHA256 binds domain and action ordering.
func AdaptiveBenchmarkActionCatalogsSHA256() string {
	payload, err := json.Marshal(adaptiveBenchmarkActionCatalogs)
	if err != nil {
		panic(fmt.Sprintf("marshal registered adaptive benchmark catalogs: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
