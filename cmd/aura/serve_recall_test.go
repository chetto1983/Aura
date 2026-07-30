package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

func TestDynamicRecallProviderUsesSelectedLongTermLimitOnce(t *testing.T) {
	var calls int
	var captured map[string]any
	provider := dynamicRecallProviderWithCall(
		&config.Config{MemoryRecall: true, MemoryRecallMaxItems: 8},
		func(
			_ context.Context,
			tool string,
			args map[string]any,
		) (string, error) {
			calls++
			if tool != "memory_get_context" {
				t.Fatalf("tool = %q", tool)
			}
			captured = args
			return validMemoryRecallJSON(t), nil
		},
	)

	recall, err := provider(context.Background(), "owner-1", "private query", 4)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if calls != 1 {
		t.Fatalf("memory calls = %d, want 1", calls)
	}
	wantArgs := map[string]any{
		"user_identifier":    "owner-1",
		"query":              "private query",
		"include_short_term": false,
		"include_long_term":  true,
		"include_reasoning":  false,
		"max_items":          4,
	}
	gotArgs, _ := json.Marshal(captured)
	expectedArgs, _ := json.Marshal(wantArgs)
	if string(gotArgs) != string(expectedArgs) {
		t.Fatalf("args = %s, want %s", gotArgs, expectedArgs)
	}
	if recall.Text != runner.FenceDynamicRecall(
		"## Relevant Knowledge\n### Retrieved Memory\n"+
			"- [memory_preference:018fbc5a-6e3f-7ed0-8a04-7cfae67e61ef] "+
			"remembered preference\n"+
			"- [memory_entity:018fbc5a-7cb4-75e8-b204-a681980e3b31] "+
			"remembered entity",
	) {
		t.Fatalf("fenced text = %q", recall.Text)
	}
	if len(recall.Results) != 2 ||
		recall.Results[0].Kind != "memory_preference" ||
		recall.Results[1].Order != 1 {
		t.Fatalf("typed results = %#v", recall.Results)
	}
	if recall.Limits["memory_entity"].RequestedK != 4 ||
		recall.Revisions.Index != "entity_idx@7" ||
		recall.CorpusEpochBefore == nil ||
		*recall.CorpusEpochBefore != 7 {
		t.Fatalf("typed metadata = %#v", recall)
	}
}

func TestDynamicRecallProviderFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		owner     string
		maxItems  int
		response  string
		wantCalls int
	}{
		{name: "blank owner", owner: " ", maxItems: 4},
		{name: "invalid selected limit", owner: "owner-1", maxItems: 7},
		{
			name: "ineligible metadata", owner: "owner-1", maxItems: 4,
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"adaptive_eligible":true`,
				`"adaptive_eligible":false`,
				1,
			),
			wantCalls: 1,
		},
		{
			name: "missing epoch", owner: "owner-1", maxItems: 4,
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"corpus_epoch_before":7`,
				`"corpus_epoch_before":null`,
				1,
			),
			wantCalls: 1,
		},
		{
			name: "unknown metadata", owner: "owner-1", maxItems: 4,
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"adaptive_eligible":true`,
				`"adaptive_eligible":true,"query":"must-not-leak"`,
				1,
			),
			wantCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			provider := dynamicRecallProviderWithCall(
				&config.Config{MemoryRecall: true, MemoryRecallMaxItems: 8},
				func(context.Context, string, map[string]any) (string, error) {
					calls++
					return test.response, nil
				},
			)
			if _, err := provider(
				context.Background(),
				test.owner,
				"query",
				test.maxItems,
			); err == nil {
				t.Fatal("provider error = nil")
			}
			if calls != test.wantCalls {
				t.Fatalf("memory calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

func TestDynamicRecallProviderIsDisabledBelowMinimumCatalog(t *testing.T) {
	for _, cfg := range []*config.Config{
		nil,
		{},
		{MemoryRecall: true, MemoryRecallMaxItems: 3},
	} {
		if provider := dynamicRecallProviderWithCall(
			cfg,
			func(context.Context, string, map[string]any) (string, error) {
				t.Fatal("disabled provider called")
				return "", nil
			},
		); provider != nil {
			t.Fatalf("provider = %#v, want nil", provider)
		}
	}
	if provider := dynamicRecallProvider(&config.Config{}, &memoryReadinessClient{}); provider != nil {
		t.Fatalf("production disabled provider = %#v, want nil", provider)
	}
}

func TestDynamicRecallProviderRejectsTransportAndResponseEdges(t *testing.T) {
	transportErr := errors.New("memory offline")
	tests := []struct {
		name     string
		response string
		callErr  error
	}{
		{name: "transport", callErr: transportErr},
		{
			name: "provider error",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"session_id":"owner-1"`,
				`"session_id":"owner-1","error":"rejected"`,
				1,
			),
		},
		{
			name: "empty context",
			response: strings.Replace(
				strings.Replace(
					validMemoryRecallJSON(t),
					`"context":"remembered fact"`,
					`"context":""`,
					1,
				),
				`"has_context":true`,
				`"has_context":false`,
				1,
			),
		},
		{
			name: "inconsistent selected limit",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"requested_k":4`,
				`"requested_k":3`,
				1,
			),
		},
		{
			name: "too many aggregate results",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"results":[`,
				`"results":[
				{"kind":"memory_preference","id":"018fbc5a-1111-7111-8111-111111111111","order":0},
				{"kind":"memory_entity","id":"018fbc5a-2222-7222-8222-222222222222","order":1},
				{"kind":"memory_preference","id":"018fbc5a-3333-7333-8333-333333333333","order":2},
				{"kind":"memory_entity","id":"018fbc5a-4444-7444-8444-444444444444","order":3},
				{"kind":"memory_entity","id":"018fbc5a-5555-7555-8555-555555555555","order":4},`,
				1,
			),
		},
		{
			name: "result order mismatch",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"order":1`,
				`"order":3`,
				1,
			),
		},
		{
			name: "result count mismatch",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"count":1`,
				`"count":2`,
				1,
			),
		},
		{
			name: "effective limit mismatch",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"effective_k":1`,
				`"effective_k":4`,
				1,
			),
		},
		{
			name: "reranker did not apply",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`"reranker_status":"applied"`,
				`"reranker_status":"degraded"`,
				1,
			),
		},
		{
			name: "context provenance mismatch",
			response: strings.Replace(
				validMemoryRecallJSON(t),
				`[memory_entity:018fbc5a-7cb4-75e8-b204-a681980e3b31]`,
				`[memory_entity:018fbc5a-aaaaaaaa-75e8-b204-a681980e3b31]`,
				1,
			),
		},
		{name: "trailing value", response: validMemoryRecallJSON(t) + `{}`},
		{name: "trailing malformed data", response: validMemoryRecallJSON(t) + `x`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := dynamicRecallProviderWithCall(
				&config.Config{MemoryRecall: true, MemoryRecallMaxItems: 8},
				func(context.Context, string, map[string]any) (string, error) {
					return test.response, test.callErr
				},
			)
			if _, err := provider(
				context.Background(),
				"owner-1",
				"query",
				4,
			); err == nil {
				t.Fatal("provider error = nil")
			}
		})
	}
}

func validMemoryRecallJSON(t *testing.T) string {
	t.Helper()
	return `{
		"session_id":"owner-1",
		"context":"## Relevant Knowledge\n### Retrieved Memory\n- [memory_preference:018fbc5a-6e3f-7ed0-8a04-7cfae67e61ef] remembered preference\n- [memory_entity:018fbc5a-7cb4-75e8-b204-a681980e3b31] remembered entity",
		"has_context":true,
		"recall_metadata":{
			"results":[
				{"kind":"memory_preference","id":"018fbc5a-6e3f-7ed0-8a04-7cfae67e61ef","order":0},
				{"kind":"memory_entity","id":"018fbc5a-7cb4-75e8-b204-a681980e3b31","order":1}
			],
			"limits":{
				"memory_preference":{"requested_k":4,"effective_k":1,"count":1},
				"memory_entity":{"requested_k":4,"effective_k":1,"count":1}
			},
			"revisions":{
				"retriever":"neo4j-long-term-v1",
				"reranker":"aura-rerank",
				"embedding":"qwen2b@7",
				"index":"entity_idx@7"
			},
			"reranker_status":"applied",
			"corpus_epoch_before":7,
			"corpus_epoch_after":7,
			"coherent":true,
			"adaptive_eligible":true
		}
	}`
}
