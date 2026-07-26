package main

import (
	"context"
	"strconv"

	"github.com/chetto1983/aura/internal/llm"
)

func adaptiveBenchmarkConcurrencyStreamInterceptor(
	knowledgeQuery string,
) adaptiveBenchmarkStreamInterceptor {
	return func(
		ctx context.Context,
		request llm.Request,
	) (<-chan llm.Chunk, error) {
		clientID, _ := ctx.Value(
			adaptiveBenchmarkConcurrencyContextKey{},
		).(string)
		switch clientID {
		case "C0", "C1", "C2":
			return adaptiveBenchmarkChunkStream(
				llm.Chunk{Text: "benchmark-control-complete"},
				llm.Chunk{FinishReason: "stop"},
			), nil
		case "C3":
			for _, message := range request.Messages {
				if message.Role == llm.RoleTool {
					return adaptiveBenchmarkChunkStream(
						llm.Chunk{Text: "benchmark-control-complete"},
						llm.Chunk{FinishReason: "stop"},
					), nil
				}
			}
			if !adaptiveBenchmarkRequestHasTool(
				request,
				"document_search",
			) {
				return nil, adaptiveBenchmarkControlError(
					"adaptive benchmark C3 lacks document_search",
				)
			}
			call := llm.ToolCall{
				ID:   "adaptive-benchmark-c3-document-search",
				Type: "function",
			}
			call.Function.Name = "document_search"
			call.Function.Arguments = `{"query":` +
				strconv.Quote(knowledgeQuery) +
				`,"limit":8}`
			return adaptiveBenchmarkChunkStream(
				llm.Chunk{ToolCall: &call},
				llm.Chunk{FinishReason: "tool_calls"},
			), nil
		default:
			return nil, adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency client is unavailable",
			)
		}
	}
}

func adaptiveBenchmarkRequestHasTool(
	request llm.Request,
	name string,
) bool {
	for _, tool := range request.Tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}

func adaptiveBenchmarkChunkStream(chunks ...llm.Chunk) <-chan llm.Chunk {
	stream := make(chan llm.Chunk, len(chunks))
	for _, chunk := range chunks {
		stream <- chunk
	}
	close(stream)
	return stream
}

func adaptiveBenchmarkInstallConcurrencyStreamInterceptors(
	states map[string]*adaptiveBenchmarkConcurrencyClientState,
) (func(), error) {
	scenario, err := adaptiveBenchmarkControlScenario("k01")
	if err != nil {
		return nil, err
	}
	clients := map[*adaptiveBenchmarkObservedClient]struct{}{}
	for _, state := range states {
		if state == nil || state.subject.observed == nil {
			return nil, adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency observed client is unavailable",
			)
		}
		clients[state.subject.observed] = struct{}{}
	}
	interceptor := adaptiveBenchmarkConcurrencyStreamInterceptor(
		scenario.Prompt,
	)
	clear := make([]func(), 0, len(clients))
	for client := range clients {
		clearClient, err := client.SetStreamInterceptor(interceptor)
		if err != nil {
			for _, installed := range clear {
				installed()
			}
			return nil, err
		}
		clear = append(clear, clearClient)
	}
	return func() {
		for _, installed := range clear {
			installed()
		}
	}, nil
}
