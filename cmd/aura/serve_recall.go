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

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
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
}

func dynamicRecallProvider(cfg *config.Config) runner.DynamicRecallProvider {
	return dynamicRecallProviderWithCall(cfg, callMemoryToolText)
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
		for _, limit := range metadata.Limits {
			if limit.RequestedK != selectedMaximum {
				return runner.DynamicRecall{}, errors.New(
					"dynamic recall requested limit is inconsistent",
				)
			}
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
