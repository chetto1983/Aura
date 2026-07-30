// serve_recall.go bridges adaptive memory recall to the live memory MCP.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type memoryRecallCall func(
	context.Context,
	string,
	map[string]any,
) (string, error)

type memoryRecallResponse struct {
	SessionID      string               `json:"session_id"`
	Context        string               `json:"context"`
	HasContext     bool                 `json:"has_context"`
	Error          string               `json:"error"`
	RecallMetadata memoryRecallMetadata `json:"recall_metadata"`
}

type memoryRecallMetadata struct {
	Results           []runner.DynamicRecallResult         `json:"results"`
	Limits            map[string]runner.DynamicRecallLimit `json:"limits"`
	Revisions         runner.DynamicRecallRevisions        `json:"revisions"`
	CorpusEpochBefore *uint64                              `json:"corpus_epoch_before"`
	CorpusEpochAfter  *uint64                              `json:"corpus_epoch_after"`
	Coherent          bool                                 `json:"coherent"`
	AdaptiveEligible  bool                                 `json:"adaptive_eligible"`
	RerankerStatus    string                               `json:"reranker_status"`
}

func dynamicRecallProvider(
	cfg *config.Config,
	client mcptools.HostClient,
) runner.DynamicRecallProvider {
	if client == nil {
		return nil
	}
	return dynamicRecallProviderWithCall(cfg, client.CallTool)
}

func dynamicRecallProviderWithCall(
	cfg *config.Config,
	call memoryRecallCall,
) runner.DynamicRecallProvider {
	if cfg == nil || !cfg.MemoryRecall || cfg.MemoryRecallMaxItems < 4 ||
		call == nil {
		return nil
	}
	configuredMaximum := cfg.MemoryRecallMaxItems
	return func(
		ctx context.Context,
		ownerID string,
		query string,
		selectedMaximum int,
	) (runner.DynamicRecall, error) {
		if strings.TrimSpace(ownerID) == "" {
			return runner.DynamicRecall{}, errors.New("dynamic recall owner is empty")
		}
		if (selectedMaximum != 4 && selectedMaximum != 8) ||
			selectedMaximum > configuredMaximum {
			return runner.DynamicRecall{}, fmt.Errorf(
				"dynamic recall limit %d is not configured",
				selectedMaximum,
			)
		}
		text, err := call(ctx, "memory_get_context", map[string]any{
			"user_identifier":    ownerID,
			"query":              query,
			"include_short_term": false,
			"include_long_term":  true,
			"include_reasoning":  false,
			"max_items":          selectedMaximum,
		})
		if err != nil {
			return runner.DynamicRecall{}, fmt.Errorf(
				"dynamic recall memory_get_context: %w",
				err,
			)
		}
		response, err := decodeMemoryRecallResponse(text)
		if err != nil {
			return runner.DynamicRecall{}, err
		}
		if response.Error != "" {
			return runner.DynamicRecall{}, errors.New("dynamic recall provider rejected request")
		}
		if !response.HasContext || strings.TrimSpace(response.Context) == "" {
			return runner.DynamicRecall{}, errors.New("dynamic recall returned no context")
		}
		metadata := response.RecallMetadata
		if !metadata.AdaptiveEligible || !metadata.Coherent ||
			metadata.CorpusEpochBefore == nil ||
			metadata.CorpusEpochAfter == nil ||
			*metadata.CorpusEpochBefore == 0 ||
			*metadata.CorpusEpochBefore != *metadata.CorpusEpochAfter {
			return runner.DynamicRecall{}, errors.New(
				"dynamic recall metadata is ineligible",
			)
		}
		if err := validateMemoryRecallMetadata(
			metadata,
			response.Context,
			selectedMaximum,
		); err != nil {
			return runner.DynamicRecall{}, err
		}
		return runner.DynamicRecall{
			Text:              runner.FenceDynamicRecall(response.Context),
			Results:           metadata.Results,
			Limits:            metadata.Limits,
			Revisions:         metadata.Revisions,
			CorpusEpochBefore: metadata.CorpusEpochBefore,
			CorpusEpochAfter:  metadata.CorpusEpochAfter,
			Coherent:          metadata.Coherent,
		}, nil
	}
}

func validateMemoryRecallMetadata(
	metadata memoryRecallMetadata,
	contextText string,
	selectedMaximum int,
) error {
	if metadata.RerankerStatus != "applied" {
		return errors.New("dynamic recall reranker did not apply")
	}
	revisions := metadata.Revisions
	if strings.TrimSpace(revisions.Retriever) == "" ||
		strings.TrimSpace(revisions.Reranker) == "" ||
		strings.EqualFold(strings.TrimSpace(revisions.Reranker), "none-v1") ||
		strings.TrimSpace(revisions.Embedding) == "" ||
		strings.TrimSpace(revisions.Index) == "" {
		return errors.New("dynamic recall revisions are incomplete")
	}
	if len(metadata.Results) == 0 || len(metadata.Results) > selectedMaximum {
		return errors.New("dynamic recall result cardinality is inconsistent")
	}
	if len(metadata.Limits) != 2 {
		return errors.New("dynamic recall limits are incomplete")
	}
	kinds := []string{"memory_preference", "memory_entity"}
	counts := map[string]int{}
	seenIDs := map[string]struct{}{}
	lastMarker := -1
	for order, result := range metadata.Results {
		if result.Order != order {
			return errors.New("dynamic recall result order is inconsistent")
		}
		if result.Kind != kinds[0] && result.Kind != kinds[1] {
			return errors.New("dynamic recall result kind is invalid")
		}
		if _, err := uuid.Parse(result.ID); err != nil {
			return errors.New("dynamic recall result ID is invalid")
		}
		if _, exists := seenIDs[result.ID]; exists {
			return errors.New("dynamic recall result ID is duplicated")
		}
		seenIDs[result.ID] = struct{}{}
		counts[result.Kind]++

		marker := "[" + result.Kind + ":" + result.ID + "]"
		if strings.Count(contextText, marker) != 1 {
			return errors.New("dynamic recall context provenance is inconsistent")
		}
		position := strings.Index(contextText, marker)
		if position <= lastMarker {
			return errors.New("dynamic recall context provenance order is inconsistent")
		}
		lastMarker = position
	}
	if strings.Count(contextText, "[memory_") != len(metadata.Results) {
		return errors.New("dynamic recall context provenance cardinality is inconsistent")
	}
	for _, kind := range kinds {
		limit, exists := metadata.Limits[kind]
		if !exists ||
			limit.RequestedK != selectedMaximum ||
			limit.EffectiveK != limit.Count ||
			limit.Count != counts[kind] ||
			limit.Count < 0 ||
			limit.Count > selectedMaximum {
			return errors.New("dynamic recall limits are inconsistent")
		}
	}
	return nil
}

func decodeMemoryRecallResponse(text string) (memoryRecallResponse, error) {
	var response memoryRecallResponse
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, fmt.Errorf("decode dynamic recall response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return response, err
	}
	return response, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode dynamic recall response: trailing JSON value")
		}
		return fmt.Errorf("decode dynamic recall response trailing data: %w", err)
	}
	return nil
}
