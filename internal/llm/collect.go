package llm

import (
	"context"
	"fmt"
	"strings"
)

// Collect runs one tool-free request to its end and returns its text and usage. It drains the
// stream, as the Client contract requires, and wants a clean stop: a stream that ends any other
// way may have cut its text.
func Collect(ctx context.Context, client Client, req Request) (string, Usage, error) {
	ch, err := client.Stream(ctx, req)
	if err != nil {
		return "", Usage{}, fmt.Errorf("stream: %w", err)
	}
	var (
		text    strings.Builder
		usage   Usage
		finish  string
		failure error
	)
	for chunk := range ch {
		if chunk.Err != nil && failure == nil {
			failure = fmt.Errorf("stream: %w", chunk.Err)
		}
		text.WriteString(chunk.Text)
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
	}
	if failure != nil {
		return "", Usage{}, failure
	}
	if finish != "stop" {
		return "", Usage{}, fmt.Errorf("incomplete stream (finish_reason=%q)", finish)
	}
	return text.String(), usage, nil
}
