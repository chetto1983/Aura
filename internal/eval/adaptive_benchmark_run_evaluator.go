package eval

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/adaptive"
)

// AdaptiveBenchmarkObserved is transient driver output and is never serialized.
type AdaptiveBenchmarkObserved struct {
	Answer    string
	ToolID    string
	SkillID   string
	ResultIDs []string
}

// AdaptiveBenchmarkEvaluation is the deterministic checker's binary result.
type AdaptiveBenchmarkEvaluation struct {
	Passed      bool
	ReasonCodes []string
}

// EvaluateAdaptiveBenchmarkScenario evaluates one synthetic observation.
func EvaluateAdaptiveBenchmarkScenario(
	scenario AdaptiveBenchmarkScenario,
	observed AdaptiveBenchmarkObserved,
) (AdaptiveBenchmarkEvaluation, error) {
	if scenario.Domain != adaptiveBenchmarkDomainForID(scenario.ScenarioID) ||
		scenario.EvaluatorID != AdaptiveBenchmarkEvaluatorID ||
		scenario.EvaluatorRevision != AdaptiveBenchmarkEvaluatorRevision {
		return AdaptiveBenchmarkEvaluation{}, errors.New(
			"adaptive benchmark evaluator input is not registered",
		)
	}
	var passed bool
	var reason string
	switch scenario.Domain {
	case adaptive.DomainReasoning, adaptive.DomainKnowledge:
		passed = normalizeAdaptiveBenchmarkAnswer(observed.Answer) ==
			normalizeAdaptiveBenchmarkAnswer(scenario.Expected)
		reason = AdaptiveBenchmarkReasonAnswerMismatch
	case adaptive.DomainToolDiscovery:
		passed = observed.ToolID == scenario.Expected
		reason = AdaptiveBenchmarkReasonToolMismatch
	case adaptive.DomainSkillRouting:
		passed = observed.SkillID == scenario.Expected
		reason = AdaptiveBenchmarkReasonSkillMismatch
	case adaptive.DomainMemoryRecall:
		expected := strings.Split(scenario.Expected, ",")
		passed = slices.Equal(observed.ResultIDs, expected)
		reason = AdaptiveBenchmarkReasonResultOrderMismatch
	default:
		return AdaptiveBenchmarkEvaluation{}, fmt.Errorf(
			"adaptive benchmark domain %q is unsupported",
			scenario.Domain,
		)
	}
	if passed {
		return AdaptiveBenchmarkEvaluation{Passed: true, ReasonCodes: []string{}}, nil
	}
	return AdaptiveBenchmarkEvaluation{
		Passed:      false,
		ReasonCodes: []string{reason},
	}, nil
}

func normalizeAdaptiveBenchmarkAnswer(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "`'\".,;:!?$€ ")
	words := strings.Fields(value)
	numberWords := map[string]string{
		"seven": "7", "eighteen": "18", "ninety": "90",
	}
	for index, word := range words {
		if replacement, ok := numberWords[word]; ok {
			words[index] = replacement
		}
	}
	value = strings.Join(words, " ")
	return strings.ReplaceAll(value, " at ", " ")
}
