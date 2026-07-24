package adaptive

import (
	"errors"
	"fmt"
	"math"
)

type FeatureKey string

const (
	FeatureAvailableSkillCount  FeatureKey = "available_skill_count"
	FeatureAvailableToolCount   FeatureKey = "available_tool_count"
	FeatureCandidateCount       FeatureKey = "candidate_count"
	FeatureContextLength        FeatureKey = "context_length"
	FeatureContextTokenCount    FeatureKey = "context_token_count"
	FeatureDeferredToolCount    FeatureKey = "deferred_tool_count"
	FeatureDocumentCount        FeatureKey = "document_count"
	FeatureInputTokenCount      FeatureKey = "input_token_count"
	FeatureKNNSimilarity        FeatureKey = "knn_similarity"
	FeatureMatchedSkillCount    FeatureKey = "matched_skill_count"
	FeatureMemoryCount          FeatureKey = "memory_count"
	FeatureMessageCount         FeatureKey = "message_count"
	FeaturePromptLength         FeatureKey = "prompt_length"
	FeatureQueryLength          FeatureKey = "query_length"
	FeatureRecallLimit          FeatureKey = "recall_limit"
	FeatureRequestLength        FeatureKey = "request_length"
	FeatureResultCount          FeatureKey = "result_count"
	FeatureRetrievalLimit       FeatureKey = "retrieval_limit"
	FeatureRetrievedResultCount FeatureKey = "retrieved_result_count"
	FeatureRetryCount           FeatureKey = "retry_count"
	FeatureSkillCandidateCount  FeatureKey = "skill_candidate_count"
	FeatureToolArgumentCount    FeatureKey = "tool_argument_count"
	FeatureToolResultCount      FeatureKey = "tool_result_count"
)

type featureSpec struct {
	minimum float64
	maximum float64
	integer bool
}

var countFeatureSpec = featureSpec{minimum: 0, maximum: 1_000_000_000, integer: true}

var featureSchema = map[FeatureKey]featureSpec{
	FeatureAvailableSkillCount:  countFeatureSpec,
	FeatureAvailableToolCount:   countFeatureSpec,
	FeatureCandidateCount:       countFeatureSpec,
	FeatureContextLength:        countFeatureSpec,
	FeatureContextTokenCount:    countFeatureSpec,
	FeatureDeferredToolCount:    countFeatureSpec,
	FeatureDocumentCount:        countFeatureSpec,
	FeatureInputTokenCount:      countFeatureSpec,
	FeatureKNNSimilarity:        {minimum: 0, maximum: 1},
	FeatureMatchedSkillCount:    countFeatureSpec,
	FeatureMemoryCount:          countFeatureSpec,
	FeatureMessageCount:         countFeatureSpec,
	FeaturePromptLength:         countFeatureSpec,
	FeatureQueryLength:          countFeatureSpec,
	FeatureRecallLimit:          countFeatureSpec,
	FeatureRequestLength:        countFeatureSpec,
	FeatureResultCount:          countFeatureSpec,
	FeatureRetrievalLimit:       countFeatureSpec,
	FeatureRetrievedResultCount: countFeatureSpec,
	FeatureRetryCount:           countFeatureSpec,
	FeatureSkillCandidateCount:  countFeatureSpec,
	FeatureToolArgumentCount:    countFeatureSpec,
	FeatureToolResultCount:      countFeatureSpec,
}

func validateNumericFeatures(features map[FeatureKey]float64) error {
	if len(features) > 128 {
		return errors.New("adaptive assignment features exceed 128 entries")
	}
	for key, value := range features {
		if privatePayloadAlias(string(key)) {
			return fmt.Errorf("adaptive feature key %q is private", key)
		}
		spec, registered := featureSchema[key]
		if !registered {
			return fmt.Errorf("adaptive feature key %q is not registered", key)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			value < spec.minimum || value > spec.maximum ||
			(spec.integer && math.Trunc(value) != value) {
			return fmt.Errorf("adaptive assignment feature %q is out of range", key)
		}
	}
	return nil
}
